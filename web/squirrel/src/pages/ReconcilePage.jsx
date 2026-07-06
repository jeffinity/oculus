/* eslint-disable max-lines-per-function, complexity, max-statements, max-lines */
import { CopyOutlined } from "@ant-design/icons";
import {
  Alert,
  Button,
  Modal,
  Progress,
  Space,
  Spin,
  Typography,
  message
} from "antd";
import { Suspense, lazy, useCallback, useEffect, useMemo, useState } from "react";

const AMOUNT_COMPARE_EPSILON = 0.000001;
const ManualReviewModal = lazy(() => import("./reconcile/ManualReviewModal"));
const ReconcileSummary = lazy(() => import("./reconcile/ReconcileSummary"));
const ReconcileResultsTable = lazy(() => import("./reconcile/ReconcileResultsTable"));

async function requestJSON(url, options = {}) {
  const res = await fetch(url, options);
  if (!res.ok) {
    let detail = "";
    try {
      const json = await res.json();
      detail = json?.message || json?.error || "";
    } catch {
      detail = "";
    }
    throw new Error(detail || `request failed (${res.status})`);
  }
  if (res.status === 204) return {};
  return res.json();
}

async function requestBlob(url, options = {}) {
  const res = await fetch(url, options);
  if (!res.ok) {
    let detail = "";
    try {
      const json = await res.json();
      detail = json?.message || json?.error || "";
    } catch {
      detail = "";
    }
    throw new Error(detail || `request failed (${res.status})`);
  }
  return {
    blob: await res.blob(),
    disposition: res.headers.get("content-disposition") || ""
  };
}

async function startReconcile(taskId, force = false) {
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/reconcile:start`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ taskId, force })
  });
}

async function getReconcileStatus(taskId, runId = "") {
  const q = new URLSearchParams();
  if (runId) q.set("run_id", runId);
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/reconcile/status?${q.toString()}`);
}

async function listReconcileRows(taskId, runId, page = 1, pageSize = 100) {
  const q = new URLSearchParams({
    run_id: runId,
    page: String(page),
    page_size: String(pageSize)
  });
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/reconcile/rows?${q.toString()}`);
}

async function upsertManualReview(taskId, payload) {
  const body = { taskId, ...payload };
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/manual-reviews:upsert`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
}

async function clearManualReviews(taskId) {
  await requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/manual-reviews`, {
    method: "DELETE"
  });
}

async function deleteManualReview(taskId, payload) {
  const body = { taskId, ...payload };
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/manual-reviews:delete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
}

async function listManualRemarkOptions(taskId) {
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/manual-remark-options`);
}

async function deleteManualRemarkOption(taskId, content) {
  return requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/manual-remark-options:delete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ taskId, content })
  });
}

function findSalesColumnIndex(salesColumns, aliases) {
  const normalizedAliases = aliases.map((it) => String(it || "").replaceAll(/\s+/g, ""));
  return salesColumns.findIndex((name) => {
    const normalized = String(name || "").replaceAll(/\s+/g, "");
    return normalizedAliases.some((alias) => normalized === alias || normalized.includes(alias));
  });
}

function toNumber(value) {
  if (value === null || value === undefined) return 0;
  const num = Number(String(value).replaceAll(',', "").trim());
  return Number.isFinite(num) ? num : 0;
}

function formatAmount(value) {
  return Number.isFinite(value) ? value.toFixed(2) : "0.00";
}

function parseDownloadFilename(disposition, fallback) {
  const utf8Match = disposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1]);
    } catch {
      return utf8Match[1];
    }
  }
  const plainMatch = disposition.match(/filename="([^"]+)"/i);
  if (plainMatch?.[1]) {
    return plainMatch[1];
  }
  return fallback;
}

function buildSettleValues(row, settleContext, reviewOverride = null) {
  if (!row || !settleContext) {
    return { settleQty: 0, settleAmount: 0, refundQty: 0, refundAmount: 0, shipQty: 0, buyerPaid: 0 };
  }
  const review = reviewOverride || row.__manualReview || null;
  const refundQty = Math.trunc(toNumber(review?.refundQty || review?.refund_qty || 0));
  const refundAmount = toNumber(review?.refundAmount || review?.refund_amount || 0);
  const shipQty = toNumber(row[`c_${settleContext.shipQtyColIndex}`] || "");
  const buyerPaid = toNumber(row[`c_${settleContext.buyerPaidColIndex}`] || "");
  const settleQty = shipQty - refundQty;
  const settleAmount = buyerPaid - refundAmount;
  return { settleQty, settleAmount, refundQty, refundAmount, shipQty, buyerPaid };
}

