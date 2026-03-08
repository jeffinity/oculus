import { useEffect, useMemo, useState } from "react";
import { Button, Card, DatePicker, Empty, Form, InputNumber, Progress, Select, Space, Spin, Typography, message } from "antd";
import dayjs from "dayjs";

const MONEY_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});

function parseNumber(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function parseDateValue(value) {
  if (!value) return null;
  const d = dayjs(value);
  if (!d.isValid()) return null;
  return d.format("YYYY-MM-DD");
}

function renderMoneyWithUnit(value) {
  return (
    <span className="value-money value-token">
      <span>{MONEY_FORMAT.format(parseNumber(value))}</span>
      <span className="value-unit">元</span>
    </span>
  );
}

function renderRate(value) {
  return <span className="value-token">{`${(parseNumber(value) * 100).toFixed(2)}%`}</span>;
}

function renderDate(value) {
  return <span className="value-token">{value || "--"}</span>;
}

function normalizePrepaymentMode(value) {
  if (typeof value === "number") return value;
  const raw = `${value || ""}`.trim();
  if (raw === "PREPAYMENT_MODE_KEEP_PAYMENT_SHORTEN_TERM") return 1;
  if (raw === "PREPAYMENT_MODE_KEEP_TERM_REDUCE_PAYMENT") return 2;
  const n = Number(raw);
  return Number.isFinite(n) ? n : 0;
}

function calcRemainingPercent(initialPrincipal, remainingPrincipal) {
  const initial = parseNumber(initialPrincipal);
  const remaining = parseNumber(remainingPrincipal);
  if (!(initial > 0)) return 0;
  const pct = (remaining / initial) * 100;
  if (pct < 0) return 0;
  if (pct > 100) return 100;
  return pct;
}

async function requestLoanDetail(loanId, months = 24) {
  const query = new URLSearchParams({ months: `${months}` });
  const res = await fetch(`/api/v1/bookkeeping/loans/${encodeURIComponent(loanId)}?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

async function requestAdjustLoanRate(payload) {
  const res = await fetch(`/api/v1/bookkeeping/loans/${encodeURIComponent(payload.loan_id)}/rate-adjustments`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

async function requestAddPrepayment(payload) {
  const res = await fetch(`/api/v1/bookkeeping/loans/${encodeURIComponent(payload.loan_id)}/prepayments`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

function toBack() {
  const search = new URLSearchParams(window.location.search);
  search.delete("view");
  search.delete("loan_id");
  const q = search.toString();
  window.location.href = `${window.location.pathname}${q ? `?${q}` : ""}`;
}

export default function LoanDetailPage() {
  const search = new URLSearchParams(window.location.search);
  const loanId = search.get("loan_id") || "";
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [prepaySubmitting, setPrepaySubmitting] = useState(false);
  const [detail, setDetail] = useState(null);
  const [msgApi, contextHolder] = message.useMessage();
  const [form] = Form.useForm();
  const [prepayForm] = Form.useForm();

  useEffect(() => {
    let active = true;
    if (!loanId) {
      setLoading(false);
      return () => {
        active = false;
      };
    }
    setLoading(true);
    (async () => {
      try {
        const data = await requestLoanDetail(loanId, 24);
        if (!active) return;
        setDetail(data);
      } catch (_) {
        if (!active) return;
        setDetail(null);
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [loanId]);

  const summary = detail?.summary;
  const currentRate = parseNumber(summary?.annualRate);

  const planData = useMemo(
    () =>
      (detail?.plans || []).map((it, idx) => ({
        key: `${it.period}-${it.dueDate}-${idx}`,
        period: it.period,
        dueDate: it.dueDate,
        ym: it.ym,
        monthlyPrincipal: parseNumber(it.monthlyPrincipal),
        monthlyInterest: parseNumber(it.monthlyInterest),
        remainingPrincipal: parseNumber(it.remainingPrincipal)
      })),
    [detail]
  );
  const rateHistoryData = useMemo(
    () =>
      (detail?.rateAdjustments || [])
        .map((it, idx) => {
          const delta = parseNumber(it.deltaBps);
          return {
            key: `${it.effectiveDate}-${idx}`,
            effectiveDate: it.effectiveDate,
            previousAnnualRate: parseNumber(it.previousAnnualRate),
            newAnnualRate: parseNumber(it.newAnnualRate),
            deltaBps: delta
          };
        })
        .sort((a, b) => (a.effectiveDate < b.effectiveDate ? 1 : -1)),
    [detail]
  );
  const prepaymentHistoryData = useMemo(
    () =>
      (detail?.prepayments || [])
        .map((it, idx) => ({
          key: `${it.prepaymentDate}-${idx}`,
          prepaymentDate: it.prepaymentDate,
          amount: parseNumber(it.amount),
          mode: normalizePrepaymentMode(it.mode),
          savedInterest: parseNumber(it.savedInterest ?? it.saved_interest)
        }))
        .sort((a, b) => (a.prepaymentDate < b.prepaymentDate ? 1 : -1)),
    [detail]
  );

  async function onAdjust(values) {
    if (!summary?.loanId) return;
    const effectiveDate = parseDateValue(values.effectiveDate);
    if (!effectiveDate) {
      msgApi.error("生效日期格式错误");
      return;
    }
    const bps = Number(values.bps);
    if (!Number.isFinite(bps) || bps <= 0 || bps > 10000) {
      msgApi.error("调整基点必须在 0~10000 之间");
      return;
    }
    const sign = values.direction === "+" ? 1 : -1;
    const delta = sign * bps * 0.0001;
    const newRate = currentRate + delta;
    if (!(newRate >= 0 && newRate < 1)) {
      msgApi.error("调整后年利率超出有效范围");
      return;
    }

    setSubmitting(true);
    try {
      await requestAdjustLoanRate({
        loan_id: summary.loanId,
        effective_date: effectiveDate,
        annual_rate: newRate.toFixed(6)
      });
      const latest = await requestLoanDetail(summary.loanId, 24);
      setDetail(latest);
      form.resetFields();
      msgApi.success(`调息成功，最新年利率 ${newRate.toFixed(4)}`);
    } catch (_) {
      msgApi.error("调息失败，请稍后重试");
    } finally {
      setSubmitting(false);
    }
  }

  async function onPrepay(values) {
    if (!summary?.loanId) return;
    const prepaymentDate = parseDateValue(values.prepaymentDate);
    if (!prepaymentDate) {
      msgApi.error("提前还款日期格式错误");
      return;
    }
    const amount = Number(values.amount);
    if (!Number.isFinite(amount) || amount <= 0) {
      msgApi.error("提前还款金额必须大于 0");
      return;
    }
    const mode = Number(values.mode);
    if (mode !== 1 && mode !== 2) {
      msgApi.error("提前还款方式不正确");
      return;
    }
    setPrepaySubmitting(true);
    try {
      await requestAddPrepayment({
        loan_id: summary.loanId,
        prepayment_date: prepaymentDate,
        amount: amount.toFixed(2),
        mode
      });
      const latest = await requestLoanDetail(summary.loanId, 24);
      setDetail(latest);
      prepayForm.resetFields();
      msgApi.success("提前还款已记录并重算后续计划");
    } catch (_) {
      msgApi.error("提前还款提交失败，请稍后重试");
    } finally {
      setPrepaySubmitting(false);
    }
  }

  return (
    <div className="page-wrap">
      {contextHolder}
      <Card className="main-card" styles={{ body: { padding: 24 } }}>
        <div className="top-row">
          <Typography.Title level={2} className="page-title">
            贷款详情
          </Typography.Title>
          <Button onClick={toBack}>返回资产总览</Button>
        </div>
        {loading ? (
          <div className="state-wrap">
            <Spin size="large" />
          </div>
        ) : !loanId || !summary ? (
          <div className="state-wrap">
            <Empty description="未找到贷款详情" />
          </div>
        ) : (
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <div className="loan-detail-summary">
              <div className="loan-detail-row loan-detail-row-full">
                <span>贷款名称</span>
                <span className="value-right">{summary.loanName || "--"}</span>
              </div>
              <div className="loan-detail-row">
                <span>剩余本金</span>
                <span className="value-right">{renderMoneyWithUnit(summary.remainingPrincipal)}</span>
                <span>剩余利息</span>
                <span className="value-right">{renderMoneyWithUnit(summary.remainingInterest)}</span>
              </div>
              <div className="loan-detail-row">
                <span>累计还款本金</span>
                <span className="value-right">{renderMoneyWithUnit(summary.cumulativePrincipal)}</span>
                <span>累计还款利息</span>
                <span className="value-right">{renderMoneyWithUnit(summary.cumulativeInterest)}</span>
              </div>
              <div className="loan-detail-row">
                <span>累计还款月数</span>
                <span className="value-right">{summary.repaidMonths || 0}</span>
                <span>当前年利率</span>
                <span className="value-right">{renderRate(currentRate)}</span>
              </div>
              <div className="loan-detail-row">
                <span>贷款日期</span>
                <span className="value-right">{renderDate(summary.loanDate)}</span>
                <span>下次还款日</span>
                <span className="value-right">{renderDate(summary.nextDueDate)}</span>
              </div>
              <div className="loan-progress-wrap">
                <Progress
                  percent={Number(calcRemainingPercent(summary.initialPrincipal, summary.remainingPrincipal).toFixed(2))}
                  size={["100%", 12]}
                  strokeColor={{ "0%": "#1d4ed8", "100%": "#38bdf8" }}
                  trailColor="#e6edf8"
                  status="active"
                  format={(p) => `待还本金占比 ${p}%`}
                />
              </div>
            </div>

            <Card className="rate-adjust-card" styles={{ body: { padding: 16 } }}>
              <Typography.Title level={5} style={{ marginTop: 0 }}>
                调整利率
              </Typography.Title>
              <Form
                form={form}
                layout="inline"
                className="rate-adjust-form"
                initialValues={{ direction: "-", bps: 25, effectiveDate: dayjs() }}
                onFinish={onAdjust}
              >
                <Form.Item
                  label="生效日期"
                  name="effectiveDate"
                  rules={[
                    { required: true, message: "请选择生效日期" },
                    {
                      validator: (_, value) => (parseDateValue(value) ? Promise.resolve() : Promise.reject(new Error("请选择有效日期")))
                    }
                  ]}
                >
                  <DatePicker allowClear={false} format="YYYY-MM-DD" style={{ width: 140 }} />
                </Form.Item>
                <Form.Item label="方向" name="direction" rules={[{ required: true }]}>
                  <Select
                    style={{ width: 96 }}
                    options={[
                      { value: "-", label: "下调(-)" },
                      { value: "+", label: "上调(+)" }
                    ]}
                  />
                </Form.Item>
                <Form.Item
                  label="基点"
                  name="bps"
                  rules={[
                    { required: true, message: "请输入基点" },
                    {
                      validator: (_, value) => {
                        const n = Number(value);
                        if (!Number.isInteger(n) || n <= 0 || n > 10000) {
                          return Promise.reject(new Error("需为 1~10000 的正整数"));
                        }
                        return Promise.resolve();
                      }
                    }
                  ]}
                >
                  <InputNumber min={1} max={10000} precision={0} step={1} style={{ width: 140 }} />
                </Form.Item>
                <Form.Item>
                  <Button type="primary" htmlType="submit" loading={submitting}>
                    提交调息
                  </Button>
                </Form.Item>
              </Form>
              <Typography.Text type="secondary" className="rate-adjust-tip">
                单位基点：1 bps = 0.01%，默认下调。
              </Typography.Text>

              <Typography.Title level={5} style={{ marginTop: 16, marginBottom: 8 }}>
                提前还款
              </Typography.Title>
              <Form
                form={prepayForm}
                layout="inline"
                className="rate-adjust-form"
                initialValues={{ prepaymentDate: dayjs(), mode: 1 }}
                onFinish={onPrepay}
              >
                <Form.Item
                  label="还款日期"
                  name="prepaymentDate"
                  rules={[
                    { required: true, message: "请选择还款日期" },
                    {
                      validator: (_, value) =>
                        parseDateValue(value) ? Promise.resolve() : Promise.reject(new Error("请选择有效日期"))
                    }
                  ]}
                >
                  <DatePicker allowClear={false} format="YYYY-MM-DD" style={{ width: 140 }} />
                </Form.Item>
                <Form.Item
                  label="还款金额"
                  name="amount"
                  rules={[
                    { required: true, message: "请输入还款金额" },
                    {
                      validator: (_, value) => {
                        const n = Number(value);
                        if (!Number.isFinite(n) || n <= 0) {
                          return Promise.reject(new Error("金额必须大于 0"));
                        }
                        return Promise.resolve();
                      }
                    }
                  ]}
                >
                  <InputNumber min={0.01} precision={2} step={1000} style={{ width: 160 }} />
                </Form.Item>
                <Form.Item label="还款方式" name="mode" rules={[{ required: true, message: "请选择还款方式" }]}>
                  <Select
                    style={{ width: 220 }}
                    options={[
                      { value: 1, label: "月供不变，缩短期限" },
                      { value: 2, label: "期限不变，降低月供" }
                    ]}
                  />
                </Form.Item>
                <Form.Item>
                  <Button type="primary" htmlType="submit" loading={prepaySubmitting}>
                    提交提前还款
                  </Button>
                </Form.Item>
              </Form>
              <Typography.Text type="secondary" className="rate-adjust-tip">
                还款日之后提交的提前还款将从该日期起重算后续本息计划。
              </Typography.Text>
            </Card>

            <Card className="plan-card" styles={{ body: { padding: 8 } }}>
              <Typography.Title level={5} className="plan-title">
                下次还款日起未来 24 个月计划
              </Typography.Title>
              {planData.length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无计划数据" />
              ) : (
                <div className="repayment-plan-list">
                  {planData.map((item) => (
                    <div key={item.key} className="repayment-plan-item">
                      <div className="repayment-plan-head">
                        <span className="repayment-plan-ym">{item.ym}</span>
                        <span className="repayment-plan-date">{item.dueDate}</span>
                      </div>
                      <div className="repayment-plan-grid">
                        <span>期数</span>
                        <span className="value-right value-token">{item.period}</span>
                        <span>月供本金</span>
                        <span className="value-right">{renderMoneyWithUnit(item.monthlyPrincipal)}</span>
                        <span>月供利息</span>
                        <span className="value-right">{renderMoneyWithUnit(item.monthlyInterest)}</span>
                        <span>剩余本金</span>
                        <span className="value-right">{renderMoneyWithUnit(item.remainingPrincipal)}</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>

            <Card className="plan-card" styles={{ body: { padding: 8 } }}>
              <Typography.Title level={5} className="plan-title">
                调息历史
              </Typography.Title>
              {rateHistoryData.length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无调息记录" />
              ) : (
                <div className="history-card-list">
                  {rateHistoryData.map((item) => (
                    <div key={item.key} className="history-card-item">
                      <div className="history-card-head">
                        <span className="history-card-title">调息生效日</span>
                        <span className="history-card-date">{item.effectiveDate}</span>
                      </div>
                      <div className="history-card-grid">
                        <span>调整前利率</span>
                        <span className="value-right">{renderRate(item.previousAnnualRate)}</span>
                        <span>调整后利率</span>
                        <span className="value-right">{renderRate(item.newAnnualRate)}</span>
                        <span>调整幅度(bps)</span>
                        <span className="value-right">
                          <span className={item.deltaBps >= 0 ? "change-value up" : "change-value down"}>
                            {`${item.deltaBps >= 0 ? "+" : ""}${item.deltaBps.toFixed(2)}`}
                          </span>
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>

            <Card className="plan-card" styles={{ body: { padding: 8 } }}>
              <Typography.Title level={5} className="plan-title">
                提前还款历史
              </Typography.Title>
              {prepaymentHistoryData.length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无提前还款记录" />
              ) : (
                <div className="history-card-list">
                  {prepaymentHistoryData.map((item) => (
                    <div key={item.key} className="history-card-item">
                      <div className="history-card-head">
                        <span className="history-card-title">还款日期</span>
                        <span className="history-card-date">{item.prepaymentDate}</span>
                      </div>
                      <div className="history-card-grid">
                        <span>还款金额</span>
                        <span className="value-right">{renderMoneyWithUnit(item.amount)}</span>
                        <span>还款方式</span>
                        <span className="value-right">
                          <span className="prepayment-mode-value">
                          {item.mode === 1 ? "月供不变，缩短期限" : item.mode === 2 ? "期限不变，降低月供" : "--"}
                          </span>
                        </span>
                        <span>节省利息</span>
                        <span className="value-right">
                          <span className="prepayment-saved-wrap">
                            <span className="prepayment-saved-text">太棒了，本次节省了</span>
                            <span className="prepayment-saved-value">
                              <span className="prepayment-saved-amount">{MONEY_FORMAT.format(item.savedInterest)}</span>
                              <span className="prepayment-saved-unit">元利息</span>
                            </span>
                          </span>
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </Space>
        )}
      </Card>
    </div>
  );
}
