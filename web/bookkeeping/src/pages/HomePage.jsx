import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { Card, Empty, Progress, Select, Skeleton, Spin, Switch, Typography } from "antd";

const TrendChartCard = lazy(() => import("../components/TrendChartCard"));

const METRICS = [
  { key: "netAsset", title: "净资产", color: "#1677ff" },
  { key: "asset", title: "资产", color: "#16a34a" },
  { key: "liability", title: "负债", color: "#dc2626" }
];

const RANGE_OPTIONS = [
  { value: "all", label: "全部" },
  { value: "1y", label: "近一年" },
  { value: "3y", label: "近三年" }
];
const RANGE_SET = new Set(RANGE_OPTIONS.map((it) => it.value));

const MOCK_DATA = [
  { ym: "2024.05", netAsset: -550178, asset: 42868, liability: 593046, remark: "" },
  { ym: "2024.06", netAsset: -531170.5, asset: 37123.5, liability: 568294, remark: "" },
  { ym: "2024.07", netAsset: -515937.55, asset: 56823.57, liability: 572761.12, remark: "" },
  { ym: "2024.08", netAsset: -523366.7, asset: 63935.72, liability: 587302.42, remark: "" },
  { ym: "2024.09", netAsset: -527260.77, asset: 62938.73, liability: 590199.5, remark: "" },
  { ym: "2024.10", netAsset: -508723, asset: 42838, liability: 551561, remark: "" },
  { ym: "2024.11", netAsset: -499419, asset: 84188, liability: 583607, remark: "" },
  { ym: "2024.12", netAsset: -497606, asset: 73250, liability: 570856, remark: "" },
  { ym: "2025.01", netAsset: -486374.01, asset: 57795.99, liability: 544170, remark: "" },
  { ym: "2025.02", netAsset: -533271.54, asset: 97201.46, liability: 630473, remark: "" },
  { ym: "2025.03", netAsset: -524059.54, asset: 96646.46, liability: 620706, remark: "" },
  { ym: "2025.04", netAsset: -508021.1, asset: 73656.5, liability: 581677.6, remark: "" },
  { ym: "2025.05", netAsset: -488740.41, asset: 61194.49, liability: 549934.9, remark: "" },
  { ym: "2025.06", netAsset: -477602.51, asset: 96059.49, liability: 573662, remark: "" },
  { ym: "2025.07", netAsset: -454445, asset: 99440, liability: 553885, remark: "" },
  { ym: "2025.08", netAsset: -414958.03, asset: 92592.28, liability: 507550.31, remark: "" },
  { ym: "2025.09", netAsset: -393522.5, asset: 150856, liability: 544378.5, remark: "" },
  { ym: "2025.10", netAsset: -391396.4, asset: 74147.6, liability: 465544, remark: "" },
  { ym: "2025.11", netAsset: -364871.48, asset: 89044.13, liability: 453915.61, remark: "" },
  { ym: "2025.12", netAsset: -347875.12, asset: 100225.78, liability: 448100.9, remark: "" },
  { ym: "2026.01", netAsset: -325817.96, asset: 150878.04, liability: 476696, remark: "" },
  { ym: "2026.02", netAsset: -307185.11, asset: 171578.31, liability: 478763.42, remark: "" },
  { ym: "2026.03", netAsset: -259458.72, asset: 156339.58, liability: 415798.3, remark: "" }
];

function parseNumber(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function renderMoneyWithUnit(value) {
  return (
    <span className="value-money value-token">
      <span>{DIFF_FORMAT.format(parseNumber(value))}</span>
      <span className="value-unit">元</span>
    </span>
  );
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

const DIFF_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});

function normalize(items) {
  return (items || [])
    .map((it) => ({
      ym: `${it.ym || ""}`,
      netAsset: parseNumber(it.netAsset ?? it.net_asset),
      asset: parseNumber(it.asset),
      liability: parseNumber(it.liability),
      remark: `${it.remark || ""}`,
      loans: (it.loans || []).map((loan) => ({
        monthlyPrincipal: parseNumber(loan.monthlyPrincipal ?? loan.monthly_principal),
        prepaymentPrincipal: parseNumber(loan.prepaymentPrincipal ?? loan.prepayment_principal)
      }))
    }))
    .filter((it) => /^\d{4}\.\d{2}$/.test(it.ym))
    .sort((a, b) => (a.ym > b.ym ? 1 : -1));
}

