/* eslint-disable max-lines-per-function, complexity */
import { CopyOutlined } from "@ant-design/icons";
import { Button, Descriptions, Divider, Popover, Space, Table, Tag, Typography } from "antd";
import { useCallback, useMemo, useState } from "react";

const MIN_RESIZE_COL_WIDTH = 80;

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

function getMatchTagKey(item) {
  return `${item.source || "unknown"}-${item.filename || "-"}-${item.rowNo || "-"}`;
}

function getMatchColumnKey(item, label) {
  return `${getMatchTagKey(item)}-${label}`;
}

function buildSettleValues(row, settleContext, reviewOverride = null) {
  if (!row || !settleContext) {
    return { settleQty: 0, settleAmount: 0, refundQty: 0, refundAmount: 0, shipQty: 0, buyerPaid: 0 };
  }
  const review = reviewOverride || row.__manualReview || null;
  const refundQty = Math.max(0, Math.floor(toNumber(review?.refundQty || review?.refund_qty || 0)));
  const refundAmount = Math.max(0, toNumber(review?.refundAmount || review?.refund_amount || 0));
  const shipQty = toNumber(row[`c_${settleContext.shipQtyColIndex}`] || "");
  const buyerPaid = toNumber(row[`c_${settleContext.buyerPaidColIndex}`] || "");
  const settleQty = shipQty - refundQty;
  const settleAmount = shipQty === 0 ? 0 : buyerPaid - refundAmount;
  return { settleQty, settleAmount };
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

function buildAmountCheckPopoverContent(checkInfo) {
  if (!checkInfo) return null;
  const detailCount = checkInfo.details?.length || 0;
  const diffAmount = checkInfo.totalIncome - checkInfo.totalSettleAmount;
  return (
    <div style={{ minWidth: 520, maxWidth: 760 }}>
      <Descriptions size="small" bordered column={1} className="match-popover-desc">
        <Descriptions.Item label="子单原始单号">{checkInfo.orderNo || "-"}</Descriptions.Item>
        <Descriptions.Item label="匹配明细总收入">{formatAmount(checkInfo.totalIncome)}</Descriptions.Item>
        <Descriptions.Item label="同子单核算金额累计">{formatAmount(checkInfo.totalSettleAmount)}</Descriptions.Item>
        <Descriptions.Item label="差额">{formatAmount(diffAmount)}</Descriptions.Item>
        <Descriptions.Item label="结果">
          {checkInfo.consistent ? (
            <Typography.Text type="success" strong>
              核算金额一致
            </Typography.Text>
          ) : (
            <Typography.Text type="danger" strong>
              核算金额不一致
            </Typography.Text>
          )}
        </Descriptions.Item>
      </Descriptions>
      <Divider style={{ margin: "10px 0" }} />
      <div className="amount-check-detail-head">
        <Typography.Text strong>累计明细</Typography.Text>
        <Typography.Text type="secondary">总计 {detailCount} 条</Typography.Text>
      </div>
      <div className="amount-check-detail-list">
        {checkInfo.details.map((it) => (
          <div key={it.key} className="amount-check-detail-item">
            行号: {it.rowNo}，文件: {it.filename || "-"}，结算金额: {formatAmount(it.settleAmount)}
          </div>
        ))}
      </div>
    </div>
  );
}

function buildMatchPopoverContent(matches = []) {
  const labels = ["来源", "文件", "行号"];
  const seen = new Set(labels);
  for (const item of matches) {
    for (const kv of (item?.detail || [])) {
      const key = String(kv?.key || "").trim();
      if (!key || seen.has(key)) continue;
      seen.add(key);
      labels.push(key);
    }
  }
  const totalIncome = sumMatchIncome(matches);
  return (
    <div className="match-popover-wrap">
      <div className="match-popover-income">总收入: {formatAmount(totalIncome)}</div>
      <div className="match-popover-matrix">
        <div className="match-popover-label-col">
          {labels.map((label) => (
            <div key={`label-${label}`} className="match-popover-label-cell">
              {label}
            </div>
          ))}
        </div>
        {matches.map((item) => {
          const detailMap = new Map();
          for (const kv of (item?.detail || [])) {
            const key = String(kv?.key || "").trim();
            if (key && !detailMap.has(key)) detailMap.set(key, kv?.value || "-");
          }
          return (
            <div key={getMatchTagKey(item)} className="match-popover-value-col">
              {labels.map((label) => {
                let value = "-";
                if (label === "来源") {
                  value = item.source === "wechat" ? "微信支付明细" : "支付宝支付明细";
                } else if (label === "文件") {
                  value = item.filename || "-";
                } else if (label === "行号") {
                  value = item.rowNo || "-";
                } else {
                  value = detailMap.get(label) || "-";
                }
                return (
                  <div key={getMatchColumnKey(item, label)} className="match-popover-value-cell">
                    {value}
                  </div>
                );
              })}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function buildManualReviewPopoverContent(review, settleValues) {
  if (!review) return null;
  const reviewType = (review.reviewType || review.review_type || "MANUAL").toUpperCase();
  const remark = String(review.remark || "").trim();
  return (
    <Descriptions size="small" bordered column={1} className="match-popover-desc">
      <Descriptions.Item label="核查类型">{reviewType === "AUTO" ? "自动核查" : "人工核查"}</Descriptions.Item>
      <Descriptions.Item label="退款数量">{review.refundQty || "-"}</Descriptions.Item>
      <Descriptions.Item label="退款金额">{review.refundAmount || "-"}</Descriptions.Item>
      <Descriptions.Item label="结算数量">{settleValues ? settleValues.settleQty : "-"}</Descriptions.Item>
      <Descriptions.Item label="结算金额">{settleValues ? formatAmount(settleValues.settleAmount) : "-"}</Descriptions.Item>
      <Descriptions.Item label="备注">{remark || "-"}</Descriptions.Item>
      <Descriptions.Item label="状态">{review.ignored ? "已忽略" : "已核查"}</Descriptions.Item>
      <Descriptions.Item label="更新时间">{review.updatedAt || "-"}</Descriptions.Item>
    </Descriptions>
  );
}

function ResizableHeaderCell(props) {
  const { onResizeStart, width, children, ...restProps } = props;
  if (!width) {
    return <th {...restProps}>{children}</th>;
  }
  return (
    <th {...restProps}>
      <div className="reconcile-resizable-th-inner">
        <span className="reconcile-resizable-th-title">{children}</span>
        <span
          className="reconcile-resize-handle"
          onMouseDown={onResizeStart}
          onClick={(e) => e.stopPropagation()}
          role="presentation"
        />
      </div>
    </th>
  );
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

function buildReconcileResultColumns({
  amountCheckMap,
  onOpenManualReview,
  salesColumns,
  settleContext
}) {
  const subOrderIdx = findSalesColumnIndex(salesColumns, ["子单原始单号"]);
  const hiddenColumnNames = new Set(["结算数量", "结算金额"]);
  const restColumns = (salesColumns || [])
    .map((name, idx) => ({ name, idx }))
    .filter((it) => {
      if (it.idx === subOrderIdx) return false;
      return !hiddenColumnNames.has(String(it.name || "").replaceAll(/\s+/g, ""));
    });
  return [
    {
      title: "匹配明细",
      dataIndex: "__matches",
      key: "__matches",
      width: 420,
      fixed: "left",
      render: (_, record) => {
        const matches = record.__matches || [];
        if (matches.length === 0) {
          const manualReview = record.__manualReview || null;
          const settleValues = buildSettleValues(record, settleContext);
          const reviewPopoverContent = buildManualReviewPopoverContent(manualReview, settleValues);
          const reviewType = (manualReview?.reviewType || manualReview?.review_type || "MANUAL").toUpperCase();
          const reviewLabel = reviewType === "AUTO" ? "自动核查" : "人工核查";
          const reviewColor = reviewType === "AUTO" ? "gold" : "green";
          return (
            <Space size={6}>
              {manualReview ? (
                <>
                  <Popover content={reviewPopoverContent} trigger="hover" placement="rightTop">
                    <Tag>未匹配</Tag>
                  </Popover>
                  <Popover content={reviewPopoverContent} trigger="hover" placement="rightTop">
                    <Tag color={reviewColor}>{reviewLabel}</Tag>
                  </Popover>
                </>
              ) : (
                <Tag>未匹配</Tag>
              )}
              <Button type="primary" size="small" onClick={() => onOpenManualReview(record)}>
                {manualReview ? "修改人工核查" : "人工核查"}
              </Button>
            </Space>
          );
        }
        const content = buildMatchPopoverContent(matches);
        const amountCheck = amountCheckMap?.get(record.key) || null;
        const amountCheckContent = buildAmountCheckPopoverContent(amountCheck);
        return (
          <Space size={[4, 4]} wrap>
            <Popover content={content} trigger="hover" placement="rightTop">
              {matches.map((it) => (
                <Tag key={getMatchTagKey(it)} color={it.source === "wechat" ? "green" : "blue"}>
                  {it.source === "wechat" ? "微信" : "支付宝"}:{it.rowNo}
                </Tag>
              ))}
            </Popover>
            {amountCheck ? (
              <Popover content={amountCheckContent} trigger="hover" placement="rightTop">
                <Tag color={amountCheck.consistent ? "success" : "error"}>
                  {amountCheck.consistent ? "核算金额一致" : "核算金额不一致"}
                </Tag>
              </Popover>
            ) : null}
          </Space>
        );
      }
    },
    {
      title: "行号",
      dataIndex: "rowNo",
      key: "rowNo",
      width: 90,
      fixed: "left"
    },
    subOrderIdx >= 0
      ? {
          title: salesColumns[subOrderIdx] || "子单原始单号",
          dataIndex: `c_${subOrderIdx}`,
          key: `c_${subOrderIdx}`,
          width: 180,
          fixed: "left",
          ellipsis: true
        }
      : null,
    ...restColumns.map(({ name, idx }) => ({
      title: name || `列${idx + 1}`,
      dataIndex: `c_${idx}`,
      key: `c_${idx}`,
      width: 180,
      ellipsis: true
    })),
    {
      title: <span className="settle-col-title">结算数量</span>,
      dataIndex: "__settleQty",
      key: "__settleQty",
      width: 120,
      fixed: "right",
      render: (_, record) => {
        const settleValues = buildSettleValues(record, settleContext);
        return <span className="settle-col-value">{settleValues.settleQty}</span>;
      }
    },
    {
      title: <span className="settle-col-title">结算金额</span>,
      dataIndex: "__settleAmount",
      key: "__settleAmount",
      width: 140,
      fixed: "right",
      render: (_, record) => {
        const settleValues = buildSettleValues(record, settleContext);
        return <span className="settle-col-value">{formatAmount(settleValues.settleAmount)}</span>;
      }
    }
  ].filter(Boolean);
}

export default function ReconcileResultsTable({
  amountCheckMap,
  filteredRows,
  onCopyOrderNo,
  onOpenManualReview,
  salesColumns,
  salesOrderNoColIndex,
  settleContext,
  tableLoading,
  tableScrollY
}) {
  const [columnWidths, setColumnWidths] = useState({});

  const startColumnResize = useCallback((event, columnKey, startWidth) => {
    event.preventDefault();
    event.stopPropagation();
    const beginX = event.clientX;
    const width = Number(startWidth) || 180;
    const onMouseMove = (moveEvent) => {
      const delta = moveEvent.clientX - beginX;
      const next = Math.max(MIN_RESIZE_COL_WIDTH, Math.round(width + delta));
      setColumnWidths((prev) => ({ ...prev, [columnKey]: next }));
    };
    const onMouseUp = () => {
      window.removeEventListener("mousemove", onMouseMove);
      window.removeEventListener("mouseup", onMouseUp);
    };
    window.addEventListener("mousemove", onMouseMove);
    window.addEventListener("mouseup", onMouseUp);
  }, []);

  const tableColumns = useMemo(
    () =>
      buildReconcileResultColumns({
        amountCheckMap,
        onOpenManualReview,
        salesColumns,
        salesOrderNoColIndex,
        settleContext
      }).map((col) => {
        const colKey = String(col.key || col.dataIndex || "");
        const rawWidth = columnWidths[colKey] ?? col.width ?? 180;
        const width = Number(rawWidth) || 180;
        const isOrderNoCol = colKey === `c_${salesOrderNoColIndex}` && salesOrderNoColIndex >= 0;
        return {
          ...col,
          render: isOrderNoCol
            ? (_, record) => <OrderNoCopyText value={record[col.dataIndex]} onCopy={onCopyOrderNo} />
            : col.render,
          width,
          onHeaderCell: () => ({
            width,
            onResizeStart: (event) => startColumnResize(event, colKey, width)
          })
        };
      }),
    [amountCheckMap, columnWidths, onCopyOrderNo, onOpenManualReview, salesColumns, salesOrderNoColIndex, settleContext, startColumnResize]
  );

  const tableComponents = useMemo(
    () => ({
      header: {
        cell: ResizableHeaderCell
      }
    }),
    []
  );

  return (
    <Table
      components={tableComponents}
      columns={tableColumns}
      dataSource={filteredRows}
      loading={tableLoading}
      size="small"
      virtual
      scroll={{ x: "max-content", y: tableScrollY }}
      pagination={false}
    />
  );
}
