package data

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	reconcileRunTableName = "squirrel_reconcile_runs"
	reconcileRowTableName = "squirrel_reconcile_rows"
)

const (
	ReconcileStatusPending   = "PENDING"
	ReconcileStatusRunning   = "RUNNING"
	ReconcileStatusSucceeded = "SUCCEEDED"
	ReconcileStatusFailed    = "FAILED"
)

type SquirrelReconcileRun struct {
	RunID             string     `gorm:"column:run_id;type:varchar(26);primaryKey;comment:核算任务ID"`
	TaskID            string     `gorm:"column:task_id;type:varchar(26);not null;index:idx_squirrel_reconcile_task,priority:1;comment:任务ID"`
	DataFingerprint   string     `gorm:"column:data_fingerprint;type:varchar(128);not null;index:idx_squirrel_reconcile_fingerprint,priority:2;comment:数据指纹"`
	Status            string     `gorm:"column:status;type:varchar(16);not null;default:'PENDING';index:idx_squirrel_reconcile_status,priority:3;comment:状态"`
	Progress          int32      `gorm:"column:progress;type:int;not null;default:0;comment:进度(0-100)"`
	Stage             string     `gorm:"column:stage;type:varchar(64);not null;default:'';comment:当前阶段"`
	Message           string     `gorm:"column:message;type:text;not null;default:'';comment:错误或补充信息"`
	TotalRows         int32      `gorm:"column:total_rows;type:int;not null;default:0;comment:待处理总行数"`
	ProcessedRows     int32      `gorm:"column:processed_rows;type:int;not null;default:0;comment:已处理行数"`
	MatchedRows       int32      `gorm:"column:matched_rows;type:int;not null;default:0;comment:已匹配行数"`
	ResultTotalRows   int32      `gorm:"column:result_total_rows;type:int;not null;default:0;comment:结果总行数"`
	ResultColumnsJSON string     `gorm:"column:result_columns_json;type:text;not null;default:'[]';comment:结果列JSON"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:timestamptz;not null;index:idx_squirrel_reconcile_created;comment:创建时间"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
	FinishedAt        *time.Time `gorm:"column:finished_at;type:timestamptz;comment:结束时间"`
}

func (*SquirrelReconcileRun) TableName() string { return reconcileRunTableName }

type SquirrelReconcileRow struct {
	ID          uint64    `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	RunID       string    `gorm:"column:run_id;type:varchar(26);not null;index:idx_squirrel_reconcile_rows_run_row,priority:1;comment:核算任务ID"`
	Filename    string    `gorm:"column:filename;type:varchar(255);not null;default:'';comment:文件名"`
	RowNo       int32     `gorm:"column:row_no;type:int;not null;index:idx_squirrel_reconcile_rows_run_row,priority:2;comment:原始行号"`
	ValuesJSON  string    `gorm:"column:values_json;type:text;not null;default:'[]';comment:销售行数据JSON"`
	MatchesJSON string    `gorm:"column:matches_json;type:text;not null;default:'[]';comment:匹配明细JSON"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamptz;not null;comment:创建时间"`
}

func (*SquirrelReconcileRow) TableName() string { return reconcileRowTableName }

type ReconcileKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ReconcileMatch struct {
	Source   string        `json:"source"`
	RowNo    int32         `json:"row_no"`
	Filename string        `json:"filename"`
	Detail   []ReconcileKV `json:"detail"`
}

type ReconcileRowData struct {
	Filename     string
	RowNo        int32
	Values       []string
	Matches      []ReconcileMatch
	ManualReview *SquirrelManualReview
}

