import { Button, Form, Input, Select, Spin, message } from "antd";
import { useEffect, useMemo, useRef, useState } from "react";

import MobileHeader from "../components/MobileHeader";

import { PRESET_META_MAP, findSubTypeMeta } from "./assetManageMeta";
import { backToAssetManage, backToCategorySelect, getCurrentYM, getSearch } from "./assetManageNav";

const MONEY_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});

function parseNumber(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function round2(value) {
  return Math.round(value * 100) / 100;
}

function parseYM(raw) {
  const value = `${raw || ""}`.trim();
  if (!/^\d{4}\.\d{2}$/.test(value)) return null;
  const year = Number(value.slice(0, 4));
  const month = Number(value.slice(5, 7));
  if (!Number.isInteger(year) || !Number.isInteger(month) || month < 1 || month > 12) return null;
  return { year, month };
}

function diffMonths(targetYM, startYM) {
  return (targetYM.year - startYM.year) * 12 + (targetYM.month - startYM.month);
}

function formatMoney(value) {
  return MONEY_FORMAT.format(round2(parseNumber(value)));
}

function buildConsumerLoanStats(item, targetYMRaw) {
  if (!item || item.subType !== "消费贷款") return null;
  const startYM = parseYM(item.consumerLoanStartYm);
  const targetYM = parseYM(targetYMRaw);
  const totalAmount = round2(item.consumerLoanTotalAmount);
  const remainingAmount = round2(item.amount);
  const termMonths = parseNumber(item.consumerLoanTermMonths);
  if (!startYM || !targetYM || totalAmount <= 0 || termMonths <= 0) return null;
  const paidPeriods = Math.max(0, Math.min(termMonths, diffMonths(targetYM, startYM)));
  const remainingPeriods = Math.max(0, termMonths - paidPeriods);
  const repaidPrincipal = Math.max(0, round2(totalAmount - remainingAmount));
  return {
    paidPeriods,
    repaidPrincipal,
    remainingPeriods,
    remainingPrincipal: Math.max(0, remainingAmount)
  };
}

function sanitizeAmountInput(value) {
  const cleaned = `${value || ""}`.replaceAll(/[^.\d]/g, "");
  const [head, ...rest] = cleaned.split(".");
  const integerPart = head || "";
  const decimalRaw = rest.join("");
  if (rest.length === 0) return integerPart;
  return `${integerPart}.${decimalRaw.slice(0, 2)}`;
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
    amount: parseNumber(it.amount),
    consumerLoanTotalAmount: parseNumber(it.consumerLoanTotalAmount ?? it.consumer_loan_total_amount),
    consumerLoanStartYm: `${it.consumerLoanStartYm ?? it.consumer_loan_start_ym ?? ""}`.trim(),
    consumerLoanTermMonths: parseNumber(it.consumerLoanTermMonths ?? it.consumer_loan_term_months)
  };
}

const CONSUMER_TERM_OPTIONS = [3, 6, 12, 24, 36, 60].map((v) => ({ label: `${v}期`, value: v }));

