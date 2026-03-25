package data

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

const (
	taskTableName          = "squirrel_tasks"
	sectionImportTableName = "squirrel_section_imports"
	sectionRowTableName    = "squirrel_section_rows"
)

var errSectionImportNotFound = errors.New("section import not found")

func ErrSectionImportNotFound() error { return errSectionImportNotFound }

type SquirrelTask struct {
	TaskID    string    `gorm:"column:task_id;type:varchar(26);primaryKey;comment:任务ID"`
	Title     string    `gorm:"column:title;type:varchar(128);not null;comment:任务标题"`
	MonthTag  string    `gorm:"column:month_tag;type:varchar(7);not null;default:'';index:idx_squirrel_tasks_month;comment:任务月份(YYYY-MM)"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;index:idx_squirrel_tasks_created;comment:创建时间"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*SquirrelTask) TableName() string { return taskTableName }

type SquirrelSectionImport struct {
	FileID      string    `gorm:"column:file_id;type:varchar(26);primaryKey;comment:文件ID"`
	TaskID      string    `gorm:"column:task_id;type:varchar(26);not null;index:idx_squirrel_import_task_section,priority:1;comment:任务ID"`
	Section     string    `gorm:"column:section;type:varchar(32);not null;index:idx_squirrel_import_task_section,priority:2;comment:区域标识"`
	Filename    string    `gorm:"column:filename;type:varchar(255);not null;default:'';comment:文件名"`
	SheetName   string    `gorm:"column:sheet_name;type:varchar(128);not null;default:'';comment:sheet 名称"`
	ColumnCount int32     `gorm:"column:column_count;type:int;not null;default:0;comment:列数"`
	TotalRows   int32     `gorm:"column:total_rows;type:int;not null;default:0;comment:总行数"`
	HeaderJSON  string    `gorm:"column:header_json;type:text;not null;default:'[]';comment:表头 JSON"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamptz;not null;comment:创建时间"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*SquirrelSectionImport) TableName() string { return sectionImportTableName }

type SquirrelSectionRow struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	FileID     string    `gorm:"column:file_id;type:varchar(26);not null;index:idx_squirrel_rows_file_row,priority:1;comment:文件ID"`
	RowNo      int32     `gorm:"column:row_no;type:int;not null;index:idx_squirrel_rows_file_row,priority:2;comment:原始行号(从1开始)"`
	ValuesJSON string    `gorm:"column:values_json;type:text;not null;default:'[]';comment:行数据 JSON"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamptz;not null;comment:创建时间"`
}

func (*SquirrelSectionRow) TableName() string { return sectionRowTableName }

type SectionPageResult struct {
	Import   *SquirrelSectionImport
	Imports  []SquirrelSectionImport
	Rows     []SquirrelSectionRowData
	Total    int64
	MaxCols  int
	HasEmpty bool
}

type SquirrelSectionRowData struct {
	FileID   string
	Filename string
	RowNo    int32
	Values   []string
}

type ParsedRow struct {
	RowNo  int32
	Values []string
}

type SquirrelRepo struct {
	data *Data
	log  *log.Helper
}

func NewSquirrelRepo(data *Data, logger log.Logger) *SquirrelRepo {
	return &SquirrelRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "squirrel/SquirrelRepo")),
	}
}

func (r *SquirrelRepo) ListTasks(ctx context.Context) ([]SquirrelTask, error) {
	items := make([]SquirrelTask, 0)
	err := r.data.pg.WithContext(ctx).
		Order("created_at DESC").
		Find(&items).Error
	return items, err
}