func (r *SquirrelRepo) CreateReconcileRun(ctx context.Context, taskID, fingerprint string, totalRows int32, columns []string) (*SquirrelReconcileRun, error) {
	now := time.Now()
	item := &SquirrelReconcileRun{
		RunID:             ulid.Make().String(),
		TaskID:            taskID,
		DataFingerprint:   fingerprint,
		Status:            ReconcileStatusPending,
		Progress:          0,
		Stage:             "已创建",
		Message:           "",
		TotalRows:         totalRows,
		ProcessedRows:     0,
		MatchedRows:       0,
		ResultTotalRows:   0,
		ResultColumnsJSON: marshalStringSlice(columns),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := r.data.pg.WithContext(ctx).Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (r *SquirrelRepo) GetReconcileRunByID(ctx context.Context, taskID, runID string) (*SquirrelReconcileRun, error) {
	var item SquirrelReconcileRun
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ? AND run_id = ?", taskID, runID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *SquirrelRepo) GetLatestReconcileRunByTask(ctx context.Context, taskID string) (*SquirrelReconcileRun, error) {
	var item SquirrelReconcileRun
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at DESC").
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *SquirrelRepo) GetRunningReconcileRunByTask(ctx context.Context, taskID string) (*SquirrelReconcileRun, error) {
	var item SquirrelReconcileRun
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ? AND status IN ?", taskID, []string{ReconcileStatusPending, ReconcileStatusRunning}).
		Order("created_at DESC").
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *SquirrelRepo) GetLatestSucceededReconcileRunByFingerprint(ctx context.Context, taskID, fingerprint string) (*SquirrelReconcileRun, error) {
	var item SquirrelReconcileRun
	err := r.data.pg.WithContext(ctx).
		Where("task_id = ? AND data_fingerprint = ? AND status = ?", taskID, fingerprint, ReconcileStatusSucceeded).
		Order("created_at DESC").
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *SquirrelRepo) UpdateReconcileRunProgress(
	ctx context.Context,
	runID string,
	status string,
	progress int32,
	stage, msg string,
	processedRows, matchedRows int32,
) error {
	now := time.Now()
	updates := map[string]any{
		"status":         status,
		"progress":       progress,
		"stage":          stage,
		"message":        msg,
		"processed_rows": processedRows,
		"matched_rows":   matchedRows,
		"updated_at":     now,
	}
	return r.data.pg.WithContext(ctx).Model(&SquirrelReconcileRun{}).Where("run_id = ?", runID).Updates(updates).Error
}

func (r *SquirrelRepo) MarkReconcileRunFailed(ctx context.Context, runID, msg string) error {
	now := time.Now()
	updates := map[string]any{
		"status":      ReconcileStatusFailed,
		"progress":    100,
		"stage":       "执行失败",
		"message":     msg,
		"updated_at":  now,
		"finished_at": &now,
	}
	return r.data.pg.WithContext(ctx).Model(&SquirrelReconcileRun{}).Where("run_id = ?", runID).Updates(updates).Error
}

func (r *SquirrelRepo) ReplaceReconcileRowsAndMarkSuccess(
	ctx context.Context,
	runID string,
	rows []ReconcileRowData,
	matchedRows int32,
	totalRows int32,
	columns []string,
) error {
	now := time.Now()
	return r.data.pg.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).Delete(&SquirrelReconcileRow{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			batch := make([]SquirrelReconcileRow, 0, len(rows))
			for i := range rows {
				batch = append(batch, SquirrelReconcileRow{
					RunID:       runID,
					Filename:    rows[i].Filename,
					RowNo:       rows[i].RowNo,
					ValuesJSON:  marshalStringSlice(rows[i].Values),
					MatchesJSON: marshalReconcileMatches(rows[i].Matches),
					CreatedAt:   now,
				})
			}
			if err := tx.CreateInBatches(batch, 200).Error; err != nil {
				return err
			}
		}
		return tx.Model(&SquirrelReconcileRun{}).
			Where("run_id = ?", runID).
			Updates(map[string]any{
				"status":              ReconcileStatusSucceeded,
				"progress":            100,
				"stage":               "处理完成",
				"message":             "",
				"processed_rows":      totalRows,
				"matched_rows":        matchedRows,
				"result_total_rows":   totalRows,
				"result_columns_json": marshalStringSlice(columns),
				"updated_at":          now,
				"finished_at":         &now,
			}).Error
	})
}

func (r *SquirrelRepo) ListReconcileRows(ctx context.Context, runID string, page, pageSize int) ([]ReconcileRowData, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 300 {
		pageSize = 300
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := r.data.pg.WithContext(ctx).
		Model(&SquirrelReconcileRow{}).
		Where("run_id = ?", runID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	dbRows := make([]SquirrelReconcileRow, 0, pageSize)
	if err := r.data.pg.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("row_no ASC").
		Offset(offset).
		Limit(pageSize).
		Find(&dbRows).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]ReconcileRowData, 0, len(dbRows))
	for i := range dbRows {
		rows = append(rows, ReconcileRowData{
			Filename: dbRows[i].Filename,
			RowNo:    dbRows[i].RowNo,
			Values:   unmarshalStringSlice(dbRows[i].ValuesJSON),
			Matches:  unmarshalReconcileMatches(dbRows[i].MatchesJSON),
		})
	}
	return rows, total, nil
}

