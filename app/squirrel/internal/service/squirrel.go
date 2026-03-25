package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/xuri/excelize/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeffinity/oculus/app/squirrel/internal/data"
	squirrelv1 "github.com/jeffinity/oculus/proto/squirrel/v1"
)

var (
	taskMonthRegex = regexp.MustCompile(`^\d{4}-\d{2}$`)
	validSections  = map[string]struct{}{
		"sales":  {},
		"wechat": {},
		"alipay": {},
	}
	sectionHeaderHints = map[string][]string{
		"sales": {
			"订单编号", "订单号", "订单来源", "店铺", "仓库", "买家", "支付", "金额",
		},
		"wechat": {
			"交易时间", "交易类型", "交易对方", "商品", "金额", "支付方式", "商户单号", "交易状态",
		},
		"alipay": {
			"订单时间", "交易号", "商户订单号", "业务类型", "商品名称", "收入", "支出", "服务费",
			"渠道", "支付状态", "业务基础订单号", "对方账号", "对方名称",
		},
	}
)

type SquirrelService struct {
	squirrelv1.UnimplementedSquirrelServiceServer

	repo *data.SquirrelRepo
	log  *log.Helper
}

func NewSquirrelService(repo *data.SquirrelRepo, logger log.Logger) *SquirrelService {
	return &SquirrelService{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "squirrel/SquirrelService")),
	}
}

func (s *SquirrelService) ListTasks(ctx context.Context, _ *squirrelv1.ListTasksRequest) (*squirrelv1.ListTasksReply, error) {
	tasks, err := s.repo.ListTasks(ctx)
	if err != nil {
		s.log.Errorf("list tasks failed: %v", err)
		return nil, status.Error(codes.Internal, "list tasks failed")
	}

	reply := &squirrelv1.ListTasksReply{Items: make([]*squirrelv1.Task, 0, len(tasks))}
	for i := range tasks {
		imports, qErr := s.repo.ListSectionImportsByTask(ctx, tasks[i].TaskID)
		if qErr != nil {
			s.log.Errorf("list section imports failed task_id=%s err=%v", tasks[i].TaskID, qErr)
			return nil, status.Error(codes.Internal, "list tasks failed")
		}
		reply.Items = append(reply.Items, toProtoTask(tasks[i], imports))
	}
	return reply, nil
}

func (s *SquirrelService) CreateTask(ctx context.Context, req *squirrelv1.CreateTaskRequest) (*squirrelv1.CreateTaskReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	title := strings.TrimSpace(req.GetTitle())
	if title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if len([]rune(title)) > 128 {
		return nil, status.Error(codes.InvalidArgument, "title too long")
	}

	monthTag := strings.TrimSpace(req.GetMonthTag())
	if monthTag == "" {
		monthTag = time.Now().Format("2006-01")
	}
	if !taskMonthRegex.MatchString(monthTag) {
		return nil, status.Error(codes.InvalidArgument, "month_tag must be YYYY-MM")
	}

	task, err := s.repo.CreateTask(ctx, title, monthTag)
	if err != nil {
		s.log.Errorf("create task failed title=%s err=%v", title, err)
		return nil, status.Error(codes.Internal, "create task failed")
	}
	return &squirrelv1.CreateTaskReply{Item: toProtoTask(*task, nil)}, nil
}

