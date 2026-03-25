package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeffinity/oculus/app/squirrel/internal/data"
	squirrelv1 "github.com/jeffinity/oculus/proto/squirrel/v1"
)

var alipayAllowedBizDesc = map[string]struct{}{
	"0010001|交易收款-交易收款":  {},
	"0020001|交易退款-余额退款":  {},
	"0020002|交易退款-保证金退款": {},
}

var orderNoRegex = regexp.MustCompile(`订单编号[:：]\s*([A-Za-z0-9]+)`)
var headerParenRegex = regexp.MustCompile(`[（(].*?[)）]`)
var reconcileFingerprintVersion = "v2-multi-match"

type indexedReconcileRow struct {
	Idx int
	Row data.ReconcileRowData
}

type reconcileMatchItem struct {
	Source   string
	RowNo    int32
	Filename string
	Detail   []data.ReconcileKV
}

func (s *SquirrelService) StartReconcile(ctx context.Context, req *squirrelv1.StartReconcileRequest) (*squirrelv1.StartReconcileReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		s.log.Errorf("query task failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "start reconcile failed")
	}
	if task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}

	salesImport, wechatImports, alipayImports, err := s.loadRequiredSectionImports(ctx, taskID)
	if err != nil {
		return nil, err
	}
	fingerprint := buildReconcileFingerprint(taskID, salesImport.FileID, collectFileIDs(wechatImports), collectFileIDs(alipayImports))

	if !req.GetForce() {
		cached, qErr := s.repo.GetLatestSucceededReconcileRunByFingerprint(ctx, taskID, fingerprint)
		if qErr != nil {
			s.log.Errorf("query cached reconcile failed task_id=%s err=%v", taskID, qErr)
			return nil, status.Error(codes.Internal, "start reconcile failed")
		}
		if cached != nil {
			return &squirrelv1.StartReconcileReply{Run: toProtoReconcileRun(*cached, true)}, nil
		}
	}

	running, err := s.repo.GetRunningReconcileRunByTask(ctx, taskID)
	if err != nil {
		s.log.Errorf("query running reconcile failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "start reconcile failed")
	}
	if running != nil {
		return &squirrelv1.StartReconcileReply{Run: toProtoReconcileRun(*running, false)}, nil
	}

	salesColumns := data.NormalizeColumnNames(data.ParseHeader(salesImport.HeaderJSON), int(salesImport.ColumnCount))
	run, err := s.repo.CreateReconcileRun(ctx, taskID, fingerprint, salesImport.TotalRows, salesColumns)
	if err != nil {
		s.log.Errorf("create reconcile run failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "start reconcile failed")
	}

	go s.executeReconcileRun(run.RunID, taskID, *salesImport, wechatImports, alipayImports, salesColumns)
	return &squirrelv1.StartReconcileReply{Run: toProtoReconcileRun(*run, false)}, nil
}

func (s *SquirrelService) GetReconcileStatus(ctx context.Context, req *squirrelv1.GetReconcileStatusRequest) (*squirrelv1.GetReconcileStatusReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	var (
		run *data.SquirrelReconcileRun
		err error
	)
	runID := strings.TrimSpace(req.GetRunId())
	if runID != "" {
		run, err = s.repo.GetReconcileRunByID(ctx, taskID, runID)
	} else {
		run, err = s.repo.GetLatestReconcileRunByTask(ctx, taskID)
	}
	if err != nil {
		s.log.Errorf("get reconcile status failed task_id=%s run_id=%s err=%v", taskID, runID, err)
		return nil, status.Error(codes.Internal, "get reconcile status failed")
	}
	if run == nil {
		return nil, status.Error(codes.NotFound, "reconcile run not found")
	}
	return &squirrelv1.GetReconcileStatusReply{Run: toProtoReconcileRun(*run, false)}, nil
}

func (s *SquirrelService) ListReconcileRows(ctx context.Context, req *squirrelv1.ListReconcileRowsRequest) (*squirrelv1.ListReconcileRowsReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	var (
		run *data.SquirrelReconcileRun
		err error
	)
	runID := strings.TrimSpace(req.GetRunId())
	if runID != "" {
		run, err = s.repo.GetReconcileRunByID(ctx, taskID, runID)
	} else {
		run, err = s.repo.GetLatestReconcileRunByTask(ctx, taskID)
	}
	if err != nil {
		s.log.Errorf("query reconcile run failed task_id=%s run_id=%s err=%v", taskID, runID, err)
		return nil, status.Error(codes.Internal, "list reconcile rows failed")
	}
	if run == nil {
		return nil, status.Error(codes.NotFound, "reconcile run not found")
	}
	if run.Status != data.ReconcileStatusSucceeded {
		return nil, status.Error(codes.FailedPrecondition, "reconcile is not completed")
	}

	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 300 {
		pageSize = 300
	}

	rows, total, err := s.repo.ListReconcileRows(ctx, run.RunID, page, pageSize)
	if err != nil {
		s.log.Errorf("list reconcile rows failed run_id=%s err=%v", run.RunID, err)
		return nil, status.Error(codes.Internal, "list reconcile rows failed")
	}
	salesImport, importErr := s.repo.GetSectionImport(ctx, taskID, "sales")
	if importErr != nil {
		s.log.Errorf("query sales import failed task_id=%s err=%v", taskID, importErr)
		return nil, status.Error(codes.Internal, "list reconcile rows failed")
	}
	defaultFilename := ""
	if salesImport != nil {
		defaultFilename = strings.TrimSpace(salesImport.Filename)
	}
	if defaultFilename != "" {
		for i := range rows {
			if strings.TrimSpace(rows[i].Filename) == "" {
				rows[i].Filename = defaultFilename
			}
		}
	}
	manualMap, err := s.repo.ListManualReviewsByRows(ctx, taskID, rows)
	if err != nil {
		s.log.Errorf("list manual reviews failed task_id=%s run_id=%s err=%v", taskID, run.RunID, err)
		return nil, status.Error(codes.Internal, "list reconcile rows failed")
	}
	for i := range rows {
		if it, ok := manualMap[buildManualReviewKey(rows[i].Filename, rows[i].RowNo)]; ok {
			cp := it
			rows[i].ManualReview = &cp
		}
	}

	pbRows := make([]*squirrelv1.ReconcileRow, 0, len(rows))
	for i := range rows {
		pbRows = append(pbRows, toProtoReconcileRow(rows[i]))
	}
	columns := data.ParseHeader(run.ResultColumnsJSON)
	return &squirrelv1.ListReconcileRowsReply{
		Run:      toProtoReconcileRun(*run, false),
		Columns:  columns,
		Rows:     pbRows,
		Page:     uint32(page),
		PageSize: uint32(pageSize),
		Total:    uint32(total),
	}, nil
}

func (s *SquirrelService) executeReconcileRun(
	runID, taskID string,
	salesImport data.SquirrelSectionImport,
	wechatImports, alipayImports []data.SquirrelSectionImport,
	salesColumns []string,
) {
	ctx := context.Background()
	fail := func(err error) {
		msg := err.Error()
		_ = s.repo.MarkReconcileRunFailed(ctx, runID, msg)
		s.log.Errorf("reconcile failed run_id=%s task_id=%s err=%v", runID, taskID, err)
	}
	defer func() {
		if r := recover(); r != nil {
			fail(fmt.Errorf("reconcile panic: %v", r))
		}
	}()

	if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 5, "加载数据", "", 0, 0); err != nil {
		s.log.Errorf("update reconcile progress failed run_id=%s err=%v", runID, err)
		return
	}

	var (
		salesRows  []data.SquirrelSectionRowData
		wechatRows []data.SquirrelSectionRowData
		alipayRows []data.SquirrelSectionRowData
		loadErr    error
		mu         sync.Mutex
		wg         sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		rows, err := s.repo.ListAllSectionRowsByFileID(ctx, salesImport.FileID)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && loadErr == nil {
			loadErr = fmt.Errorf("load sales rows failed")
			return
		}
		salesRows = rows
	}()
	go func() {
		defer wg.Done()
		rows, err := s.loadRowsByImports(ctx, wechatImports)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && loadErr == nil {
			loadErr = fmt.Errorf("load wechat rows failed")
			return
		}
		wechatRows = rows
	}()
	go func() {
		defer wg.Done()
		rows, err := s.loadRowsByImports(ctx, alipayImports)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && loadErr == nil {
			loadErr = fmt.Errorf("load alipay rows failed")
			return
		}
		alipayRows = rows
	}()
	wg.Wait()
	if loadErr != nil {
		fail(loadErr)
		return
	}

	wechatColumns := columnsFromImports(wechatImports)
	alipayColumns := columnsFromImports(alipayImports)
	wechatMap := make(map[string][]reconcileMatchItem, len(wechatRows))
	alipayMap := make(map[string][]reconcileMatchItem, len(alipayRows))

	if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 28, "构建支付索引", "", 0, 0); err != nil {
		s.log.Errorf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}

	var (
		mapErr error
		mapMu  sync.Mutex
	)
	wg = sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		idxOrderNo := findColumnIndexForReconcile(wechatColumns, []string{"淘宝订单编号"})
		idxIncomeType := findColumnIndexForReconcile(wechatColumns, []string{"入账类型"})
		if idxOrderNo < 0 || idxIncomeType < 0 {
			mapMu.Lock()
			if mapErr == nil {
				mapErr = fmt.Errorf("missing wechat key columns")
			}
			mapMu.Unlock()
			return
		}
		for i := range wechatRows {
			incomeType := strings.TrimSpace(valueAt(wechatRows[i].Values, idxIncomeType))
			if incomeType != "交易收款" && incomeType != "交易退款(售后)" {
				continue
			}
			key := normalizeOrderNoForReconcile(valueAt(wechatRows[i].Values, idxOrderNo))
			if key == "" {
				continue
			}
			wechatMap[key] = append(wechatMap[key], reconcileMatchItem{
				Source:   "wechat",
				RowNo:    wechatRows[i].RowNo,
				Filename: wechatRows[i].Filename,
				Detail:   toReconcileDetail(wechatColumns, wechatRows[i].Values),
			})
		}
	}()
	go func() {
		defer wg.Done()
		idxOrderNo := findColumnIndexForReconcile(alipayColumns, []string{"业务基础订单号"})
		idxBizDesc := findColumnIndexForReconcile(alipayColumns, []string{"业务描述"})
		idxRemark := findColumnIndexForReconcile(alipayColumns, []string{"备注"})
		if idxOrderNo < 0 || idxBizDesc < 0 || idxRemark < 0 {
			mapMu.Lock()
			if mapErr == nil {
				mapErr = fmt.Errorf("missing alipay key columns")
			}
			mapMu.Unlock()
			return
		}
		for i := range alipayRows {
			bizDesc := strings.TrimSpace(valueAt(alipayRows[i].Values, idxBizDesc))
			remark := strings.TrimSpace(valueAt(alipayRows[i].Values, idxRemark))
			baseOrderNo := normalizeOrderNoForReconcile(valueAt(alipayRows[i].Values, idxOrderNo))
			_, descHit := alipayAllowedBizDesc[bizDesc]
			keywordHit := strings.Contains(remark, "扣款用途：基金代发任务")
			if !descHit && !keywordHit {
				continue
			}

			keys := map[string]struct{}{}
			if descHit && baseOrderNo != "" {
				keys[baseOrderNo] = struct{}{}
			}
			if keywordHit {
				matches := orderNoRegex.FindAllStringSubmatch(remark, -1)
				for _, m := range matches {
					if len(m) > 1 {
						no := normalizeOrderNoForReconcile(m[1])
						if no != "" {
							keys[no] = struct{}{}
						}
					}
				}
			}
			for k := range keys {
				alipayMap[k] = append(alipayMap[k], reconcileMatchItem{
					Source:   "alipay",
					RowNo:    alipayRows[i].RowNo,
					Filename: alipayRows[i].Filename,
					Detail:   toReconcileDetail(alipayColumns, alipayRows[i].Values),
				})
			}
		}
	}()
	wg.Wait()
	if mapErr != nil {
		fail(mapErr)
		return
	}

	if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 55, "并行匹配销售数据", "", 0, 0); err != nil {
		s.log.Errorf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}

	salesOrderIdx := findColumnIndexForReconcile(salesColumns, []string{"子单原始单号"})
	if salesOrderIdx < 0 {
		fail(fmt.Errorf("missing sales key column"))
		return
	}

	workerCount := runtime.NumCPU()
	if workerCount < 2 {
		workerCount = 2
	}
	if workerCount > 8 {
		workerCount = 8
	}
	jobs := make(chan indexedReconcileRow, workerCount*2)
	results := make(chan indexedReconcileRow, workerCount*2)

	for i := 0; i < workerCount; i++ {
		go func() {
			for job := range jobs {
				orderNo := normalizeOrderNoForReconcile(valueAt(job.Row.Values, salesOrderIdx))
				matches := make([]data.ReconcileMatch, 0, 2)
				if items, ok := wechatMap[orderNo]; ok {
					for _, it := range items {
						matches = append(matches, data.ReconcileMatch{
							Source:   it.Source,
							RowNo:    it.RowNo,
							Filename: it.Filename,
							Detail:   it.Detail,
						})
					}
				}
				if items, ok := alipayMap[orderNo]; ok {
					for _, it := range items {
						matches = append(matches, data.ReconcileMatch{
							Source:   it.Source,
							RowNo:    it.RowNo,
							Filename: it.Filename,
							Detail:   it.Detail,
						})
					}
				}
				row := data.ReconcileRowData{
					Filename: job.Row.Filename,
					RowNo:    job.Row.RowNo,
					Values:   padValues(job.Row.Values, len(salesColumns)),
					Matches:  matches,
				}
				results <- indexedReconcileRow{Idx: job.Idx, Row: row}
			}
		}()
	}

	go func() {
		for i := range salesRows {
			jobs <- indexedReconcileRow{
				Idx: i,
				Row: data.ReconcileRowData{
					Filename: salesRows[i].Filename,
					RowNo:    salesRows[i].RowNo,
					Values:   salesRows[i].Values,
				},
			}
		}
		close(jobs)
	}()

	outRows := make([]data.ReconcileRowData, len(salesRows))
	var matchedRows int32
	for i := 0; i < len(salesRows); i++ {
		item := <-results
		outRows[item.Idx] = item.Row
		if len(item.Row.Matches) > 0 {
			matchedRows++
		}
		processed := int32(i + 1)
		if processed%300 == 0 || processed == int32(len(salesRows)) {
			progress := int32(55 + (processed * 40 / maxInt32(int32(len(salesRows)), 1)))
			if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, progress, "并行匹配销售数据", "", processed, matchedRows); err != nil {
				s.log.Warnf("update reconcile progress failed run_id=%s err=%v", runID, err)
			}
		}
	}
	close(results)

	sort.Slice(outRows, func(i, j int) bool { return outRows[i].RowNo < outRows[j].RowNo })

	if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 97, "写入核算结果", "", int32(len(outRows)), matchedRows); err != nil {
		s.log.Warnf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}
	if err := s.repo.ReplaceReconcileRowsAndMarkSuccess(ctx, runID, outRows, matchedRows, int32(len(outRows)), salesColumns); err != nil {
		fail(fmt.Errorf("save reconcile result failed"))
		return
	}
	autoReviews := buildAutoManualReviews(taskID, salesImport.Filename, outRows, salesColumns)
	if len(autoReviews) > 0 {
		if err := s.repo.BatchUpsertManualReviews(ctx, autoReviews); err != nil {
			fail(fmt.Errorf("save auto manual reviews failed"))
			return
		}
	}
}

