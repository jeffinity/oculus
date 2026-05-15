package service

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeffinity/oculus/app/squirrel/internal/data"
)

const exportSupplementPrefix = "【核算补充】"

type exportSettleValues struct {
	SettleQty    int64
	SettleAmount float64
	RefundQty    int64
	RefundAmount float64
}

func (s *SquirrelService) ExportReconcileDetail(ctx context.Context, taskID, runID string) ([]byte, string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "task_id is required")
	}

	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		s.log.Errorf("query task failed task_id=%s err=%v", taskID, err)
		return nil, "", status.Error(codes.Internal, "export reconcile detail failed")
	}
	if task == nil {
		return nil, "", status.Error(codes.NotFound, "task not found")
	}

	run, err := s.loadExportRun(ctx, taskID, runID)
	if err != nil {
		return nil, "", err
	}

	rows, err := s.repo.ListAllReconcileRows(ctx, run.RunID)
	if err != nil {
		s.log.Errorf("list all reconcile rows failed run_id=%s err=%v", run.RunID, err)
		return nil, "", status.Error(codes.Internal, "export reconcile detail failed")
	}

	salesImport, err := s.repo.GetSectionImport(ctx, taskID, "sales")
	if err != nil {
		s.log.Errorf("query sales import failed task_id=%s err=%v", taskID, err)
		return nil, "", status.Error(codes.Internal, "export reconcile detail failed")
	}
	if salesImport == nil {
		return nil, "", status.Error(codes.FailedPrecondition, "sales import not found")
	}

	if err = fillMissingReconcileFilenames(rows, strings.TrimSpace(salesImport.Filename)); err != nil {
		return nil, "", err
	}
	if err = s.attachManualReviews(ctx, taskID, run.RunID, rows, "export reconcile detail failed"); err != nil {
		return nil, "", err
	}

	columns := resolveExportColumns(run, salesImport)

	content, err := buildReconcileDetailWorkbook(rows, columns, salesImport.SheetName)
	if err != nil {
		s.log.Errorf("build reconcile workbook failed task_id=%s run_id=%s err=%v", taskID, run.RunID, err)
		return nil, "", status.Error(codes.Internal, "export reconcile detail failed")
	}
	return content, buildReconcileExportFilename(task.Title, task.MonthTag), nil
}

func (s *SquirrelService) loadExportRun(ctx context.Context, taskID, runID string) (*data.SquirrelReconcileRun, error) {
	runID = strings.TrimSpace(runID)
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
		return nil, status.Error(codes.Internal, "export reconcile detail failed")
	}
	if run == nil {
		return nil, status.Error(codes.NotFound, "reconcile run not found")
	}
	if run.Status != data.ReconcileStatusSucceeded {
		return nil, status.Error(codes.FailedPrecondition, "reconcile is not completed")
	}
	return run, nil
}

func buildReconcileDetailWorkbook(rows []data.ReconcileRowData, columns []string, sheetName string) ([]byte, error) {
	file := excelize.NewFile()
	targetSheet := strings.TrimSpace(sheetName)
	if targetSheet == "" {
		targetSheet = "明细表"
	}
	defaultSheet := file.GetSheetName(0)
	if err := file.SetSheetName(defaultSheet, targetSheet); err != nil {
		return nil, err
	}

	headers := buildReconcileExportHeaders(columns)
	if err := writeWorkbookHeaders(file, targetSheet, headers); err != nil {
		return nil, err
	}

	colIndexShipQty := findColumnIndexForReconcile(columns, []string{"实发数量"})
	colIndexBuyerPaid := findColumnIndexForReconcile(columns, []string{"买家实付"})
	if err := writeWorkbookRows(file, targetSheet, rows, columns, colIndexShipQty, colIndexBuyerPaid); err != nil {
		return nil, err
	}

	if err := styleReconcileWorkbook(file, targetSheet, headers, len(columns), len(rows)); err != nil {
		return nil, err
	}

	buf, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return bytes.Clone(buf.Bytes()), nil
}

func fillMissingReconcileFilenames(rows []data.ReconcileRowData, defaultFilename string) error {
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

func resolveExportColumns(run *data.SquirrelReconcileRun, salesImport *data.SquirrelSectionImport) []string {
	columns := data.ParseHeader(run.ResultColumnsJSON)
	salesColumns := data.NormalizeColumnNames(data.ParseHeader(salesImport.HeaderJSON), int(salesImport.ColumnCount))
	if len(salesColumns) > 0 {
		return salesColumns
	}
	return data.NormalizeColumnNames(columns, len(columns))
}

func buildReconcileExportHeaders(columns []string) []string {
	headers := append([]string{}, columns...)
	return append(headers,
		exportSupplementPrefix+"匹配渠道",
		exportSupplementPrefix+"匹配行",
		exportSupplementPrefix+"匹配总收入",
		exportSupplementPrefix+"退款数量",
		exportSupplementPrefix+"退款金额",
		exportSupplementPrefix+"备注",
		exportSupplementPrefix+"结算数量",
		exportSupplementPrefix+"结算金额",
	)
}

func writeWorkbookHeaders(file *excelize.File, sheet string, headers []string) error {
	for idx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(idx+1, 1)
		if err := file.SetCellValue(sheet, cell, header); err != nil {
			return err
		}
	}
	return nil
}