func (s *SquirrelService) DeleteTask(ctx context.Context, req *squirrelv1.DeleteTaskRequest) (*squirrelv1.DeleteTaskReply, error) {
	if req == nil || strings.TrimSpace(req.GetTaskId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		s.log.Errorf("query task failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "delete task failed")
	}
	if task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	if err = s.repo.DeleteTask(ctx, taskID); err != nil {
		s.log.Errorf("delete task failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "delete task failed")
	}
	return &squirrelv1.DeleteTaskReply{Success: true}, nil
}

func (s *SquirrelService) UploadSectionFile(ctx context.Context, req *squirrelv1.UploadSectionFileRequest) (*squirrelv1.UploadSectionFileReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	section := normalizeSection(req.GetSection())
	if !isValidSection(section) {
		return nil, status.Error(codes.InvalidArgument, "unsupported section")
	}
	filename := strings.TrimSpace(req.GetFilename())
	if filename == "" {
		return nil, status.Error(codes.InvalidArgument, "filename is required")
	}
	if strings.ToLower(filepath.Ext(filename)) != ".xlsx" {
		return nil, status.Error(codes.InvalidArgument, "only .xlsx is supported")
	}
	content := req.GetFileContent()
	if len(content) == 0 {
		return nil, status.Error(codes.InvalidArgument, "file_content is empty")
	}

	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		s.log.Errorf("query task failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "upload failed")
	}
	if task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}

	sheetName, header, rows, err := parseXLSX(content, section)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	replaceOld := section == "sales"
	item, err := s.repo.SaveSectionFile(ctx, taskID, section, filename, sheetName, header, rows, replaceOld)
	if err != nil {
		s.log.Errorf("save section file failed task_id=%s section=%s err=%v", taskID, section, err)
		return nil, status.Error(codes.Internal, "upload failed")
	}

	return &squirrelv1.UploadSectionFileReply{
		Summary: toProtoSectionSummary(*item),
	}, nil
}

func (s *SquirrelService) ListSectionRows(ctx context.Context, req *squirrelv1.ListSectionRowsRequest) (*squirrelv1.ListSectionRowsReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	section := normalizeSection(req.GetSection())
	if !isValidSection(section) {
		return nil, status.Error(codes.InvalidArgument, "unsupported section")
	}

	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	res, err := s.repo.ListSectionRows(ctx, taskID, section, page, pageSize)
	if err != nil {
		if errors.Is(err, data.ErrSectionImportNotFound()) {
			return &squirrelv1.ListSectionRowsReply{
				Rows:     []*squirrelv1.SheetRow{},
				Columns:  []string{},
				Page:     uint32(page),
				PageSize: uint32(pageSize),
				Total:    0,
			}, nil
		}
		s.log.Errorf("list section rows failed task_id=%s section=%s err=%v", taskID, section, err)
		return nil, status.Error(codes.Internal, "list section rows failed")
	}

	header := data.ParseHeader(res.Import.HeaderJSON)
	columns := data.NormalizeColumnNames(header, res.MaxCols)
	rows := make([]*squirrelv1.SheetRow, 0, len(res.Rows))
	for i := range res.Rows {
		values := padValues(res.Rows[i].Values, len(columns))
		rows = append(rows, &squirrelv1.SheetRow{
			RowNo:    uint32(res.Rows[i].RowNo),
			Values:   values,
			Filename: res.Rows[i].Filename,
		})
	}
	summaries := make([]*squirrelv1.SectionSummary, 0, len(res.Imports))
	for i := range res.Imports {
		summaries = append(summaries, toProtoSectionSummary(res.Imports[i]))
	}

	return &squirrelv1.ListSectionRowsReply{
		Summary:   toProtoSectionSummary(*res.Import),
		Summaries: summaries,
		Columns:   columns,
		Rows:      rows,
		Page:      uint32(page),
		PageSize:  uint32(pageSize),
		Total:     uint32(res.Total),
	}, nil
}

func (s *SquirrelService) ClearSectionData(ctx context.Context, req *squirrelv1.ClearSectionDataRequest) (*squirrelv1.ClearSectionDataReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	section := normalizeSection(req.GetSection())
	if !isValidSection(section) {
		return nil, status.Error(codes.InvalidArgument, "unsupported section")
	}
	filename := strings.TrimSpace(req.GetFilename())

	var err error
	if filename != "" {
		err = s.repo.ClearSectionByFilename(ctx, taskID, section, filename)
	} else {
		err = s.repo.ClearSection(ctx, taskID, section)
	}
	if err != nil {
		s.log.Errorf("clear section failed task_id=%s section=%s filename=%s err=%v", taskID, section, filename, err)
		return nil, status.Error(codes.Internal, "clear section failed")
	}
	return &squirrelv1.ClearSectionDataReply{Success: true}, nil
}

func toProtoTask(task data.SquirrelTask, imports []data.SquirrelSectionImport) *squirrelv1.Task {
	summaries := make([]*squirrelv1.SectionSummary, 0, len(imports))
	for i := range imports {
		summaries = append(summaries, toProtoSectionSummary(imports[i]))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Section < summaries[j].Section
	})
	return &squirrelv1.Task{
		TaskId:           task.TaskID,
		Title:            task.Title,
		MonthTag:         task.MonthTag,
		CreatedAt:        task.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        task.UpdatedAt.Format(time.RFC3339),
		SectionSummaries: summaries,
	}
}

func toProtoSectionSummary(item data.SquirrelSectionImport) *squirrelv1.SectionSummary {
	header := data.ParseHeader(item.HeaderJSON)
	return &squirrelv1.SectionSummary{
		Section:     item.Section,
		FileId:      item.FileID,
		Filename:    item.Filename,
		SheetName:   item.SheetName,
		TotalRows:   uint32(item.TotalRows),
		ColumnCount: uint32(item.ColumnCount),
		Header:      header,
		UpdatedAt:   item.UpdatedAt.Format(time.RFC3339),
	}
}

func parseXLSX(content []byte, section string) (string, []string, []data.ParsedRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid xlsx file")
	}
	defer func() {
		_ = f.Close()
	}()

	sheetList := f.GetSheetList()
	if len(sheetList) == 0 {
		return "", nil, nil, fmt.Errorf("xlsx has no sheet")
	}
	firstSheet := strings.TrimSpace(sheetList[0])
	if firstSheet == "" {
		return "", nil, nil, fmt.Errorf("xlsx sheet name is empty")
	}

	rows, err := f.GetRows(firstSheet)
	if err != nil {
		return "", nil, nil, fmt.Errorf("read first sheet failed")
	}

	normalizedRows := make([][]string, 0, len(rows))
	for i := range rows {
		normalizedRows = append(normalizedRows, trimTailBlanks(rows[i]))
	}
	if len(normalizedRows) == 0 {
		return firstSheet, []string{}, []data.ParsedRow{}, nil
	}

	headerRowIdx := detectHeaderRowIndex(normalizedRows, section)
	head := normalizedRows[headerRowIdx]
	requiredCols := buildRequiredColumnIndexes(section, head)
	parsedRows := make([]data.ParsedRow, 0, len(normalizedRows)-headerRowIdx-1)
	for i := headerRowIdx + 1; i < len(normalizedRows); i++ {
		row := trimTailBlanks(normalizedRows[i])
		if len(row) == 0 {
			continue
		}
		if isDuplicateHeaderRow(head, row) {
			continue
		}
		if !keepRowByRequiredColumns(row, requiredCols) {
			continue
		}
		parsedRows = append(parsedRows, data.ParsedRow{
			RowNo:  int32(i + 1),
			Values: row,
		})
	}
	return firstSheet, head, parsedRows, nil
}