func (s *SquirrelService) loadRequiredSectionImports(
	ctx context.Context,
	taskID string,
) (*data.SquirrelSectionImport, []data.SquirrelSectionImport, []data.SquirrelSectionImport, error) {
	salesImport, err := s.repo.GetSectionImport(ctx, taskID, "sales")
	if err != nil {
		s.log.Errorf("query sales import failed task_id=%s err=%v", taskID, err)
		return nil, nil, nil, status.Error(codes.Internal, "query section data failed")
	}
	wechatImports, err := s.repo.ListSectionImports(ctx, taskID, "wechat")
	if err != nil {
		s.log.Errorf("query wechat import failed task_id=%s err=%v", taskID, err)
		return nil, nil, nil, status.Error(codes.Internal, "query section data failed")
	}
	alipayImports, err := s.repo.ListSectionImports(ctx, taskID, "alipay")
	if err != nil {
		s.log.Errorf("query alipay import failed task_id=%s err=%v", taskID, err)
		return nil, nil, nil, status.Error(codes.Internal, "query section data failed")
	}
	missing := make([]string, 0, 3)
	if salesImport == nil {
		missing = append(missing, "销售明细")
	}
	if len(wechatImports) == 0 {
		missing = append(missing, "微信支付明细")
	}
	if len(alipayImports) == 0 {
		missing = append(missing, "支付宝支付明细")
	}
	if len(missing) > 0 {
		return nil, nil, nil, status.Error(codes.FailedPrecondition, "请先上传"+strings.Join(missing, "、")+"后再核算")
	}
	return salesImport, wechatImports, alipayImports, nil
}