func writeWorkbookRows(
	file *excelize.File,
	sheet string,
	rows []data.ReconcileRowData,
	columns []string,
	shipQtyColIndex, buyerPaidColIndex int,
) error {
	for rowIdx, row := range rows {
		record := buildWorkbookRowRecord(row, columns, shipQtyColIndex, buyerPaidColIndex)
		axis, _ := excelize.CoordinatesToCellName(1, rowIdx+2)
		if err := file.SetSheetRow(sheet, axis, &record); err != nil {
			return err
		}
	}
	return nil
}

func buildWorkbookRowRecord(
	row data.ReconcileRowData,
	columns []string,
	shipQtyColIndex, buyerPaidColIndex int,
) []any {
	values := padValues(row.Values, len(columns))
	record := append([]any{}, stringSliceToAnySlice(values)...)
	return append(record, buildExportSupplementValues(row, shipQtyColIndex, buyerPaidColIndex)...)
}

func styleReconcileWorkbook(file *excelize.File, sheet string, headers []string, columnCount, rowCount int) error {
	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	if err := file.SetColWidth(sheet, "A", lastCol, 16); err != nil {
		return err
	}
	matchLineCol, _ := excelize.ColumnNumberToName(columnCount + 2)
	if err := file.SetColWidth(sheet, matchLineCol, matchLineCol, 42); err != nil {
		return err
	}
	styleID, err := file.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			WrapText:   true,
			Vertical:   "top",
			Horizontal: "left",
		},
	})
	if err != nil {
		return err
	}
	if err := file.SetCellStyle(sheet, "A1", fmt.Sprintf("%s%d", lastCol, rowCount+1), styleID); err != nil {
		return err
	}
	if err := file.SetRowHeight(sheet, 1, 24); err != nil {
		return err
	}
	return file.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
}

func buildExportSupplementValues(row data.ReconcileRowData, shipQtyColIndex, buyerPaidColIndex int) []any {
	matches := row.Matches
	channel := exportMatchChannel(matches)
	matchLines := buildExportMatchLines(matches)
	matchIncome := formatAmount(sumExportMatchIncome(matches))

	refundQty := int64(0)
	refundAmount := 0.0
	remark := "无"
	if row.ManualReview != nil && strings.EqualFold(strings.TrimSpace(row.ManualReview.ReviewType), data.ManualReviewTypeManual) {
		refundQty = parseInt64(row.ManualReview.RefundQty)
		refundAmount = toNumber(row.ManualReview.RefundAmount)
		if text := strings.TrimSpace(row.ManualReview.Remark); text != "" {
			remark = text
		}
	}

	settleValues := buildExportSettleValues(row, shipQtyColIndex, buyerPaidColIndex)
	return []any{
		channel,
		matchLines,
		matchIncome,
		refundQty,
		formatAmount(refundAmount),
		remark,
		settleValues.SettleQty,
		formatAmount(settleValues.SettleAmount),
	}
}

func buildExportSettleValues(row data.ReconcileRowData, shipQtyColIndex, buyerPaidColIndex int) exportSettleValues {
	review := row.ManualReview
	refundQty := int64(0)
	refundAmount := 0.0
	if review != nil {
		refundQty = parseInt64(review.RefundQty)
		refundAmount = toNumber(review.RefundAmount)
	}
	shipQty := maxInt64(0, parseInt64(valueAt(row.Values, shipQtyColIndex)))
	buyerPaid := toNumber(valueAt(row.Values, buyerPaidColIndex))
	settleQty := shipQty - refundQty
	settleAmount := buyerPaid - refundAmount
	return exportSettleValues{
		SettleQty:    settleQty,
		SettleAmount: settleAmount,
		RefundQty:    refundQty,
		RefundAmount: refundAmount,
	}
}

func exportMatchChannel(matches []data.ReconcileMatch) string {
	if len(matches) == 0 {
		return "未匹配"
	}
	source := strings.ToLower(strings.TrimSpace(matches[0].Source))
	if source == "wechat" {
		return "微信"
	}
	if source == "alipay" {
		return "支付宝"
	}
	return "未匹配"
}

func buildExportMatchLines(matches []data.ReconcileMatch) string {
	if len(matches) == 0 {
		return ""
	}
	lines := make([]string, 0, len(matches))
	for _, item := range matches {
		filename := strings.TrimSpace(item.Filename)
		if filename == "" {
			filename = exportMatchChannel([]data.ReconcileMatch{item})
		}
		lines = append(lines, fmt.Sprintf("%s 第 %d 行", filename, item.RowNo))
	}
	return strings.Join(lines, "\n")
}

func sumExportMatchIncome(matches []data.ReconcileMatch) float64 {
	total := 0.0
	for _, item := range matches {
		for _, kv := range item.Detail {
			if !strings.Contains(strings.TrimSpace(kv.Key), "收入") {
				continue
			}
			total += toNumber(kv.Value)
		}
	}
	return total
}

func buildReconcileExportFilename(title, monthTag string) string {
	name := strings.TrimSpace(title)
	if name == "" {
		name = "核算明细"
	}
	if strings.TrimSpace(monthTag) != "" {
		name = monthTag + "-" + name
	}
	name = sanitizeExportFilename(name)
	return name + "-核算补充明细.xlsx"
}

func sanitizeExportFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	name = strings.TrimSpace(replacer.Replace(name))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" {
		return "核算明细"
	}
	return name
}

func stringSliceToAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func parseInt64(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return int64(toNumber(value))
	}
	return parsed
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func toNumber(value string) float64 {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" {
		return 0
	}
	num, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return num
}

func formatAmount(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