func trimTailBlanks(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	end := len(values) - 1
	for end >= 0 {
		if strings.TrimSpace(values[end]) != "" {
			break
		}
		end--
	}
	if end < 0 {
		return []string{}
	}
	out := make([]string, 0, end+1)
	for i := 0; i <= end; i++ {
		out = append(out, strings.TrimSpace(values[i]))
	}
	return out
}

func padValues(values []string, length int) []string {
	if length < 0 {
		length = 0
	}
	if len(values) >= length {
		return values
	}
	out := make([]string, 0, length)
	out = append(out, values...)
	for len(out) < length {
		out = append(out, "")
	}
	return out
}

func normalizeSection(section string) string {
	return strings.ToLower(strings.TrimSpace(section))
}

func isValidSection(section string) bool {
	_, ok := validSections[section]
	return ok
}

func detectHeaderRowIndex(rows [][]string, section string) int {
	if len(rows) == 0 {
		return 0
	}
	hints := sectionHeaderHints[section]
	bestIdx := 0
	bestScore := -1
	for i := range rows {
		row := rows[i]
		if len(row) == 0 {
			continue
		}
		score := 0
		nonEmpty := 0
		for _, cell := range row {
			val := strings.TrimSpace(cell)
			if val == "" {
				continue
			}
			nonEmpty++
			for _, hint := range hints {
				if strings.Contains(val, hint) {
					score += 6
				}
			}
		}
		score += nonEmpty * 2

		joined := strings.Join(row, "")
		if strings.Contains(joined, "查询起始日期") || strings.Contains(joined, "查询终止日期") {
			score -= 6
		}
		if len(row) > 0 && strings.HasPrefix(strings.TrimSpace(row[0]), "#") {
			score -= 6
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx
}

func isDuplicateHeaderRow(header []string, row []string) bool {
	if len(header) == 0 || len(row) == 0 {
		return false
	}
	n := len(header)
	if len(row) < n {
		n = len(row)
	}
	sameCount := 0
	nonEmpty := 0
	for i := 0; i < n; i++ {
		h := strings.TrimSpace(header[i])
		r := strings.TrimSpace(row[i])
		if h == "" && r == "" {
			continue
		}
		if h != "" || r != "" {
			nonEmpty++
		}
		if normalizeHeaderToken(h) == normalizeHeaderToken(r) {
			sameCount++
		}
	}
	if nonEmpty < 3 {
		return false
	}
	return sameCount == nonEmpty
}

func normalizeHeaderToken(input string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(input) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func buildRequiredColumnIndexes(section string, header []string) []int {
	switch section {
	case "sales":
		return pickRequiredColumns(header, [][]string{
			{"订单编号", "订单号"},
			{"子单原始单号"},
		})
	case "wechat":
		return pickRequiredColumns(header, [][]string{
			{"淘宝订单编号"},
		})
	case "alipay":
		return pickRequiredColumns(header, [][]string{
			{"支付宝流水号", "支付宝交易号"},
		})
	default:
		return []int{}
	}
}

func pickRequiredColumns(header []string, groups [][]string) []int {
	out := make([]int, 0, len(groups))
	for i := range groups {
		idx := findHeaderIndexByAliases(header, groups[i])
		if idx >= 0 {
			out = append(out, idx)
		}
	}
	return out
}

func findHeaderIndexByAliases(header []string, aliases []string) int {
	if len(header) == 0 || len(aliases) == 0 {
		return -1
	}
	normalizedAliases := make([]string, 0, len(aliases))
	for i := range aliases {
		normalizedAliases = append(normalizedAliases, normalizeHeaderToken(aliases[i]))
	}
	for i := range header {
		cur := normalizeHeaderToken(header[i])
		for j := range normalizedAliases {
			if cur == normalizedAliases[j] || strings.Contains(cur, normalizedAliases[j]) {
				return i
			}
		}
	}
	return -1
}

func keepRowByRequiredColumns(row []string, indexes []int) bool {
	if len(indexes) == 0 {
		return true
	}
	for i := range indexes {
		idx := indexes[i]
		if idx < 0 || idx >= len(row) {
			return false
		}
		if strings.TrimSpace(row[idx]) == "" {
			return false
		}
	}
	return true
}