func (s *SquirrelService) loadRowsByImports(ctx context.Context, imports []data.SquirrelSectionImport) ([]data.SquirrelSectionRowData, error) {
	rows := make([]data.SquirrelSectionRowData, 0, 1024)
	for i := range imports {
		part, err := s.repo.ListAllSectionRowsByFileID(ctx, imports[i].FileID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, part...)
	}
	return rows, nil
}

func toProtoReconcileRun(item data.SquirrelReconcileRun, reused bool) *squirrelv1.ReconcileRun {
	finishedAt := ""
	if item.FinishedAt != nil {
		finishedAt = item.FinishedAt.Format(time.RFC3339)
	}
	return &squirrelv1.ReconcileRun{
		RunId:           item.RunID,
		TaskId:          item.TaskID,
		Status:          item.Status,
		Progress:        uint32(item.Progress),
		Stage:           item.Stage,
		Message:         item.Message,
		Reused:          reused,
		DataFingerprint: item.DataFingerprint,
		TotalRows:       uint32(item.TotalRows),
		ProcessedRows:   uint32(item.ProcessedRows),
		MatchedRows:     uint32(item.MatchedRows),
		ResultTotalRows: uint32(item.ResultTotalRows),
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.Format(time.RFC3339),
		FinishedAt:      finishedAt,
	}
}