function sumMatchIncome(matches = []) {
  return matches.reduce((sum, item) => {
    const details = item?.detail || [];
    const itemIncome = details.reduce((acc, kv) => {
      if (!String(kv?.key || "").includes("收入")) return acc;
      return acc + toNumber(kv?.value || 0);
    }, 0);
    return sum + itemIncome;
  }, 0);
}

function OrderNoCopyText({ value, onCopy }) {
  const text = String(value || "").trim();
  if (!text) return "-";
  return (
    <Space size={4}>
      <Typography.Text ellipsis={{ tooltip: text }}>{text}</Typography.Text>
      <Button
        type="text"
        size="small"
        icon={<CopyOutlined />}
        onClick={(e) => {
          e.stopPropagation();
          onCopy(text);
        }}
      />
    </Space>
  );
}

function normalizeReconcileRows(rows = [], columnCount = 0) {
  return rows.map((row) => {
    const values = [...(row.values || [])];
    while (values.length < columnCount) values.push("");
    const mapped = {
      key: `${row.filename || "-"}-${row.rowNo}`,
      filename: row.filename || "",
      rowNo: row.rowNo,
      __matches: row.matches || [],
      __manualReview: row.manualReview || row.manual_review || null
    };
    for (const [idx, v] of values.entries()) {
      mapped[`c_${idx}`] = v || "";
    }
    return mapped;
  });
}

function valueFromRow(row, idx) {
  if (!row || idx < 0) {
    return "";
  }
  return row[`c_${idx}`] || "";
}

function buildConicGradient(parts) {
  let start = 0;
  const segments = parts.map(({ color, value }) => {
    const end = start + value;
    const segment = `${color} ${start}deg ${end}deg`;
    start = end;
    return segment;
  });
  return {
    background: `conic-gradient(${segments.join(", ")})`
  };
}