async function requestAssetList(includeLoan) {
  const query = new URLSearchParams({
    include_loan: includeLoan ? "true" : "false"
  });
  const res = await fetch(`/api/v1/bookkeeping/assets?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return normalize(json.items || []);
}

async function requestLoanSummaryList() {
  const res = await fetch("/api/v1/bookkeeping/loans");
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return json.items || [];
}

function buildNetAssetChanges(items, includeLoan) {
  const result = [];
  for (let i = 1; i < items.length; i++) {
    const prev = items[i - 1];
    const curr = items[i];
    const scheduledPrincipal = (curr.loans || []).reduce((sum, loan) => sum + parseNumber(loan.monthlyPrincipal), 0);
    const prepaymentPrincipal = (curr.loans || []).reduce((sum, loan) => sum + parseNumber(loan.prepaymentPrincipal), 0);
    let diff = curr.netAsset - prev.netAsset;
    if (includeLoan) {
      diff = diff - scheduledPrincipal + prepaymentPrincipal;
    }
    result.push({
      ym: curr.ym.replace(".", "-"),
      diff,
      remark: `${curr.remark || ""}`.trim()
    });
  }
  return result.reverse();
}

function filterByRange(items, range) {
  if (range === "all") return items;
  const months = range === "1y" ? 12 : 36;
  if (!Array.isArray(items) || items.length <= months) return items;
  return items.slice(items.length - months);
}

function getInitialIncludeLoan() {
  const search = new URLSearchParams(window.location.search);
  return search.get("include_loan") === "true";
}

function getInitialRange() {
  const search = new URLSearchParams(window.location.search);
  const value = search.get("range") || "all";
  return RANGE_SET.has(value) ? value : "all";
}

export default function HomePage() {
  const [loading, setLoading] = useState(true);
  const [includeLoan, setIncludeLoan] = useState(getInitialIncludeLoan);
  const [range, setRange] = useState(getInitialRange);
  const [assets, setAssets] = useState([]);
  const [loanLoading, setLoanLoading] = useState(false);
  const [loanSummaries, setLoanSummaries] = useState([]);
  const filteredAssets = useMemo(() => filterByRange(assets, range), [assets, range]);
  const netAssetChanges = buildNetAssetChanges(filteredAssets, includeLoan);

  useEffect(() => {
    let active = true;
    setLoading(true);
    (async () => {
      try {
        const remote = await requestAssetList(includeLoan);
        if (!active) return;
        const finalData = remote.length > 0 ? remote : MOCK_DATA;
        setAssets(finalData);
      } catch (_) {
        if (!active) return;
        setAssets(MOCK_DATA);
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [includeLoan]);

  useEffect(() => {
    const search = new URLSearchParams(window.location.search);
    search.set("include_loan", includeLoan ? "true" : "false");
    search.set("range", range);
    const next = `${window.location.pathname}?${search.toString()}`;
    window.history.replaceState(null, "", next);
  }, [includeLoan, range]);

  useEffect(() => {
    let active = true;
    if (!includeLoan) {
      setLoanSummaries([]);
      setLoanLoading(false);
      return () => {
        active = false;
      };
    }
    setLoanLoading(true);
    (async () => {
      try {
        const items = await requestLoanSummaryList();
        if (!active) return;
        setLoanSummaries(items);
      } catch (_) {
        if (!active) return;
        setLoanSummaries([]);
      } finally {
        if (active) setLoanLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [includeLoan]);

  function openLoanDetail(loanId) {
    const search = new URLSearchParams(window.location.search);
    search.set("view", "loan-detail");
    search.set("loan_id", loanId);
    window.location.search = search.toString();
  }

  function openLedgerMonth(ym) {
    const search = new URLSearchParams(window.location.search);
    search.set("view", "ledger-month");
    search.set("ledger_ym", ym.replace("-", "."));
    window.location.search = search.toString();
  }

  return (
    <div className="page-wrap">
      <Card className="main-card" styles={{ body: { padding: 24 } }}>
        <div className="top-row">
          <Typography.Title level={2} className="page-title">
            资产走势图
          </Typography.Title>
          <Select
            className="range-select"
            value={range}
            options={RANGE_OPTIONS}
            onChange={setRange}
            style={{ width: 128 }}
          />
        </div>
        <div className="loan-toggle-row">
          <Typography.Text className="loan-toggle-label">计算贷款</Typography.Text>
          <Switch checked={includeLoan} loading={loading} onChange={setIncludeLoan} />
        </div>

        {loading ? (
          <div className="state-wrap">
            <Spin size="large" />
          </div>
        ) : filteredAssets.length === 0 ? (
          <div className="state-wrap">
            <Empty description="暂无资产数据" />
          </div>
        ) : (
          <>
            <div className="chart-grid">
              {METRICS.map((metric) => (
                <Suspense
                  key={metric.key}
                  fallback={
                    <Card className="trend-card" styles={{ body: { padding: 18 } }}>
                      <Skeleton active paragraph={{ rows: 6 }} />
                    </Card>
                  }
                >
                  <TrendChartCard
                    title={metric.title}
                    color={metric.color}
                    items={filteredAssets}
                    metricKey={metric.key}
                  />
                </Suspense>
              ))}
            </div>
            {includeLoan ? (
              <div className="loan-list-card">
                <Typography.Title level={4} className="loan-list-title">
                  贷款汇总信息
                </Typography.Title>
                {loanLoading ? (
                  <div className="loan-list-loading">
                    <Spin />
                  </div>
                ) : loanSummaries.length === 0 ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无贷款数据" />
                ) : (
                  <div className="loan-list">
                    {loanSummaries.map((item, idx) => (
                      <button
                        type="button"
                        key={item.loanId}
                        className="loan-list-item"
                        onClick={() => openLoanDetail(item.loanId)}
                      >
                        <div className="loan-item-head">
                          <span className="loan-item-name">{item.loanName || "未命名贷款"}</span>
                          <span className="loan-item-link">查看详情</span>
                        </div>
                        <div className="loan-item-grid">
                          <span>剩余本金</span>
                          <span className="value-right">{renderMoneyWithUnit(item.remainingPrincipal)}</span>
                          <span>剩余利息</span>
                          <span className="value-right">{renderMoneyWithUnit(item.remainingInterest)}</span>
                          <span>累计还款月数</span>
                          <span className="value-right value-token">{item.repaidMonths || 0}</span>
                          <span>累计还款本金</span>
                          <span className="value-right">{renderMoneyWithUnit(item.cumulativePrincipal)}</span>
                          <span>累计还款利息</span>
                          <span className="value-right">{renderMoneyWithUnit(item.cumulativeInterest)}</span>
                        </div>
                        <div className="loan-progress-wrap">
                          <Progress
                            percent={Number(
                              calcRemainingPercent(item.initialPrincipal, item.remainingPrincipal).toFixed(2)
                            )}
                            size={["100%", 10]}
                            strokeColor={{ "0%": "#1d4ed8", "100%": "#38bdf8" }}
                            trailColor="#e6edf8"
                            status="active"
                            format={(p) => `待还本金占比 ${p}%`}
                          />
                        </div>
                        {idx < loanSummaries.length - 1 ? <div className="loan-list-divider" /> : null}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            ) : null}
            <div className="change-list-card">
              <Typography.Title level={4} className="change-list-title">
                资产变动清单
              </Typography.Title>
              {netAssetChanges.length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无变动数据" />
              ) : (
                <div className="change-list">
                  {netAssetChanges.map((item) => (
                    <button
                      type="button"
                      key={item.ym}
                      className="change-item change-item-btn"
                      onClick={() => openLedgerMonth(item.ym)}
                    >
                      <span className="change-ym">{item.ym}</span>
                      <span className="change-value-wrap">
                        <span className={item.diff >= 0 ? "change-value up" : "change-value down"}>
                          {`${item.diff >= 0 ? "+" : ""}${DIFF_FORMAT.format(item.diff)}`}
                        </span>
                        {item.remark ? <span className="change-remark">{item.remark}</span> : null}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </Card>
    </div>
  );
}