async function requestAssetDetails(ym) {
  const query = new URLSearchParams({ ym });
  const res = await fetch(`/api/v1/bookkeeping/asset-details?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return (json.items || []).map(normalizeItem);
}

async function requestCreate(payload) {
  const res = await fetch("/api/v1/bookkeeping/asset-details", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

async function requestUpdate(detailId, payload) {
  const res = await fetch(`/api/v1/bookkeeping/asset-details/${encodeURIComponent(detailId)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

export default function AssetDetailFormPage() {
  const [form] = Form.useForm();
  const nameInputRef = useRef(null);
  const remarkInputRef = useRef(null);
  const amountInputRef = useRef(null);
  const search = getSearch();
  const ym = getCurrentYM();
  const mode = `${search.get("mode") || "create"}`.trim();
  const detailId = `${search.get("detail_id") || ""}`.trim();
  const querySubType = `${search.get("sub_type") || ""}`.trim();
  const queryAssetType = `${search.get("asset_type") || ""}`.trim();
  const queryPresetCode = `${search.get("preset_code") || "other"}`.trim();

  const [loading, setLoading] = useState(mode === "edit");
  const [submitting, setSubmitting] = useState(false);
  const [resolvedSubType, setResolvedSubType] = useState(querySubType);
  const [resolvedAssetType, setResolvedAssetType] = useState(queryAssetType);
  const [resolvedPresetCode, setResolvedPresetCode] = useState(queryPresetCode);
  const [currentItem, setCurrentItem] = useState(null);
  const focusedByGestureRef = useRef(false);
  const isConsumerLoan = resolvedSubType === "消费贷款";
  const consumerLoanStats = useMemo(() => buildConsumerLoanStats(currentItem, ym), [currentItem, ym]);

  const preset = PRESET_META_MAP[resolvedPresetCode] || PRESET_META_MAP.other;
  const subMeta = findSubTypeMeta(resolvedSubType);
  const hasPresetList = (subMeta?.presets || []).length > 0;

  function focusAmountInputWithFallback() {
    amountInputRef.current?.focus?.({ cursor: "all" });
    const nativeInput =
      amountInputRef.current?.input || amountInputRef.current?.nativeElement?.querySelector?.("input");
    nativeInput?.focus?.();
    nativeInput?.click?.();
  }

  useEffect(() => {
    if (mode !== "edit" || !detailId) {
      if (mode !== "edit") {
        setResolvedSubType(querySubType);
        setResolvedAssetType(queryAssetType);
        setResolvedPresetCode(queryPresetCode);
        setCurrentItem(null);
        let defaultName = "";
        if (queryPresetCode) {
          defaultName = preset.name;
        } else {
          defaultName = subMeta?.title || querySubType;
        }
        form.setFieldsValue({
          assetName: defaultName,
          remark: "",
          amount: "",
          consumerLoanStartYm: ym,
          consumerLoanTermMonths: 12
        });
      }
      setLoading(false);
      return () => {};
    }

    let active = true;
    setLoading(true);
    (async () => {
      try {
        const list = await requestAssetDetails(ym);
        if (!active) return;
        const current = list.find((it) => it.detailId === detailId);
        if (!current) {
          message.error("未找到资产明细");
          backToAssetManage(ym);
          return;
        }
        setCurrentItem(current);
        setResolvedSubType(current.subType || querySubType);
        setResolvedAssetType(current.assetType || queryAssetType);
        setResolvedPresetCode(current.presetCode || queryPresetCode);
        form.setFieldsValue({
          assetName: current.assetName,
          remark: current.remark,
          amount: current.amount ? String(current.amount) : "",
          consumerLoanTotalAmount: current.consumerLoanTotalAmount ? String(current.consumerLoanTotalAmount) : "",
          consumerLoanStartYm: current.consumerLoanStartYm || ym,
          consumerLoanTermMonths: current.consumerLoanTermMonths || 12
        });
      } catch {
        if (!active) return;
        message.error("加载资产明细失败");
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [detailId, form, hasPresetList, mode, preset.name, queryAssetType, queryPresetCode, querySubType, subMeta?.title, ym]);

  useEffect(() => {
    let timer = 0;
    let raf = 0;
    if (!loading) {
      timer = window.setTimeout(() => {
        focusAmountInputWithFallback();
      }, 0);
      raf = window.requestAnimationFrame(() => {
        focusAmountInputWithFallback();
      });
    }
    return () => {
      if (timer) window.clearTimeout(timer);
      if (raf) window.cancelAnimationFrame(raf);
    };
  }, [loading]);

  useEffect(() => {
    if (loading) return () => {};

    const onFirstGesture = () => {
      if (focusedByGestureRef.current) return;
      focusedByGestureRef.current = true;
      focusAmountInputWithFallback();
    };

    window.addEventListener("touchstart", onFirstGesture, { capture: true });
    window.addEventListener("pointerdown", onFirstGesture, { capture: true });

    return () => {
      window.removeEventListener("touchstart", onFirstGesture, { capture: true });
      window.removeEventListener("pointerdown", onFirstGesture, { capture: true });
    };
  }, [loading]);

  const pageTitle = useMemo(() => (mode === "edit" ? "编辑资产明细" : "新增资产明细"), [mode]);
  const onBack = useMemo(() => {
    if (mode === "edit") {
      return () => backToAssetManage(ym);
    }
    return () => backToCategorySelect(ym);
  }, [mode, ym]);

  async function onFinish(values) {
    const assetName = `${values.assetName || ""}`.trim();
    const remark = `${values.remark || ""}`.trim();
    const amountRaw = sanitizeAmountInput(values.amount);
    const amount = Number(amountRaw);
    if (!assetName) {
      message.error("请输入资产名称");
      return;
    }
    let consumerLoanPayload = null;
    if (isConsumerLoan) {
      const totalRaw = sanitizeAmountInput(values.consumerLoanTotalAmount || values.amount);
      const total = Number(totalRaw);
      const startYM = `${values.consumerLoanStartYm || ""}`.trim();
      const termMonths = Number(values.consumerLoanTermMonths);
      if (!/^\d+(\.\d{1,2})?$/.test(totalRaw) || !Number.isFinite(total) || total <= 0) {
        message.error("请输入消费贷总金额");
        return;
      }
      if (!/^\d{4}\.\d{2}$/.test(startYM)) {
        message.error("开始月份格式应为 YYYY.MM");
        return;
      }
      if (![3, 6, 12, 24, 36, 60].includes(termMonths)) {
        message.error("期数只支持 3/6/12/24/36/60");
        return;
      }
      consumerLoanPayload = {
        consumer_loan_total_amount: total.toFixed(2),
        consumer_loan_start_ym: startYM,
        consumer_loan_term_months: termMonths
      };
    } else if (!/^\d+(\.\d{1,2})?$/.test(amountRaw) || !Number.isFinite(amount) || amount <= 0) {
      message.error("请输入大于 0 的金额");
      return;
    }
    setSubmitting(true);
    try {
      const payload = {
        ym: isConsumerLoan ? consumerLoanPayload.consumer_loan_start_ym : ym,
        asset_type: resolvedAssetType,
        sub_type: resolvedSubType,
        preset_code: resolvedPresetCode,
        asset_name: assetName,
        remark,
        amount: isConsumerLoan ? consumerLoanPayload.consumer_loan_total_amount : amount.toFixed(2),
        ...(consumerLoanPayload || {})
      };
      if (mode === "edit") {
        await requestUpdate(detailId, { ...payload, detail_id: detailId });
      } else {
        await requestCreate(payload);
      }
      message.success(mode === "edit" ? "更新成功" : "保存成功");
      backToAssetManage(ym);
    } catch {
      message.error(mode === "edit" ? "更新失败" : "保存失败");
    } finally {
      setSubmitting(false);
    }
  }

  function handleAmountKeyDown(event) {
    const allowControlKeys = new Set([
      "Backspace",
      "Delete",
      "Tab",
      "Enter",
      "ArrowLeft",
      "ArrowRight",
      "ArrowUp",
      "ArrowDown",
      "Home",
      "End"
    ]);
    if (event.ctrlKey || event.metaKey || allowControlKeys.has(event.key)) {
      return;
    }

    if (/^\d$/.test(event.key)) {
      return;
    }
    if (event.key === ".") {
      const inputEl = event.currentTarget;
      if (!inputEl.value.includes(".")) return;
    }
    event.preventDefault();
  }

  function focusField(field) {
    if (field === "assetName") {
      nameInputRef.current?.focus?.({ cursor: "all" });
      return;
    }
    if (field === "remark") {
      remarkInputRef.current?.focus?.({ cursor: "all" });
      return;
    }
    amountInputRef.current?.focus?.({ cursor: "all" });
  }

  return (
    <div className="mobile-page-wrap">
      <MobileHeader title={pageTitle} onBack={onBack} />
      <div className="mobile-page-content">
        <div className="asset-form-content">
        {loading ? (
          <div className="state-wrap">
            <Spin size="large" />
          </div>
        ) : (
          <Form className="asset-form asset-sheet-form" form={form} onFinish={onFinish}>
            <Form.Item className="asset-sheet-item">
              <div
                className="asset-sheet-row"
                role="button"
                tabIndex={0}
                onClick={() => focusField("assetName")}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    focusField("assetName");
                  }
                }}
              >
                <span className="asset-sheet-label">名称</span>
                <Form.Item
                  name="assetName"
                  noStyle
                  rules={[{ required: true, whitespace: true, message: "请输入资产名称" }]}
                >
                  <Input
                    ref={nameInputRef}
                    className="asset-sheet-input"
                    placeholder={subMeta?.title || "请输入名称"}
                    maxLength={48}
                    onClick={(event) => event.stopPropagation()}
                  />
                </Form.Item>
              </div>
            </Form.Item>
            <Form.Item className="asset-sheet-item">
              <div
                className="asset-sheet-row"
                role="button"
                tabIndex={0}
                onClick={() => focusField("remark")}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    focusField("remark");
                  }
                }}
              >
                <span className="asset-sheet-label">备注</span>
                <Form.Item name="remark" noStyle>
                  <Input
                    ref={remarkInputRef}
                    className="asset-sheet-input"
                    placeholder="(选填)"
                    maxLength={80}
                    onClick={(event) => event.stopPropagation()}
                  />
                </Form.Item>
              </div>
            </Form.Item>
            <Form.Item className="asset-sheet-item">
              <div
                className="asset-sheet-row"
                role="button"
                tabIndex={0}
                onClick={() => focusField("amount")}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    focusField("amount");
                  }
                }}
              >
                <span className="asset-sheet-label">{isConsumerLoan ? "总金额" : "余额"}</span>
                <Form.Item
                    name={isConsumerLoan ? "consumerLoanTotalAmount" : "amount"}
                    noStyle
                    rules={[{ required: true, message: "请输入金额" }]}
                  >
                  <Input
                    ref={amountInputRef}
                    className="asset-sheet-input"
                    placeholder="请输入金额"
                    // eslint-disable-next-line jsx-a11y/no-autofocus
                    autoFocus
                    inputMode="decimal"
                    pattern="[0-9.]*"
                    enterKeyHint="done"
                    onKeyDown={handleAmountKeyDown}
                    onChange={(event) => {
                      const next = sanitizeAmountInput(event.target.value);
                      form.setFieldValue(isConsumerLoan ? "consumerLoanTotalAmount" : "amount", next);
                    }}
                    onClick={(event) => event.stopPropagation()}
                  />
                </Form.Item>
              </div>
            </Form.Item>
            {isConsumerLoan ? (
              <Form.Item className="asset-sheet-item">
                <div className="asset-sheet-row">
                  <span className="asset-sheet-label">开始月份</span>
                  <Form.Item
                    name="consumerLoanStartYm"
                    noStyle
                    rules={[{ required: true, message: "请输入开始月份" }]}
                  >
                    <Input className="asset-sheet-input" placeholder="YYYY.MM" maxLength={7} />
                  </Form.Item>
                </div>
              </Form.Item>
            ) : null}
            {isConsumerLoan ? (
              <Form.Item className="asset-sheet-item">
                <div className="asset-sheet-row">
                  <span className="asset-sheet-label">期数</span>
                  <Form.Item
                    name="consumerLoanTermMonths"
                    noStyle
                    rules={[{ required: true, message: "请选择期数" }]}
                  >
                    <Select
                      className="asset-sheet-input"
                      options={CONSUMER_TERM_OPTIONS}
                      placeholder="请选择期数"
                    />
                  </Form.Item>
                </div>
              </Form.Item>
            ) : null}
            {isConsumerLoan && mode === "edit" && consumerLoanStats ? (
              <Form.Item className="asset-sheet-item">
                <div className="asset-sheet-row asset-sheet-row-readonly">
                  <span className="asset-sheet-label">已归还</span>
                  <div className="asset-sheet-static">
                    <span>{`${consumerLoanStats.paidPeriods}期`}</span>
                    <span>{`累计归还本金 ${formatMoney(consumerLoanStats.repaidPrincipal)}`}</span>
                  </div>
                </div>
              </Form.Item>
            ) : null}
            {isConsumerLoan && mode === "edit" && consumerLoanStats ? (
              <Form.Item className="asset-sheet-item">
                <div className="asset-sheet-row asset-sheet-row-readonly">
                  <span className="asset-sheet-label">剩余</span>
                  <div className="asset-sheet-static">
                    <span>{`${consumerLoanStats.remainingPeriods}期`}</span>
                    <span>{`剩余本金 ${formatMoney(consumerLoanStats.remainingPrincipal)}`}</span>
                  </div>
                </div>
              </Form.Item>
            ) : null}
            <Form.Item className="asset-form-submit-wrap">
              <Button className="asset-form-submit" type="primary" htmlType="submit" loading={submitting}>
                保存
              </Button>
            </Form.Item>
          </Form>
        )}
        </div>
      </div>
    </div>
  );
}
