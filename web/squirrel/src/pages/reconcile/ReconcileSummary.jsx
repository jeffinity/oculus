/* eslint-disable max-lines-per-function */
import { Button, Card, Input, Select, Space, Typography } from "antd";

export default function ReconcileSummary({
  amountCheckFilter,
  amountCheckPieStyle,
  amountCheckStats,
  exporting,
  filteredRows,
  handleAmountCheckFilterChange,
  handleClearManualReviews,
  handleExportDetail,
  handleMatchFilterChange,
  handleResetFilters,
  matchFilter,
  orderNoKeyword,
  pieStats,
  pieStyle,
  setAmountCheckFilter,
  setMatchFilter,
  setOrderNoKeyword
}) {
  const matchPieSelected = matchFilter === "unmatched";
  const amountPieSelected = amountCheckFilter === "inconsistent";

  function handlePendingReviewClick() {
    if (matchPieSelected) {
      setMatchFilter("all");
      return;
    }
    setAmountCheckFilter("all");
    setMatchFilter("unmatched");
  }

  function handlePendingIssueClick() {
    if (amountPieSelected) {
      setAmountCheckFilter("all");
      return;
    }
    setMatchFilter("all");
    setAmountCheckFilter("inconsistent");
  }

  return (
    <Card style={{ marginBottom: 12 }}>
      <div className="reconcile-summary-row">
        <div className="reconcile-pie-wrap">
          <div className="reconcile-pie-group">
            <div className="reconcile-pie-card">
              <div className="reconcile-pie-row">
                <button
                  type="button"
                  className={`reconcile-pie-button${matchPieSelected ? " is-selected" : ""}`}
                  onClick={handlePendingReviewClick}
                  aria-pressed={matchPieSelected}
                >
                  <div className="reconcile-pie" style={pieStyle}>
                    <div className="reconcile-pie-center">
                      <div className="reconcile-pie-center-label">全部</div>
                      <div className="reconcile-pie-center-value">{pieStats.total}</div>
                    </div>
                  </div>
                </button>
                <div className="reconcile-pie-legend">
                  <div>匹配微信: {pieStats.matchedWechat}</div>
                  <div>匹配支付宝: {pieStats.matchedAlipay}</div>
                  <div>未匹配: {pieStats.unmatched}</div>
                </div>
              </div>
              <div
                className={`reconcile-pie-metric ${pieStats.pendingReview === 0 ? "is-zero" : "is-danger"}`}
              >
                待核算数量: {pieStats.pendingReview}
              </div>
            </div>
            <div className="reconcile-pie-card">
              <div className="reconcile-pie-row">
                <button
                  type="button"
                  className={`reconcile-pie-button${amountPieSelected ? " is-selected" : ""}`}
                  onClick={handlePendingIssueClick}
                  aria-pressed={amountPieSelected}
                >
                  <div className="reconcile-pie" style={amountCheckPieStyle}>
                    <div className="reconcile-pie-center">
                      <div className="reconcile-pie-center-label">金额校验</div>
                      <div className="reconcile-pie-center-value">{amountCheckStats.total}</div>
                    </div>
                  </div>
                </button>
                <div className="reconcile-pie-legend">
                  <div>核算金额一致: {amountCheckStats.consistent}</div>
                  <div>核算金额不一致: {amountCheckStats.inconsistent}</div>
                </div>
              </div>
              <div
                className={`reconcile-pie-metric ${amountCheckStats.pendingIssue === 0 ? "is-zero" : "is-danger"}`}
              >
                待处理明细: {amountCheckStats.pendingIssue}
              </div>
            </div>
          </div>
        </div>
        <div className="reconcile-filter-panel">
          <Space wrap>
            <Select
              value={matchFilter}
              onChange={handleMatchFilterChange}
              allowClear={matchFilter !== "all"}
              onClear={() => setMatchFilter("all")}
              style={{ width: 180 }}
              options={[
                { label: "全部", value: "all" },
                { label: "仅匹配结果", value: "matched" },
                { label: "未匹配结果", value: "unmatched" }
              ]}
            />
            <Select
              value={amountCheckFilter}
              onChange={handleAmountCheckFilterChange}
              allowClear={amountCheckFilter !== "all"}
              onClear={() => setAmountCheckFilter("all")}
              style={{ width: 200 }}
              options={[
                { label: "金额筛选：全部", value: "all" },
                { label: "核算金额一致", value: "consistent" },
                { label: "核算金额不一致", value: "inconsistent" }
              ]}
            />
            <Input
              value={orderNoKeyword}
              onChange={(e) => setOrderNoKeyword(e.target.value)}
              allowClear
              style={{ width: 280 }}
              placeholder="按子单原始单号搜索"
            />
          </Space>
          <Space wrap>
            <Button onClick={handleResetFilters}>清空所有筛选条件</Button>
            <Button type="primary" loading={exporting} onClick={handleExportDetail}>
              导出明细表
            </Button>
            <Button danger onClick={handleClearManualReviews}>
              清理人工核查数据
            </Button>
            <Typography.Text type="secondary">当前 {filteredRows.length} 条</Typography.Text>
          </Space>
        </div>
      </div>
    </Card>
  );
}