func toProtoReconcileRow(item data.ReconcileRowData) *squirrelv1.ReconcileRow {
	matches := make([]*squirrelv1.ReconcileMatch, 0, len(item.Matches))
	for i := range item.Matches {
		detail := make([]*squirrelv1.KeyValue, 0, len(item.Matches[i].Detail))
		for j := range item.Matches[i].Detail {
			detail = append(detail, &squirrelv1.KeyValue{
				Key:   item.Matches[i].Detail[j].Key,
				Value: item.Matches[i].Detail[j].Value,
			})
		}
		matches = append(matches, &squirrelv1.ReconcileMatch{
			Source:   item.Matches[i].Source,
			RowNo:    uint32(item.Matches[i].RowNo),
			Detail:   detail,
			Filename: item.Matches[i].Filename,
		})
	}
	return &squirrelv1.ReconcileRow{
		RowNo:        uint32(item.RowNo),
		Values:       item.Values,
		Matches:      matches,
		Filename:     item.Filename,
		ManualReview: toProtoManualReview(item.ManualReview),
	}
}

func toProtoManualReview(item *data.SquirrelManualReview) *squirrelv1.ManualReview {
	if item == nil {
		return nil
	}
	return &squirrelv1.ManualReview{
		TaskId:       item.TaskID,
		Filename:     item.Filename,
		RowNo:        uint32(item.RowNo),
		RefundQty:    item.RefundQty,
		RefundAmount: item.RefundAmount,
		SettleQty:    item.SettleQty,
		SettleAmount: item.SettleAmount,
		Ignored:      item.Ignored,
		UpdatedAt:    item.UpdatedAt.Format(time.RFC3339),
		ReviewType:   normalizeManualReviewType(item.ReviewType),
		Remark:       item.Remark,
	}
}