function ReconcilePage() {
  const [msgApi, msgContext] = message.useMessage();
  const query = new URLSearchParams(window.location.search);
  const taskId = query.get("task_id") || "";
  const taskTitle = query.get("task_title") || "核算页面";

  const [loading, setLoading] = useState(true);
  const [run, setRun] = useState(null);
  const [error, setError] = useState("");
  const [tableLoading, setTableLoading] = useState(false);
  const [salesColumns, setSalesColumns] = useState([]);
  const [rows, setRows] = useState([]);
  const [matchFilter, setMatchFilter] = useState("all");
  const [amountCheckFilter, setAmountCheckFilter] = useState("all");
  const [orderNoKeyword, setOrderNoKeyword] = useState("");
  const [manualModalOpen, setManualModalOpen] = useState(false);
  const [activeManualRow, setActiveManualRow] = useState(null);
  const [refundQty, setRefundQty] = useState(0);
  const [refundAmount, setRefundAmount] = useState(0);
  const [manualRemark, setManualRemark] = useState("");
  const [manualRemarkOptions, setManualRemarkOptions] = useState([]);
  const [manualRemarkOptionsLoading, setManualRemarkOptionsLoading] = useState(false);
  const [manualSaving, setManualSaving] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [tableScrollY, setTableScrollY] = useState(() =>
    Math.max(window.innerHeight - 360, 320)
  );

  const salesOrderNoColIndex = useMemo(() => findSalesColumnIndex(salesColumns, ["子单原始单号"]), [salesColumns]);
  const salesPayAmountColIndex = useMemo(() => findSalesColumnIndex(salesColumns, ["订单支付金额"]), [salesColumns]);
  const buyerPaidColIndex = useMemo(() => findSalesColumnIndex(salesColumns, ["买家实付"]), [salesColumns]);
  const shipQtyColIndex = useMemo(() => findSalesColumnIndex(salesColumns, ["实发数量"]), [salesColumns]);
  const settleContext = useMemo(
    () => ({
      shipQtyColIndex,
      buyerPaidColIndex
    }),
    [shipQtyColIndex, buyerPaidColIndex]
  );

  const amountCheckMap = useMemo(() => {
    const orderMap = new Map();
    for (const row of rows) {
      const orderNo = String(valueFromRow(row, salesOrderNoColIndex) || "").trim();
      if (!orderNo) continue;
      const settleValues = buildSettleValues(row, settleContext);
      const current = orderMap.get(orderNo) || {
        totalSettleAmount: 0,
        details: []
      };
      current.totalSettleAmount += settleValues.settleAmount;
      current.details.push({
        key: row.key,
        rowNo: row.rowNo,
        filename: row.filename || "",
        settleAmount: settleValues.settleAmount
      });
      orderMap.set(orderNo, current);
    }

    const out = new Map();
    for (const row of rows) {
      const hasMatches = (row.__matches || []).length > 0;
      if (!hasMatches) {
        out.set(row.key, {
          hasMatches: false,
          consistent: false,
          totalIncome: 0,
          totalSettleAmount: 0,
          orderNo: String(valueFromRow(row, salesOrderNoColIndex) || "").trim(),
          details: []
        });
        continue;
      }
      const orderNo = String(valueFromRow(row, salesOrderNoColIndex) || "").trim();
      const aggregated = orderMap.get(orderNo) || { totalSettleAmount: 0, details: [] };
      const totalIncome = sumMatchIncome(row.__matches || []);
      const consistent = Math.abs(aggregated.totalSettleAmount - totalIncome) <= AMOUNT_COMPARE_EPSILON;
      out.set(row.key, {
        hasMatches: true,
        consistent,
        totalIncome,
        totalSettleAmount: aggregated.totalSettleAmount,
        orderNo,
        details: aggregated.details
      });
    }
    return out;
  }, [rows, salesOrderNoColIndex, settleContext]);

  async function loadManualRemarkOptions() {
    setManualRemarkOptionsLoading(true);
    try {
      const json = await listManualRemarkOptions(taskId);
      setManualRemarkOptions(json.items || []);
    } catch (err) {
      msgApi.error(err?.message || "常用备注加载失败");
    } finally {
      setManualRemarkOptionsLoading(false);
    }
  }

  function openManualReview(row) {
    setActiveManualRow(row);
    const review = row?.__manualReview || null;
    setRefundQty(Math.trunc(review?.refundQty ? toNumber(review.refundQty) : 0));
    setRefundAmount(review?.refundAmount ? toNumber(review.refundAmount) : 0);
    setManualRemark(String(review?.remark || ""));
    loadManualRemarkOptions();
    setManualModalOpen(true);
  }

  async function handleDeleteManualRemarkOption(content) {
    try {
      await deleteManualRemarkOption(taskId, content);
      setManualRemarkOptions((prev) => prev.filter((item) => item.content !== content));
      msgApi.success("常用备注已删除");
    } catch (err) {
      msgApi.error(err?.message || "常用备注删除失败");
    }
  }

  async function copyOrderNo(text) {
    try {
      await navigator.clipboard.writeText(text);
      msgApi.success("订单号已复制");
    } catch {
      msgApi.error("复制失败，请手动复制");
    }
  }

  async function handleDeleteManualReview(row) {
    if (!row) return;
    try {
      await deleteManualReview(taskId, {
        filename: row.filename,
        rowNo: row.rowNo
      });
      setRows((prev) => prev.map((it) => (it.key === row.key ? { ...it, __manualReview: null } : it)));
      msgApi.success("人工核查记录已删除");
    } catch (err) {
      msgApi.error(err?.message || "人工核查记录删除失败");
    }
  }

  function handleResetFilters() {
    setMatchFilter("all");
    setAmountCheckFilter("all");
    setOrderNoKeyword("");
  }

  function handleMatchFilterChange(value) {
    setMatchFilter(value);
    if (value !== "all") {
      setAmountCheckFilter("all");
    }
  }

  function handleAmountCheckFilterChange(value) {
    setAmountCheckFilter(value);
    if (value !== "all") {
      setMatchFilter("all");
    }
  }

  const pieStats = useMemo(() => {
    const total = rows.length;
    let matchedWechat = 0;
    let matchedAlipay = 0;
    let unmatched = 0;
    let autoReviewed = 0;
    let manualReviewed = 0;
    for (const row of rows) {
      const matches = row.__matches || [];
      const hasWechat = matches.some((it) => it.source === "wechat");
      const hasAlipay = matches.some((it) => it.source === "alipay");
      if (hasWechat) matchedWechat += 1;
      if (hasAlipay) matchedAlipay += 1;
      if (!hasWechat && !hasAlipay) {
        unmatched += 1;
        const reviewType = String(row.__manualReview?.reviewType || row.__manualReview?.review_type || "").toUpperCase();
        if (reviewType === "AUTO") {
          autoReviewed += 1;
        } else if (reviewType === "MANUAL") {
          manualReviewed += 1;
        }
      }
    }
    return {
      total,
      matchedWechat,
      matchedAlipay,
      unmatched,
      autoReviewed,
      manualReviewed,
      pendingReview: Math.max(unmatched - autoReviewed - manualReviewed, 0)
    };
  }, [rows]);

  const amountCheckStats = useMemo(() => {
    let consistent = 0;
    let inconsistent = 0;
    for (const row of rows) {
      const info = amountCheckMap.get(row.key);
      if (!info || !info.hasMatches) continue;
      if (info.consistent) {
        consistent += 1;
      } else {
        inconsistent += 1;
      }
    }
    return {
      total: consistent + inconsistent,
      consistent,
      inconsistent,
      pendingIssue: inconsistent
    };
  }, [rows, amountCheckMap]);

  const filteredRows = useMemo(() => {
    let data = rows;
    if (matchFilter === "matched") {
      data = data.filter((row) => (row.__matches || []).length > 0);
    } else if (matchFilter === "unmatched") {
      data = data.filter((row) => (row.__matches || []).length === 0);
    }
    const keyword = orderNoKeyword.trim();
    if (keyword && salesOrderNoColIndex >= 0) {
      const key = `c_${salesOrderNoColIndex}`;
      data = data.filter((row) => String(row[key] || "").includes(keyword));
    }
    if (amountCheckFilter !== "all") {
      data = data.filter((row) => {
        const info = amountCheckMap.get(row.key);
        if (!info || !info.hasMatches) return false;
        if (amountCheckFilter === "consistent") return info.consistent;
        return !info.consistent;
      });
    }
    return data;
  }, [rows, matchFilter, orderNoKeyword, salesOrderNoColIndex, amountCheckFilter, amountCheckMap]);

  const pieStyle = useMemo(() => {
    const { matchedWechat, matchedAlipay, unmatched } = pieStats;
    const total = Math.max(matchedWechat + matchedAlipay + unmatched, 1);
    return buildConicGradient([
      { color: "#00a870", value: (matchedWechat / total) * 360 },
      { color: "#1476ff", value: (matchedAlipay / total) * 360 },
      { color: "#bfbfbf", value: (unmatched / total) * 360 }
    ]);
  }, [pieStats]);

  const amountCheckPieStyle = useMemo(() => {
    const total = Math.max(amountCheckStats.total, 1);
    return buildConicGradient([
      { color: "#52c41a", value: (amountCheckStats.consistent / total) * 360 },
      { color: "#ff4d4f", value: 360 - (amountCheckStats.consistent / total) * 360 }
    ]);
  }, [amountCheckStats]);

  const manualPreview = useMemo(() => {
    if (!activeManualRow) return null;
    return buildSettleValues(activeManualRow, settleContext, {
      refundQty: String(Math.trunc(toNumber(refundQty ?? 0))),
      refundAmount: String(toNumber(refundAmount ?? 0))
    });
  }, [activeManualRow, settleContext, refundQty, refundAmount]);

  async function submitManualReview(ignored) {
    if (!activeManualRow) return;
    try {
      setManualSaving(true);
      const payload = {
        filename: activeManualRow.filename,
        rowNo: activeManualRow.rowNo,
        refundQty: String(Math.trunc(toNumber(refundQty))),
        refundAmount: String(toNumber(refundAmount)),
        remark: manualRemark.trim(),
        ignored
      };
      const json = await upsertManualReview(taskId, payload);
      const review = json.review || null;
      setRows((prev) =>
        prev.map((it) =>
          it.key === activeManualRow.key
            ? {
                ...it,
                __manualReview: review
                  ? {
                      ...review,
                      settleQty:
                        manualPreview && Number.isFinite(manualPreview.settleQty)
                          ? String(manualPreview.settleQty)
                          : review.settleQty || "",
                      settleAmount:
                        manualPreview && Number.isFinite(manualPreview.settleAmount)
                          ? String(manualPreview.settleAmount)
                          : review.settleAmount || ""
                    }
                  : null
              }
            : it
        )
      );
      setManualModalOpen(false);
      msgApi.success(ignored ? "已忽略本条数据" : "人工核查已提交");
    } catch (err) {
      msgApi.error(err?.message || "人工核查保存失败");
    } finally {
      setManualSaving(false);
    }
  }

  function handleClearManualReviews() {
    Modal.confirm({
      title: "确认清理本任务所有人工核查数据吗？",
      content: "此操作不可撤销。",
      okType: "danger",
      onOk: async () => {
        await clearManualReviews(taskId);
        setRows((prev) =>
          prev.map((it) => {
            const reviewType = String(it.__manualReview?.reviewType || it.__manualReview?.review_type || "MANUAL").toUpperCase();
            if (reviewType !== "MANUAL") {
              return it;
            }
            return { ...it, __manualReview: null };
          })
        );
        msgApi.success("人工核查数据已清理");
      }
    });
  }

  async function handleExportDetail() {
    try {
      setExporting(true);
      const q = new URLSearchParams();
      if (run?.runId) q.set("run_id", run.runId);
      const { blob, disposition } = await requestBlob(
        `/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/reconcile/export?${q.toString()}`
      );
      const filename = parseDownloadFilename(disposition, `${taskTitle || "核算结果"}-核算补充明细.xlsx`);
      const objectURL = window.URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = objectURL;
      link.download = filename;
      document.body.append(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(objectURL);
      msgApi.success("明细表已开始导出");
    } catch (err) {
      msgApi.error(err?.message || "明细表导出失败");
    } finally {
      setExporting(false);
    }
  }

  useEffect(() => {
    const onResize = () => {
      setTableScrollY(Math.max(window.innerHeight - 360, 320));
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const loadAllResultRows = useCallback(async (runId) => {
    setTableLoading(true);
    try {
      const queryPageSize = 300;
      let page = 1;
      let total = 0;
      let columns = [];
      let all = [];
      let latestRun = null;

      while (true) {
        const json = await listReconcileRows(taskId, runId, page, queryPageSize);
        const currentRows = json.rows || [];
        if (page === 1) {
          total = json.total || 0;
          columns = json.columns || [];
        }
        all = all.concat(currentRows);
        latestRun = json.run || latestRun;
        if (all.length >= total || currentRows.length === 0) {
          break;
        }
        page += 1;
      }

      setSalesColumns(columns);
      setRows(normalizeReconcileRows(all, columns.length));
      if (latestRun) setRun(latestRun);
    } finally {
      setTableLoading(false);
    }
  }, [taskId]);

  useEffect(() => {
    let alive = true;
    let timer = null;
    const wait = (ms) =>
      new Promise((resolve) => {
        timer = setTimeout(resolve, ms);
      });

    if (!taskId) {
      setError("缺少 task_id");
      setLoading(false);
      return () => {
        alive = false;
        if (timer) clearTimeout(timer);
      };
    }

    (async () => {
      setLoading(true);
      setError("");
      try {
        const start = await startReconcile(taskId, false);
        if (!alive) return;
        let currentRun = start.run || null;
        setRun(currentRun);
        if (!currentRun) throw new Error("核算任务启动失败");

        while (alive) {
          if (currentRun.status === "SUCCEEDED") {
            await loadAllResultRows(currentRun.runId);
            break;
          }
          if (currentRun.status === "FAILED") {
            throw new Error(currentRun.message || "核算失败");
          }
          await wait(1000);
          if (!alive) return;
          const st = await getReconcileStatus(taskId, currentRun.runId);
          if (!alive) return;
          currentRun = st.run || currentRun;
          setRun(currentRun);
        }
      } catch (err) {
        if (!alive) return;
        const msg = err?.message || "核算失败";
        setError(msg);
        msgApi.error(msg);
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [loadAllResultRows, msgApi, taskId]);

  return (
    <div className="reconcile-page">
      {msgContext}
      <div className="reconcile-head">
        <Typography.Title level={3} style={{ margin: 0 }}>
          {taskTitle} - 核算结果
        </Typography.Title>
      </div>
      {error ? <Alert type="error" message={error} showIcon style={{ marginBottom: 12 }} /> : null}
      {loading ? (
        <div className="reconcile-loading">
          <div className="reconcile-progress-card">
            <Spin size="large" />
            <Typography.Text className="reconcile-progress-text">
              {run?.stage || "核算任务处理中..."}
              {run?.reused ? "（复用历史结果）" : ""}
              {run?.totalRows ? ` ${run.processedRows || 0}/${run.totalRows}` : ""}
            </Typography.Text>
            <Progress percent={run?.progress || 0} size="small" style={{ width: 360, maxWidth: "100%" }} />
          </div>
        </div>
      ) : (
        <>
          <Suspense fallback={null}>
            <ReconcileSummary
              amountCheckFilter={amountCheckFilter}
              amountCheckPieStyle={amountCheckPieStyle}
              amountCheckStats={amountCheckStats}
              filteredRows={filteredRows}
              handleAmountCheckFilterChange={handleAmountCheckFilterChange}
              handleClearManualReviews={handleClearManualReviews}
              handleExportDetail={handleExportDetail}
              handleMatchFilterChange={handleMatchFilterChange}
              handleResetFilters={handleResetFilters}
              matchFilter={matchFilter}
              orderNoKeyword={orderNoKeyword}
              pieStats={pieStats}
              pieStyle={pieStyle}
              exporting={exporting}
              setAmountCheckFilter={setAmountCheckFilter}
              setMatchFilter={setMatchFilter}
              setOrderNoKeyword={setOrderNoKeyword}
            />
          </Suspense>
          <Suspense fallback={<Spin style={{ width: "100%", marginTop: 24 }} />}>
            <ReconcileResultsTable
              amountCheckMap={amountCheckMap}
              filteredRows={filteredRows}
              onCopyOrderNo={copyOrderNo}
              onDeleteManualReview={handleDeleteManualReview}
              onOpenManualReview={openManualReview}
              salesColumns={salesColumns}
              salesOrderNoColIndex={salesOrderNoColIndex}
              settleContext={settleContext}
              tableLoading={tableLoading}
              tableScrollY={tableScrollY}
            />
          </Suspense>
          <Suspense fallback={null}>
            <ManualReviewModal
              activeManualRow={activeManualRow}
              buyerPaidColIndex={buyerPaidColIndex}
              copyOrderNo={copyOrderNo}
              formatAmount={formatAmount}
              manualModalOpen={manualModalOpen}
              manualRemarkOptions={manualRemarkOptions}
              manualRemarkOptionsLoading={manualRemarkOptionsLoading}
              manualPreview={manualPreview}
              manualRemark={manualRemark}
              manualSaving={manualSaving}
              onCancel={() => setManualModalOpen(false)}
              onDeleteRemarkOption={handleDeleteManualRemarkOption}
              onRemarkChange={(e) => setManualRemark(e.target.value)}
              onSelectRemarkOption={(value) => setManualRemark(value)}
              onRefundAmountChange={(v) => setRefundAmount(toNumber(v))}
              onRefundQtyChange={(v) => setRefundQty(Math.trunc(toNumber(v)))}
              onSubmit={submitManualReview}
              refundAmount={refundAmount}
              refundQty={refundQty}
              salesOrderNoColIndex={salesOrderNoColIndex}
              salesPayAmountColIndex={salesPayAmountColIndex}
              shipQtyColIndex={shipQtyColIndex}
              valueFromRow={valueFromRow}
              OrderNoCopyText={OrderNoCopyText}
            />
          </Suspense>
        </>
      )}
    </div>
  );
}

export default ReconcilePage;
