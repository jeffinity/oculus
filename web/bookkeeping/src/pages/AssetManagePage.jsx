import { CheckOutlined, CloseOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Empty, Input, Modal, Spin, Typography, message } from "antd";
import { useEffect, useMemo, useRef, useState } from "react";

import AssetBrandIcon from "../components/AssetBrandIcon";
import MobileHeader from "../components/MobileHeader";

import { PRESET_META_MAP, SUB_TYPE_ORDER, findSubTypeMeta } from "./assetManageMeta";
import { backToHome, getCurrentYM, navigateToView, shiftYM } from "./assetManageNav";

const MONEY_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});
const ASSET_DETAIL_START_YM = "2026.04";

function parseNumber(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function normalizeItem(it) {
  return {
    detailId: `${it.detailId ?? it.detail_id ?? ""}`.trim(),
    ym: `${it.ym || ""}`.trim(),
    assetType: `${it.assetType ?? it.asset_type ?? ""}`.trim(),
    subType: `${it.subType ?? it.sub_type ?? ""}`.trim(),
    presetCode: `${it.presetCode ?? it.preset_code ?? ""}`.trim(),
    assetName: `${it.assetName ?? it.asset_name ?? ""}`.trim(),
    remark: `${it.remark || ""}`.trim(),
    amount: parseNumber(it.amount)
  };
}

async function requestAssetDetails(ym) {
  const query = new URLSearchParams({ ym });
  const res = await fetch(`/api/v1/bookkeeping/asset-details?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return (json.items || []).map(normalizeItem);
}

async function requestAssetSummaryByYM(ym) {
  const query = new URLSearchParams({ start_ym: ym, end_ym: ym });
  const res = await fetch(`/api/v1/bookkeeping/assets?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  const items = Array.isArray(json.items) ? json.items : [];
  const target = items.find((it) => `${it.ym || ""}`.trim() === ym);
  return `${target?.remark || ""}`.trim();
}

async function requestUpdateAssetRemark(ym, remark) {
  const res = await fetch(`/api/v1/bookkeeping/assets/${encodeURIComponent(ym)}/remark`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ym, remark })
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

async function requestDeleteAssetDetail(detailId) {
  const res = await fetch(`/api/v1/bookkeeping/asset-details/${encodeURIComponent(detailId)}`, {
    method: "DELETE"
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

function buildGroups(items) {
  const groupMap = new Map();
  for (const subType of SUB_TYPE_ORDER) groupMap.set(subType, { subType, items: [], subtotal: 0 });
  for (const item of items) {
    if (!groupMap.has(item.subType)) {
      groupMap.set(item.subType, { subType: item.subType, items: [], subtotal: 0 });
    }
    const group = groupMap.get(item.subType);
    group.items.push(item);
    group.subtotal += item.assetType === "liability" ? -item.amount : item.amount;
  }
  return [...groupMap.values()].filter((group) => group.items.length > 0);
}

function toDisplayAmount(item) {
  const signed = item.assetType === "liability" ? -Math.abs(item.amount) : Math.abs(item.amount);
  return `${signed < 0 ? "-" : ""}${MONEY_FORMAT.format(Math.abs(signed))}`;
}

function toDisplaySubtotal(value) {
  return `${value < 0 ? "-" : ""}${MONEY_FORMAT.format(Math.abs(value))}`;
}

function findIcon(item) {
  if (item.presetCode && item.presetCode !== "other" && PRESET_META_MAP[item.presetCode]) {
    return PRESET_META_MAP[item.presetCode].icon;
  }
  const subMeta = findSubTypeMeta(item.subType);
  return subMeta?.icon || PRESET_META_MAP.other.icon;
}

export default function AssetManagePage() {
  const [ym, setYM] = useState(getCurrentYM);
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState([]);
  const [deletingId, setDeletingId] = useState("");
  const [monthRemark, setMonthRemark] = useState("");
  const [remarkDraft, setRemarkDraft] = useState("");
  const [remarkEditing, setRemarkEditing] = useState(false);
  const [remarkSubmitting, setRemarkSubmitting] = useState(false);
  const remarkInputRef = useRef(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    (async () => {
      try {
        const [list, remark] = await Promise.all([requestAssetDetails(ym), requestAssetSummaryByYM(ym)]);
        if (!active) return;
        setItems(list);
        setMonthRemark(remark);
        setRemarkEditing(false);
        setRemarkDraft("");
      } catch {
        if (!active) return;
        setItems([]);
        setMonthRemark("");
        setRemarkEditing(false);
        setRemarkDraft("");
        message.error("资产明细加载失败");
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [ym]);

  useEffect(() => {
    const search = new URLSearchParams(window.location.search);
    search.set("view", "asset-manage");
    search.set("ym", ym);
    window.history.replaceState(null, "", `${window.location.pathname}?${search.toString()}`);
  }, [ym]);

  useEffect(() => {
    if (!remarkEditing) return;
    remarkInputRef.current?.focus({ cursor: "end" });
  }, [remarkEditing]);

  const summary = useMemo(() => {
    let asset = 0;
    let liability = 0;
    for (const item of items) {
      if (item.assetType === "liability") {
        liability += item.amount;
      } else {
        asset += item.amount;
      }
    }
    return { asset, liability, netAsset: asset - liability };
  }, [items]);
  const groups = useMemo(() => buildGroups(items), [items]);
  const isCurrentMonth = useMemo(() => {
    const now = new Date();
    const currentYM = `${now.getFullYear()}.${`${now.getMonth() + 1}`.padStart(2, "0")}`;
    return ym === currentYM;
  }, [ym]);

  async function onDelete(item) {
    setDeletingId(item.detailId);
    try {
      await requestDeleteAssetDetail(item.detailId);
      setItems((prev) => prev.filter((it) => it.detailId !== item.detailId));
      message.success("删除成功");
    } catch {
      message.error("删除失败，请稍后重试");
    } finally {
      setDeletingId("");
    }
  }

  async function onSubmitRemark() {
    const finalRemark = remarkDraft.trim();
    if (!finalRemark) {
      message.error("请输入备注内容");
      return;
    }
    setRemarkSubmitting(true);
    try {
      await requestUpdateAssetRemark(ym, finalRemark);
      setMonthRemark(finalRemark);
      setRemarkDraft("");
      setRemarkEditing(false);
      message.success("备注已提交");
    } catch {
      message.error("备注提交失败，请稍后重试");
    } finally {
      setRemarkSubmitting(false);
    }
  }

  function onCancelRemark() {
    setRemarkDraft("");
    setRemarkEditing(false);
  }

  function beginRemarkEdit() {
    setRemarkDraft(monthRemark);
    setRemarkEditing(true);
  }

  function onPrevMonth() {
    const prevYM = shiftYM(ym, -1);
    if (prevYM < ASSET_DETAIL_START_YM) {
      message.info("没有更早的数据了");
      return;
    }
    setYM(prevYM);
  }

  let content = null;
  if (loading) {
    content = (
      <div className="state-wrap">
        <Spin size="large" />
      </div>
    );
  } else if (groups.length === 0) {
    content = (
      <div className="asset-empty-wrap">
        <Empty description="本月暂无资产明细" />
      </div>
    );
  } else {
    content = (
      <div className="asset-group-list">
        {groups.map((group) => (
          <section key={group.subType} className="asset-group-block">
            <div className="asset-group-head">
              <span className="asset-group-title">{group.subType}</span>
              <span className="asset-group-subtotal">{toDisplaySubtotal(group.subtotal)}</span>
            </div>
            <div className="asset-item-list">
              {group.items.map((item) => (
                <div
                  key={item.detailId}
                  className="asset-item-row"
                  role="button"
                  tabIndex={0}
                  onClick={() =>
                    navigateToView("asset-detail-form", {
                      ym,
                      mode: "edit",
                      detail_id: item.detailId,
                      sub_type: item.subType,
                      asset_type: item.assetType,
                      preset_code: item.presetCode
                    })
                  }
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      navigateToView("asset-detail-form", {
                        ym,
                        mode: "edit",
                        detail_id: item.detailId,
                        sub_type: item.subType,
                        asset_type: item.assetType,
                        preset_code: item.presetCode
                      });
                    }
                  }}
                >
                  <AssetBrandIcon icon={findIcon(item)} />
                  <span className="asset-item-name-wrap">
                    <span className="asset-item-name">{item.assetName}</span>
                    {item.remark ? <span className="asset-item-remark">{item.remark}</span> : null}
                  </span>
                  <span className={`asset-item-amount ${item.assetType === "liability" ? "down" : ""}`}>
                    {toDisplayAmount(item)}
                  </span>
                  <Button
                    type="text"
                    size="small"
                    className="asset-item-delete"
                    icon={<DeleteOutlined />}
                    onClick={(event) => {
                      event.stopPropagation();
                      Modal.confirm({
                        title: "确认删除该资产明细？",
                        okText: "删除",
                        cancelText: "取消",
                        okButtonProps: { danger: true, loading: deletingId === item.detailId },
                        onOk: () => onDelete(item)
                      });
                    }}
                  />
                </div>
              ))}
            </div>
          </section>
        ))}
      </div>
    );
  }

  let remarkContent = <span className="asset-month-remark-placeholder" />;
  if (remarkEditing) {
    remarkContent = (
      <Input
        ref={remarkInputRef}
        value={remarkDraft}
        maxLength={80}
        placeholder="请输入备注"
        onChange={(event) => setRemarkDraft(event.target.value)}
      />
    );
  } else if (monthRemark) {
    remarkContent = <span className="asset-month-remark-text">{monthRemark}</span>;
  }

  return (
    <div className="mobile-page-wrap">
      <MobileHeader title="资产管理" onBack={backToHome} />
      <div className="mobile-page-content">
        <div className="asset-manage-content">
          <div className="asset-month-row">
            <Button type="text" onClick={onPrevMonth}>
              上月
            </Button>
            <span className="asset-month-text">{ym}</span>
            <Button type="text" disabled={isCurrentMonth} onClick={() => setYM((v) => shiftYM(v, 1))}>
              下月
            </Button>
          </div>

          <div className="asset-summary-card">
            <Typography.Text className="asset-summary-label">净资产</Typography.Text>
            <Typography.Title level={2} className="asset-summary-net">
              {toDisplaySubtotal(summary.netAsset)}
            </Typography.Title>
            <div className="asset-summary-bottom">
              <span className="asset-summary-line asset-summary-line-asset">
                <span className="asset-summary-line-label asset-summary-line-label-asset">资产：</span>
                <span className="asset-summary-line-amount asset-summary-line-amount-asset">
                  {MONEY_FORMAT.format(summary.asset)}
                </span>
              </span>
              <span className="asset-summary-line asset-summary-line-liability">
                <span className="asset-summary-line-label asset-summary-line-label-liability">负债：</span>
                <span className="asset-summary-line-amount asset-summary-line-amount-liability">
                  {MONEY_FORMAT.format(summary.liability)}
                </span>
              </span>
            </div>

            <div className="asset-month-remark-row">
              {remarkContent}
              <div className="asset-month-remark-actions">
                {remarkEditing ? (
                  <Button
                    type="text"
                    className="asset-remark-icon-btn"
                    icon={<CheckOutlined />}
                    aria-label="提交备注"
                    loading={remarkSubmitting}
                    onClick={onSubmitRemark}
                  />
                ) : null}
                <Button
                  type="text"
                  className="asset-remark-icon-btn"
                  icon={remarkEditing ? <CloseOutlined /> : monthRemark ? <EditOutlined /> : <PlusOutlined />}
                  aria-label={remarkEditing ? "取消备注" : monthRemark ? "编辑备注" : "添加备注"}
                  onClick={() => {
                    if (remarkEditing) {
                      onCancelRemark();
                      return;
                    }
                    beginRemarkEdit();
                  }}
                />
              </div>
            </div>
          </div>

          {content}
        </div>
      </div>
      <button
        type="button"
        className="asset-fab-add"
        onClick={() => navigateToView("asset-category-select", { ym })}
      >
        <PlusOutlined className="asset-fab-add-icon" />
      </button>
    </div>
  );
}