func toProtoManualRemarkOption(item data.SquirrelManualRemarkOption) *squirrelv1.ManualRemarkOption {
	return &squirrelv1.ManualRemarkOption{
		TaskId:    item.TaskID,
		Content:   item.Content,
		UsedCount: item.UsedCount,
		UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
	}
}

func buildManualReviewKey(filename string, rowNo int32) string {
	return strings.TrimSpace(filename) + "|" + fmt.Sprintf("%d", rowNo)
}

func buildAutoManualReviews(
	taskID string,
	defaultFilename string,
	rows []data.ReconcileRowData,
	salesColumns []string,
) []data.SquirrelManualReview {
	idxShipQty := findColumnIndexForReconcile(salesColumns, []string{"实发数量"})
	if idxShipQty < 0 || len(rows) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]data.SquirrelManualReview, 0, len(rows)/4)
	for i := range rows {
		if len(rows[i].Matches) > 0 {
			continue
		}
		shipQty := parseDecimalForAutoReview(valueAt(rows[i].Values, idxShipQty))
		if shipQty != 0 {
			continue
		}
		filename := strings.TrimSpace(rows[i].Filename)
		if filename == "" {
			filename = strings.TrimSpace(defaultFilename)
		}
		if filename == "" || rows[i].RowNo <= 0 {
			continue
		}
		k := buildManualReviewKey(filename, rows[i].RowNo)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, data.SquirrelManualReview{
			TaskID:       taskID,
			Filename:     filename,
			RowNo:        rows[i].RowNo,
			RefundQty:    "0",
			RefundAmount: "0",
			SettleQty:    "0",
			SettleAmount: "0",
			ReviewType:   data.ManualReviewTypeAuto,
			Ignored:      false,
		})
	}
	return out
}