func (r *SquirrelRepo) GetTask(ctx context.Context, taskID string) (*SquirrelTask, error) {
	var task SquirrelTask
	err := r.data.pg.WithContext(ctx).Where("task_id = ?", taskID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *SquirrelRepo) CreateTask(ctx context.Context, title, monthTag string) (*SquirrelTask, error) {
	now := time.Now()
	task := &SquirrelTask{
		TaskID:    ulid.Make().String(),
		Title:     title,
		MonthTag:  monthTag,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := r.data.pg.WithContext(ctx).Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func (r *SquirrelRepo) DeleteTask(ctx context.Context, taskID string) error {
	return r.data.pg.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		imports := make([]SquirrelSectionImport, 0)
		if err := tx.Where("task_id = ?", taskID).Find(&imports).Error; err != nil {
			return err
		}
		for i := range imports {
			if err := tx.Where("file_id = ?", imports[i].FileID).Delete(&SquirrelSectionRow{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&SquirrelSectionImport{}).Error; err != nil {
			return err
		}
		runs := make([]SquirrelReconcileRun, 0)
		if err := tx.Where("task_id = ?", taskID).Find(&runs).Error; err != nil {
			return err
		}
		for i := range runs {
			if err := tx.Where("run_id = ?", runs[i].RunID).Delete(&SquirrelReconcileRow{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&SquirrelReconcileRun{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&SquirrelManualReview{}).Error; err != nil {
			return err
		}
		return tx.Where("task_id = ?", taskID).Delete(&SquirrelTask{}).Error
	})
}

func (r *SquirrelRepo) ListSectionImportsByTask(ctx context.Context, taskID string) ([]SquirrelSectionImport, error) {
	items := make([]SquirrelSectionImport, 0)
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("updated_at DESC").
		Find(&items).Error
	return items, err
}

func (r *SquirrelRepo) GetSectionImport(ctx context.Context, taskID, section string) (*SquirrelSectionImport, error) {
	var item SquirrelSectionImport
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ? AND section = ?", taskID, section).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *SquirrelRepo) ListSectionImports(ctx context.Context, taskID, section string) ([]SquirrelSectionImport, error) {
	items := make([]SquirrelSectionImport, 0)
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ? AND section = ?", taskID, section).
		Order("updated_at DESC, file_id DESC").
		Find(&items).Error
	return items, err
}

func (r *SquirrelRepo) SaveSectionFile(ctx context.Context, taskID, section, filename, sheetName string, header []string, rows []ParsedRow, replaceOld bool) (*SquirrelSectionImport, error) {
	if section == "sales" {
		// 强制销售明细单文件模式：新文件覆盖旧文件。
		replaceOld = true
	}
	normalizedHeader := trimTailBlanks(header)
	maxColumns := len(normalizedHeader)
	if maxColumns == 0 {
		for i := range rows {
			if len(rows[i].Values) > maxColumns {
				maxColumns = len(rows[i].Values)
			}
		}
	}
	if maxColumns < len(normalizedHeader) {
		maxColumns = len(normalizedHeader)
	}

	headerJSON := marshalStringSlice(normalizedHeader)
	now := time.Now()
	newImport := &SquirrelSectionImport{
		FileID:      ulid.Make().String(),
		TaskID:      taskID,
		Section:     section,
		Filename:    filename,
		SheetName:   sheetName,
		ColumnCount: int32(maxColumns),
		TotalRows:   int32(len(rows)),
		HeaderJSON:  headerJSON,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := r.data.pg.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if replaceOld {
			oldImports := make([]SquirrelSectionImport, 0)
			if err := tx.Where("task_id = ? AND section = ?", taskID, section).Find(&oldImports).Error; err != nil {
				return err
			}
			for i := range oldImports {
				if err := tx.Where("file_id = ?", oldImports[i].FileID).Delete(&SquirrelSectionRow{}).Error; err != nil {
					return err
				}
				if err := tx.Where("file_id = ?", oldImports[i].FileID).Delete(&SquirrelSectionImport{}).Error; err != nil {
					return err
				}
			}
		}

		if err := tx.Create(newImport).Error; err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		batch := make([]SquirrelSectionRow, 0, len(rows))
		for i := range rows {
			batch = append(batch, SquirrelSectionRow{
				FileID:     newImport.FileID,
				RowNo:      rows[i].RowNo,
				ValuesJSON: marshalStringSlice(rows[i].Values),
				CreatedAt:  now,
			})
		}
		return tx.CreateInBatches(batch, 200).Error
	})
	if err != nil {
		return nil, err
	}
	return newImport, nil
}

func (r *SquirrelRepo) ClearSection(ctx context.Context, taskID, section string) error {
	return r.data.pg.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		imports := make([]SquirrelSectionImport, 0)
		if err := tx.Where("task_id = ? AND section = ?", taskID, section).Find(&imports).Error; err != nil {
			return err
		}
		for i := range imports {
			if err := tx.Where("file_id = ?", imports[i].FileID).Delete(&SquirrelSectionRow{}).Error; err != nil {
				return err
			}
			if err := tx.Where("file_id = ?", imports[i].FileID).Delete(&SquirrelSectionImport{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *SquirrelRepo) ClearSectionByFilename(ctx context.Context, taskID, section, filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil
	}
	return r.data.pg.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		imports := make([]SquirrelSectionImport, 0)
		if err := tx.Where("task_id = ? AND section = ? AND filename = ?", taskID, section, filename).Find(&imports).Error; err != nil {
			return err
		}
		for i := range imports {
			if err := tx.Where("file_id = ?", imports[i].FileID).Delete(&SquirrelSectionRow{}).Error; err != nil {
				return err
			}
			if err := tx.Where("file_id = ?", imports[i].FileID).Delete(&SquirrelSectionImport{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *SquirrelRepo) ListSectionRows(ctx context.Context, taskID, section string, page, pageSize int) (*SectionPageResult, error) {
	imports, err := r.ListSectionImports(ctx, taskID, section)
	if err != nil {
		return nil, err
	}
	if len(imports) == 0 {
		return nil, errSectionImportNotFound
	}

	latest := imports[0]
	maxCols := int(latest.ColumnCount)
	for i := range imports {
		if int(imports[i].ColumnCount) > maxCols {
			maxCols = int(imports[i].ColumnCount)
		}
	}

	type rowItem struct {
		FileID     string
		Filename   string
		RowNo      int32
		ValuesJSON string
	}

	var total int64
	query := r.data.pg.WithContext(ctx).
		Table(sectionRowTableName+" AS r").
		Joins("JOIN "+sectionImportTableName+" AS i ON r.file_id = i.file_id").
		Where("i.task_id = ? AND i.section = ?", taskID, section)

	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	dbRows := make([]rowItem, 0, pageSize)
	err = r.data.pg.WithContext(ctx).
		Table(sectionRowTableName+" AS r").
		Select("r.file_id AS file_id, i.filename AS filename, r.row_no AS row_no, r.values_json AS values_json").
		Joins("JOIN "+sectionImportTableName+" AS i ON r.file_id = i.file_id").
		Where("i.task_id = ? AND i.section = ?", taskID, section).
		Order("i.updated_at DESC, i.file_id DESC, r.row_no ASC").
		Offset(offset).
		Limit(pageSize).
		Find(&dbRows).Error
	if err != nil {
		return nil, err
	}

	rows := make([]SquirrelSectionRowData, 0, len(dbRows))
	for i := range dbRows {
		rows = append(rows, SquirrelSectionRowData{
			FileID:   dbRows[i].FileID,
			Filename: dbRows[i].Filename,
			RowNo:    dbRows[i].RowNo,
			Values:   unmarshalStringSlice(dbRows[i].ValuesJSON),
		})
	}
	return &SectionPageResult{Import: &latest, Imports: imports, Rows: rows, Total: total, MaxCols: maxCols}, nil
}

func ParseHeader(headerJSON string) []string {
	return unmarshalStringSlice(headerJSON)
}

func NormalizeColumnNames(header []string, colCount int) []string {
	if colCount < 0 {
		colCount = 0
	}
	columns := make([]string, 0, colCount)
	for i := 0; i < colCount; i++ {
		name := ""
		if i < len(header) {
			name = strings.TrimSpace(header[i])
		}
		if name == "" {
			name = "列" + strconvItoa(i+1)
		}
		columns = append(columns, name)
	}
	return columns
}

func marshalStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	values := make([]string, 0)
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

func trimTailBlanks(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	cp := append([]string(nil), values...)
	last := len(cp) - 1
	for last >= 0 {
		if strings.TrimSpace(cp[last]) != "" {
			break
		}
		last--
	}
	if last < 0 {
		return []string{}
	}
	return cp[:last+1]
}

func strconvItoa(v int) string { return strconv.Itoa(v) }
