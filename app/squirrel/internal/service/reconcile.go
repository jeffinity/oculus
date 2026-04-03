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

type reconcileDataset struct {
	salesRows     []data.SquirrelSectionRowData
	wechatRows    []data.SquirrelSectionRowData
	alipayRows    []data.SquirrelSectionRowData
	wechatColumns []string
	alipayColumns []string
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

	run, err := s.loadCompletedReconcileRun(ctx, taskID, strings.TrimSpace(req.GetRunId()), "list reconcile rows failed")
	if err != nil {
		return nil, err
	}

	page, pageSize := normalizeReconcileListParams(req)

	rows, total, err := s.repo.ListReconcileRows(ctx, run.RunID, page, pageSize)
	if err != nil {
		s.log.Errorf("list reconcile rows failed run_id=%s err=%v", run.RunID, err)
		return nil, status.Error(codes.Internal, "list reconcile rows failed")
	}
	if err = s.fillReconcileRowFilenames(ctx, taskID, rows, "list reconcile rows failed"); err != nil {
		return nil, err
	}
	if err = s.attachManualReviews(ctx, taskID, run.RunID, rows, "list reconcile rows failed"); err != nil {
		return nil, err
	}
	return buildListReconcileRowsReply(run, rows, total, page, pageSize), nil
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
	dataset, err := s.loadReconcileDataset(ctx, salesImport, wechatImports, alipayImports)
	if err != nil {
		fail(err)
		return
	}
	if err = s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 28, "构建支付索引", "", 0, 0); err != nil {
		s.log.Errorf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}
	wechatMap, alipayMap, err := buildPaymentMatchMaps(*dataset)
	if err != nil {
		fail(err)
		return
	}
	if err = s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 55, "并行匹配销售数据", "", 0, 0); err != nil {
		s.log.Errorf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}
	outRows, matchedRows, err := s.matchSalesRows(ctx, runID, dataset.salesRows, salesColumns, wechatMap, alipayMap)
	if err != nil {
		fail(err)
		return
	}
	if err = s.finalizeReconcileRun(ctx, runID, taskID, salesImport.Filename, outRows, matchedRows, salesColumns); err != nil {
		fail(err)
	}
}

func (s *SquirrelService) loadCompletedReconcileRun(
	ctx context.Context,
	taskID, runID, action string,
) (*data.SquirrelReconcileRun, error) {
	var (
		run *data.SquirrelReconcileRun
		err error
	)
	if runID != "" {
		run, err = s.repo.GetReconcileRunByID(ctx, taskID, runID)
	} else {
		run, err = s.repo.GetLatestReconcileRunByTask(ctx, taskID)
	}
	if err != nil {
		s.log.Errorf("query reconcile run failed task_id=%s run_id=%s err=%v", taskID, runID, err)
		return nil, status.Error(codes.Internal, action)
	}
	if run == nil {
		return nil, status.Error(codes.NotFound, "reconcile run not found")
	}
	if run.Status != data.ReconcileStatusSucceeded {
		return nil, status.Error(codes.FailedPrecondition, "reconcile is not completed")
	}
	return run, nil
}

func normalizeReconcileListParams(req *squirrelv1.ListReconcileRowsRequest) (int, int) {
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
	return page, pageSize
}

func (s *SquirrelService) fillReconcileRowFilenames(
	ctx context.Context,
	taskID string,
	rows []data.ReconcileRowData,
	action string,
) error {
	salesImport, err := s.repo.GetSectionImport(ctx, taskID, "sales")
	if err != nil {
		s.log.Errorf("query sales import failed task_id=%s err=%v", taskID, err)
		return status.Error(codes.Internal, action)
	}
	defaultFilename := ""
	if salesImport != nil {
		defaultFilename = strings.TrimSpace(salesImport.Filename)
	}
	if defaultFilename == "" {
		return nil
	}
	for i := range rows {
		if strings.TrimSpace(rows[i].Filename) == "" {
			rows[i].Filename = defaultFilename
		}
	}
	return nil
}

func (s *SquirrelService) attachManualReviews(
	ctx context.Context,
	taskID, runID string,
	rows []data.ReconcileRowData,
	action string,
) error {
	manualMap, err := s.repo.ListManualReviewsByRows(ctx, taskID, rows)
	if err != nil {
		s.log.Errorf("list manual reviews failed task_id=%s run_id=%s err=%v", taskID, runID, err)
		return status.Error(codes.Internal, action)
	}
	for i := range rows {
		if it, ok := manualMap[buildManualReviewKey(rows[i].Filename, rows[i].RowNo)]; ok {
			cp := it
			rows[i].ManualReview = &cp
		}
	}
	return nil
}