func normalizeManualReviewType(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case data.ManualReviewTypeAuto:
		return data.ManualReviewTypeAuto
	default:
		return data.ManualReviewTypeManual
	}
}

func parseDecimalForAutoReview(raw string) float64 {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	if v == -0 {
		return 0
	}
	return v
}

func (s *SquirrelService) UpsertManualReview(ctx context.Context, req *squirrelv1.UpsertManualReviewRequest) (*squirrelv1.UpsertManualReviewReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	filename := strings.TrimSpace(req.GetFilename())
	if filename == "" {
		salesImport, err := s.repo.GetSectionImport(ctx, taskID, "sales")
		if err != nil {
			s.log.Errorf("query sales import failed task_id=%s err=%v", taskID, err)
			return nil, status.Error(codes.Internal, "save manual review failed")
		}
		if salesImport != nil {
			filename = strings.TrimSpace(salesImport.Filename)
		}
	}
	if filename == "" {
		return nil, status.Error(codes.InvalidArgument, "filename is required")
	}
	rowNo := int32(req.GetRowNo())
	if rowNo <= 0 {
		return nil, status.Error(codes.InvalidArgument, "row_no is required")
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		s.log.Errorf("query task failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "save manual review failed")
	}
	if task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	item, err := s.repo.UpsertManualReview(
		ctx,
		taskID,
		filename,
		rowNo,
		strings.TrimSpace(req.GetRefundQty()),
		strings.TrimSpace(req.GetRefundAmount()),
		strings.TrimSpace(req.GetRemark()),
		req.GetIgnored(),
	)
	if err != nil {
		s.log.Errorf("upsert manual review failed task_id=%s filename=%s row_no=%d err=%v", taskID, filename, rowNo, err)
		return nil, status.Error(codes.Internal, "save manual review failed")
	}
	return &squirrelv1.UpsertManualReviewReply{Review: toProtoManualReview(item)}, nil
}

func (s *SquirrelService) ClearManualReviews(ctx context.Context, req *squirrelv1.ClearManualReviewsRequest) (*squirrelv1.ClearManualReviewsReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if err := s.repo.ClearManualReviewsByTask(ctx, taskID); err != nil {
		s.log.Errorf("clear manual reviews failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "clear manual reviews failed")
	}
	return &squirrelv1.ClearManualReviewsReply{Success: true}, nil
}

func (s *SquirrelService) ListManualRemarkOptions(ctx context.Context, req *squirrelv1.ListManualRemarkOptionsRequest) (*squirrelv1.ListManualRemarkOptionsReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	items, err := s.repo.ListManualRemarkOptions(ctx, taskID, 20)
	if err != nil {
		s.log.Errorf("list manual remark options failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "list manual remark options failed")
	}
	out := make([]*squirrelv1.ManualRemarkOption, 0, len(items))
	for i := range items {
		out = append(out, toProtoManualRemarkOption(items[i]))
	}
	return &squirrelv1.ListManualRemarkOptionsReply{Items: out}, nil
}

func (s *SquirrelService) DeleteManualRemarkOption(ctx context.Context, req *squirrelv1.DeleteManualRemarkOptionRequest) (*squirrelv1.DeleteManualRemarkOptionReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	taskID := strings.TrimSpace(req.GetTaskId())
	if taskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	content := strings.TrimSpace(req.GetContent())
	if content == "" {
		return nil, status.Error(codes.InvalidArgument, "content is required")
	}
	if err := s.repo.DeleteManualRemarkOption(ctx, taskID, content); err != nil {
		s.log.Errorf("delete manual remark option failed task_id=%s err=%v", taskID, err)
		return nil, status.Error(codes.Internal, "delete manual remark option failed")
	}
	return &squirrelv1.DeleteManualRemarkOptionReply{Success: true}, nil
}

func buildReconcileFingerprint(taskID, salesFileID string, wechatFileIDs, alipayFileIDs []string) string {
	raw := strings.Join([]string{
		"algo:" + reconcileFingerprintVersion,
		taskID,
		salesFileID,
		"wechat:" + strings.Join(append([]string(nil), wechatFileIDs...), ","),
		"alipay:" + strings.Join(append([]string(nil), alipayFileIDs...), ","),
	}, "|")
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func collectFileIDs(items []data.SquirrelSectionImport) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, items[i].FileID)
	}
	sort.Strings(out)
	return out
}

func columnsFromImports(items []data.SquirrelSectionImport) []string {
	maxCols := 0
	var header []string
	for i := range items {
		if int(items[i].ColumnCount) > maxCols {
			maxCols = int(items[i].ColumnCount)
		}
		curHeader := data.ParseHeader(items[i].HeaderJSON)
		if len(curHeader) > len(header) {
			header = curHeader
		}
	}
	return data.NormalizeColumnNames(header, maxCols)
}

func toReconcileDetail(columns, values []string) []data.ReconcileKV {
	if len(columns) == 0 {
		return []data.ReconcileKV{}
	}
	out := make([]data.ReconcileKV, 0, len(columns))
	for i := range columns {
		out = append(out, data.ReconcileKV{
			Key:   columns[i],
			Value: valueAt(values, i),
		})
	}
	return out
}

func valueAt(values []string, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[idx])
}

func normalizeOrderNoForReconcile(v string) string {
	return strings.TrimLeft(strings.TrimSpace(v), " '’\"＇")
}

func normalizeHeaderNameForReconcile(input string) string {
	clean := headerParenRegex.ReplaceAllString(strings.TrimSpace(input), "")
	replacer := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "")
	return strings.ToLower(replacer.Replace(clean))
}

func findColumnIndexForReconcile(columns []string, expectedNames []string) int {
	normalized := make([]string, 0, len(columns))
	for i := range columns {
		normalized = append(normalized, normalizeHeaderNameForReconcile(columns[i]))
	}
	for i := range expectedNames {
		target := normalizeHeaderNameForReconcile(expectedNames[i])
		for idx := range normalized {
			if normalized[idx] == target {
				return idx
			}
		}
	}
	return -1
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