func (r *SquirrelRepo) ListAllReconcileRows(ctx context.Context, runID string) ([]ReconcileRowData, error) {
	dbRows := make([]SquirrelReconcileRow, 0, 1024)
	if err := r.data.pg.WithContext(ctx).
		Where("run_id = ?", runID).
		Order("row_no ASC").
		Find(&dbRows).Error; err != nil {
		return nil, err
	}

	rows := make([]ReconcileRowData, 0, len(dbRows))
	for i := range dbRows {
		rows = append(rows, ReconcileRowData{
			Filename: dbRows[i].Filename,
			RowNo:    dbRows[i].RowNo,
			Values:   unmarshalStringSlice(dbRows[i].ValuesJSON),
			Matches:  unmarshalReconcileMatches(dbRows[i].MatchesJSON),
		})
	}
	return rows, nil
}

func (r *SquirrelRepo) UpsertManualReview(
	ctx context.Context,
	taskID, filename string,
	rowNo int32,
	refundQty, refundAmount, remark string,
	ignored bool,
) (*SquirrelManualReview, error) {
	now := time.Now()
	item := &SquirrelManualReview{
		TaskID:       taskID,
		Filename:     strings.TrimSpace(filename),
		RowNo:        rowNo,
		RefundQty:    strings.TrimSpace(refundQty),
		RefundAmount: strings.TrimSpace(refundAmount),
		SettleQty:    "",
		SettleAmount: "",
		Remark:       strings.TrimSpace(remark),
		ReviewType:   ManualReviewTypeManual,
		Ignored:      ignored,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := r.data.pg.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "task_id"},
					{Name: "filename"},
					{Name: "row_no"},
				},
				DoUpdates: clause.Assignments(map[string]any{
					"refund_qty":    item.RefundQty,
					"refund_amount": item.RefundAmount,
					"settle_qty":    item.SettleQty,
					"settle_amount": item.SettleAmount,
					"remark":        item.Remark,
					"review_type":   item.ReviewType,
					"ignored":       item.Ignored,
					"updated_at":    now,
				}),
			}).
			Create(item).Error; err != nil {
			return err
		}
		if item.Remark == "" {
			return nil
		}
		remarkItem := &SquirrelManualRemarkOption{
			TaskID:    item.TaskID,
			Content:   item.Remark,
			UsedCount: 1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "task_id"},
				{Name: "content"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"used_count": gorm.Expr(manualRemarkOptionTableName + ".used_count + 1"),
				"updated_at": now,
			}),
		}).Create(remarkItem).Error
	}); err != nil {
		return nil, err
	}
	out := &SquirrelManualReview{}
	if err := r.data.pg.WithContext(ctx).
		Where("task_id = ? AND filename = ? AND row_no = ?", taskID, item.Filename, rowNo).
		First(out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SquirrelRepo) ListManualRemarkOptions(ctx context.Context, taskID string, limit int) ([]SquirrelManualRemarkOption, error) {
	if limit <= 0 {
		limit = 20
	}
	items := make([]SquirrelManualRemarkOption, 0, limit)
	if err := r.data.pg.WithContext(ctx).
		Where("task_id = ?", strings.TrimSpace(taskID)).
		Order("used_count DESC").
		Order("updated_at DESC").
		Order("id DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SquirrelRepo) DeleteManualRemarkOption(ctx context.Context, taskID, content string) error {
	return r.data.pg.WithContext(ctx).
		Where("task_id = ? AND content = ?", strings.TrimSpace(taskID), strings.TrimSpace(content)).
		Delete(&SquirrelManualRemarkOption{}).Error
}

func (r *SquirrelRepo) BatchUpsertManualReviews(ctx context.Context, items []SquirrelManualReview) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now()
	batch := make([]SquirrelManualReview, 0, len(items))
	for i := range items {
		it := items[i]
		it.Filename = strings.TrimSpace(it.Filename)
		it.RefundQty = strings.TrimSpace(it.RefundQty)
		it.RefundAmount = strings.TrimSpace(it.RefundAmount)
		it.SettleQty = strings.TrimSpace(it.SettleQty)
		it.SettleAmount = strings.TrimSpace(it.SettleAmount)
		it.Remark = strings.TrimSpace(it.Remark)
		it.ReviewType = strings.TrimSpace(it.ReviewType)
		if it.ReviewType == "" {
			it.ReviewType = ManualReviewTypeManual
		}
		it.CreatedAt = now
		it.UpdatedAt = now
		batch = append(batch, it)
	}
	return r.data.pg.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "task_id"},
				{Name: "filename"},
				{Name: "row_no"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"refund_qty":    gorm.Expr("EXCLUDED.refund_qty"),
				"refund_amount": gorm.Expr("EXCLUDED.refund_amount"),
				"settle_qty":    gorm.Expr("EXCLUDED.settle_qty"),
				"settle_amount": gorm.Expr("EXCLUDED.settle_amount"),
				"remark":        gorm.Expr("EXCLUDED.remark"),
				"review_type":   gorm.Expr("EXCLUDED.review_type"),
				"ignored":       gorm.Expr("EXCLUDED.ignored"),
				"updated_at":    now,
			}),
		}).
		CreateInBatches(batch, 200).Error
}