func buildListReconcileRowsReply(
	run *data.SquirrelReconcileRun,
	rows []data.ReconcileRowData,
	total int64,
	page, pageSize int,
) *squirrelv1.ListReconcileRowsReply {
	pbRows := make([]*squirrelv1.ReconcileRow, 0, len(rows))
	for i := range rows {
		pbRows = append(pbRows, toProtoReconcileRow(rows[i]))
	}
	return &squirrelv1.ListReconcileRowsReply{
		Run:      toProtoReconcileRun(*run, false),
		Columns:  data.ParseHeader(run.ResultColumnsJSON),
		Rows:     pbRows,
		Page:     uint32(page),
		PageSize: uint32(pageSize),
		Total:    uint32(total),
	}
}

func (s *SquirrelService) loadReconcileDataset(
	ctx context.Context,
	salesImport data.SquirrelSectionImport,
	wechatImports, alipayImports []data.SquirrelSectionImport,
) (*reconcileDataset, error) {
	var (
		out     reconcileDataset
		loadErr error
		mu      sync.Mutex
		wg      sync.WaitGroup
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
		out.salesRows = rows
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
		out.wechatRows = rows
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
		out.alipayRows = rows
	}()
	wg.Wait()
	if loadErr != nil {
		return nil, loadErr
	}
	out.wechatColumns = columnsFromImports(wechatImports)
	out.alipayColumns = columnsFromImports(alipayImports)
	return &out, nil
}

func buildPaymentMatchMaps(
	dataset reconcileDataset,
) (map[string][]reconcileMatchItem, map[string][]reconcileMatchItem, error) {
	wechatMap := make(map[string][]reconcileMatchItem, len(dataset.wechatRows))
	alipayMap := make(map[string][]reconcileMatchItem, len(dataset.alipayRows))
	if err := buildWechatMatchMap(dataset.wechatColumns, dataset.wechatRows, wechatMap); err != nil {
		return nil, nil, err
	}
	if err := buildAlipayMatchMap(dataset.alipayColumns, dataset.alipayRows, alipayMap); err != nil {
		return nil, nil, err
	}
	return wechatMap, alipayMap, nil
}

func buildWechatMatchMap(
	columns []string,
	rows []data.SquirrelSectionRowData,
	out map[string][]reconcileMatchItem,
) error {
	idxOrderNo := findColumnIndexForReconcile(columns, []string{"淘宝订单编号"})
	idxIncomeType := findColumnIndexForReconcile(columns, []string{"入账类型"})
	if idxOrderNo < 0 || idxIncomeType < 0 {
		return fmt.Errorf("missing wechat key columns")
	}
	for i := range rows {
		incomeType := strings.TrimSpace(valueAt(rows[i].Values, idxIncomeType))
		if incomeType != "交易收款" && incomeType != "交易退款(售后)" {
			continue
		}
		key := normalizeOrderNoForReconcile(valueAt(rows[i].Values, idxOrderNo))
		if key == "" {
			continue
		}
		out[key] = append(out[key], reconcileMatchItem{
			Source:   "wechat",
			RowNo:    rows[i].RowNo,
			Filename: rows[i].Filename,
			Detail:   toReconcileDetail(columns, rows[i].Values),
		})
	}
	return nil
}

func buildAlipayMatchMap(
	columns []string,
	rows []data.SquirrelSectionRowData,
	out map[string][]reconcileMatchItem,
) error {
	idxOrderNo := findColumnIndexForReconcile(columns, []string{"业务基础订单号"})
	idxBizDesc := findColumnIndexForReconcile(columns, []string{"业务描述"})
	idxRemark := findColumnIndexForReconcile(columns, []string{"备注"})
	if idxOrderNo < 0 || idxBizDesc < 0 || idxRemark < 0 {
		return fmt.Errorf("missing alipay key columns")
	}
	for i := range rows {
		for key := range extractAlipayMatchKeys(rows[i].Values, idxOrderNo, idxBizDesc, idxRemark) {
			out[key] = append(out[key], reconcileMatchItem{
				Source:   "alipay",
				RowNo:    rows[i].RowNo,
				Filename: rows[i].Filename,
				Detail:   toReconcileDetail(columns, rows[i].Values),
			})
		}
	}
	return nil
}

func extractAlipayMatchKeys(values []string, idxOrderNo, idxBizDesc, idxRemark int) map[string]struct{} {
	bizDesc := strings.TrimSpace(valueAt(values, idxBizDesc))
	remark := strings.TrimSpace(valueAt(values, idxRemark))
	baseOrderNo := normalizeOrderNoForReconcile(valueAt(values, idxOrderNo))
	_, descHit := alipayAllowedBizDesc[bizDesc]
	keywordHit := strings.Contains(remark, "扣款用途：基金代发任务")
	if !descHit && !keywordHit {
		return nil
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
	return keys
}

func (s *SquirrelService) matchSalesRows(
	ctx context.Context,
	runID string,
	salesRows []data.SquirrelSectionRowData,
	salesColumns []string,
	wechatMap, alipayMap map[string][]reconcileMatchItem,
) ([]data.ReconcileRowData, int32, error) {
	salesOrderIdx := findColumnIndexForReconcile(salesColumns, []string{"子单原始单号"})
	if salesOrderIdx < 0 {
		return nil, 0, fmt.Errorf("missing sales key column")
	}
	workerCount := normalizeReconcileWorkerCount(runtime.NumCPU())
	jobs := make(chan indexedReconcileRow, workerCount*2)
	results := make(chan indexedReconcileRow, workerCount*2)

	for i := 0; i < workerCount; i++ {
		go runReconcileMatchWorker(jobs, results, salesOrderIdx, len(salesColumns), wechatMap, alipayMap)
	}
	enqueueReconcileJobs(jobs, salesRows)

	outRows := make([]data.ReconcileRowData, len(salesRows))
	var matchedRows int32
	for i := 0; i < len(salesRows); i++ {
		item := <-results
		outRows[item.Idx] = item.Row
		if len(item.Row.Matches) > 0 {
			matchedRows++
		}
		s.updateReconcileMatchProgress(ctx, runID, i+1, len(salesRows), matchedRows)
	}
	close(results)
	sort.Slice(outRows, func(i, j int) bool { return outRows[i].RowNo < outRows[j].RowNo })
	return outRows, matchedRows, nil
}

func normalizeReconcileWorkerCount(workerCount int) int {
	if workerCount < 2 {
		return 2
	}
	if workerCount > 8 {
		return 8
	}
	return workerCount
}

func runReconcileMatchWorker(
	jobs <-chan indexedReconcileRow,
	results chan<- indexedReconcileRow,
	salesOrderIdx, salesColumnCount int,
	wechatMap, alipayMap map[string][]reconcileMatchItem,
) {
	for job := range jobs {
		results <- indexedReconcileRow{
			Idx: job.Idx,
			Row: buildMatchedReconcileRow(job.Row, salesOrderIdx, salesColumnCount, wechatMap, alipayMap),
		}
	}
}

func buildMatchedReconcileRow(
	row data.ReconcileRowData,
	salesOrderIdx, salesColumnCount int,
	wechatMap, alipayMap map[string][]reconcileMatchItem,
) data.ReconcileRowData {
	orderNo := normalizeOrderNoForReconcile(valueAt(row.Values, salesOrderIdx))
	return data.ReconcileRowData{
		Filename: row.Filename,
		RowNo:    row.RowNo,
		Values:   padValues(row.Values, salesColumnCount),
		Matches:  appendMatchedReconcileItems(nil, wechatMap[orderNo], alipayMap[orderNo]),
	}
}

func appendMatchedReconcileItems(
	base []data.ReconcileMatch,
	groups ...[]reconcileMatchItem,
) []data.ReconcileMatch {
	matches := base
	for _, group := range groups {
		for _, it := range group {
			matches = append(matches, data.ReconcileMatch{
				Source:   it.Source,
				RowNo:    it.RowNo,
				Filename: it.Filename,
				Detail:   it.Detail,
			})
		}
	}
	return matches
}

func enqueueReconcileJobs(jobs chan<- indexedReconcileRow, salesRows []data.SquirrelSectionRowData) {
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
}

func (s *SquirrelService) updateReconcileMatchProgress(
	ctx context.Context,
	runID string,
	processed, total int,
	matchedRows int32,
) {
	processedRows := int32(processed)
	totalRows := int32(total)
	if processedRows%300 != 0 && processedRows != totalRows {
		return
	}
	progress := int32(55 + (processedRows * 40 / maxInt32(totalRows, 1)))
	if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, progress, "并行匹配销售数据", "", processedRows, matchedRows); err != nil {
		s.log.Warnf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}
}

func (s *SquirrelService) finalizeReconcileRun(
	ctx context.Context,
	runID, taskID, salesFilename string,
	outRows []data.ReconcileRowData,
	matchedRows int32,
	salesColumns []string,
) error {
	if err := s.repo.UpdateReconcileRunProgress(ctx, runID, data.ReconcileStatusRunning, 97, "写入核算结果", "", int32(len(outRows)), matchedRows); err != nil {
		s.log.Warnf("update reconcile progress failed run_id=%s err=%v", runID, err)
	}
	if err := s.repo.ReplaceReconcileRowsAndMarkSuccess(ctx, runID, outRows, matchedRows, int32(len(outRows)), salesColumns); err != nil {
		return fmt.Errorf("save reconcile result failed")
	}
	autoReviews := buildAutoManualReviews(taskID, salesFilename, outRows, salesColumns)
	if len(autoReviews) == 0 {
		return nil
	}
	if err := s.repo.BatchUpsertManualReviews(ctx, autoReviews); err != nil {
		return fmt.Errorf("save auto manual reviews failed")
	}
	return nil
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