func (r *SquirrelRepo) ClearManualReviewsByTask(ctx context.Context, taskID string) error {
	return r.data.pg.WithContext(ctx).
		Where("task_id = ? AND review_type = ?", taskID, ManualReviewTypeManual).
		Delete(&SquirrelManualReview{}).Error
}

func (r *SquirrelRepo) ListManualReviewsByRows(ctx context.Context, taskID string, rows []ReconcileRowData) (map[string]SquirrelManualReview, error) {
	if len(rows) == 0 {
		return map[string]SquirrelManualReview{}, nil
	}
	clauses := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*2+1)
	args = append(args, taskID)
	seen := map[string]struct{}{}
	for i := range rows {
		key := buildManualReviewKey(rows[i].Filename, rows[i].RowNo)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		clauses = append(clauses, "(filename = ? AND row_no = ?)")
		args = append(args, rows[i].Filename, rows[i].RowNo)
	}
	if len(clauses) == 0 {
		return map[string]SquirrelManualReview{}, nil
	}
	items := make([]SquirrelManualReview, 0, len(clauses))
	sql := "task_id = ? AND (" + strings.Join(clauses, " OR ") + ")"
	if err := r.data.pg.WithContext(ctx).Where(sql, args...).Find(&items).Error; err != nil {
		return nil, err
	}
	out := make(map[string]SquirrelManualReview, len(items))
	for i := range items {
		out[buildManualReviewKey(items[i].Filename, items[i].RowNo)] = items[i]
	}
	return out, nil
}

func buildManualReviewKey(filename string, rowNo int32) string {
	return strings.TrimSpace(filename) + "|" + strconv.FormatInt(int64(rowNo), 10)
}

func (r *SquirrelRepo) ListAllSectionRowsByFileID(ctx context.Context, fileID string) ([]SquirrelSectionRowData, error) {
	var item SquirrelSectionImport
	if err := r.data.pg.WithContext(ctx).Where("file_id = ?", fileID).First(&item).Error; err != nil {
		return nil, err
	}
	dbRows := make([]SquirrelSectionRow, 0, 1024)
	if err := r.data.pg.WithContext(ctx).
		Where("file_id = ?", fileID).
		Order("row_no ASC").
		Find(&dbRows).Error; err != nil {
		return nil, err
	}
	rows := make([]SquirrelSectionRowData, 0, len(dbRows))
	for i := range dbRows {
		rows = append(rows, SquirrelSectionRowData{
			FileID:   dbRows[i].FileID,
			Filename: item.Filename,
			RowNo:    dbRows[i].RowNo,
			Values:   unmarshalStringSlice(dbRows[i].ValuesJSON),
		})
	}
	return rows, nil
}

func marshalReconcileMatches(items []ReconcileMatch) string {
	if len(items) == 0 {
		return "[]"
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalReconcileMatches(raw string) []ReconcileMatch {
	if raw == "" {
		return []ReconcileMatch{}
	}
	items := make([]ReconcileMatch, 0)
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return []ReconcileMatch{}
	}
	return items
}
