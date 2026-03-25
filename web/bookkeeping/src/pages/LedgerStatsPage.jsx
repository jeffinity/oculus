/* eslint-disable complexity, max-lines-per-function */
import { Line } from "@ant-design/charts";
import { Button, Card, Empty, Space, Spin, Tabs, Typography } from "antd";
import { useEffect, useMemo, useState } from "react";

const MONEY_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});

const DIMENSION_TAB_ITEMS = [
  { label: "按月", key: "month" },
  { label: "按年", key: "year" }
];

function parseNumber(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function toStringList(value) {
  return Array.isArray(value) ? value.map((it) => `${it}`) : [];
}

function toTrendPoints(value) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((it) => ({
    key: `${it.key || ""}`,
    expense: parseNumber(it.expense)
  }));
}

function toExpenseRankings(value) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((it, idx) => ({
    rank: Number(it.rank) || idx + 1,
    category: `${it.category || "未分类"}`,
    expense: parseNumber(it.expense)
  }));
}

function normalizeReply(json) {
  return {
    dimension: `${json.dimension || "month"}`,
    ym: `${json.ym || ""}`,
    year: `${json.year || ""}`,
    availableYms: toStringList(json.availableYms ?? json.available_yms),
    availableYears: toStringList(json.availableYears ?? json.available_years),
    trendPoints: toTrendPoints(json.trendPoints ?? json.trend_points),
    expenseRankings: toExpenseRankings(json.expenseRankings ?? json.expense_rankings),
    totalExpense: parseNumber(json.totalExpense ?? json.total_expense)
  };
}

function getInitialDimension() {
  const search = new URLSearchParams(window.location.search);
  const value = `${search.get("dimension") || "month"}`.toLowerCase();
  return value === "year" ? "year" : "month";
}

function getInitialYM() {
  const search = new URLSearchParams(window.location.search);
  return `${search.get("ledger_ym") || ""}`.trim();
}

function getInitialYear() {
  const search = new URLSearchParams(window.location.search);
  return `${search.get("year") || ""}`.trim();
}

async function requestLedgerStats({ dimension, ym, year }) {
  const query = new URLSearchParams({ dimension });
  if (dimension === "month" && ym) query.set("ym", ym);
  if (dimension === "year" && year) query.set("year", year);
  const res = await fetch(`/api/v1/bookkeeping/ledgers/stats?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return normalizeReply(json);
}

function toBillList(ym) {
  const search = new URLSearchParams(window.location.search);
  search.set("view", "ledger-month");
  search.delete("dimension");
  search.delete("year");
  if (/^\d{4}\.\d{2}$/.test(ym)) {
    search.set("ledger_ym", ym);
  }
  window.location.search = search.toString();
}

function formatPointLabel(pointKey, dimension) {
  if (dimension === "month") {
    const parts = pointKey.split("-");
    if (parts.length === 3) {
      return `${parts[1]}-${parts[2]}`;
    }
    return pointKey;
  }
  return pointKey.replace(".", "-");
}

function renderStatsState(loading, chartData, chartConfig, stats) {
  if (loading) {
    return (
      <div className="state-wrap">
        <Spin size="large" />
      </div>
    );
  }
  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <Card className="ledger-stats-chart-card" styles={{ body: { padding: 16 } }}>
        <div className="ledger-stats-chart-head">
          <Typography.Title level={4} style={{ margin: 0 }}>
            支出趋势
          </Typography.Title>
          <Typography.Text className="ledger-stats-total-expense">
            总支出: {MONEY_FORMAT.format(stats?.totalExpense || 0)}
          </Typography.Text>
        </div>
        {chartData.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无趋势数据" /> : <Line {...chartConfig} />}
      </Card>

      <Card className="ledger-stats-ranking-card" styles={{ body: { padding: 16 } }}>
        <Typography.Title level={4} style={{ marginTop: 0 }}>
          支出排行榜
        </Typography.Title>
        {(stats?.expenseRankings || []).length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无排行数据" />
        ) : (
          <div className="ledger-ranking-list">
            {(stats?.expenseRankings || []).map((item) => (
              <div key={`${item.rank}-${item.category}`} className="ledger-ranking-item">
                <span className="ledger-ranking-left">
                  <span className="ledger-ranking-rank">#{item.rank}</span>
                  <span className="ledger-ranking-category">{item.category || "未分类"}</span>
                </span>
                <span className="ledger-ranking-expense">{MONEY_FORMAT.format(item.expense)}</span>
              </div>
            ))}
          </div>
        )}
      </Card>
    </Space>
  );
}

export default function LedgerStatsPage() {
  const [loading, setLoading] = useState(true);
  const [dimension, setDimension] = useState(getInitialDimension);
  const [selectedYM, setSelectedYM] = useState(getInitialYM);
  const [selectedYear, setSelectedYear] = useState(getInitialYear);
  const [stats, setStats] = useState(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    (async () => {
      try {
        const data = await requestLedgerStats({
          dimension,
          ym: selectedYM,
          year: selectedYear
        });
        if (!active) return;
        setStats(data);
        if (dimension === "month" && data.ym && data.ym !== selectedYM) {
          setSelectedYM(data.ym);
        }
        if (dimension === "year" && data.year && data.year !== selectedYear) {
          setSelectedYear(data.year);
        }
      } catch {
        if (!active) return;
        setStats({
          dimension,
          ym: selectedYM,
          year: selectedYear,
          availableYms: [],
          availableYears: [],
          trendPoints: [],
          expenseRankings: [],
          totalExpense: 0
        });
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [dimension, selectedYM, selectedYear]);

  useEffect(() => {
    const search = new URLSearchParams(window.location.search);
    search.set("view", "ledger-stats");
    search.set("dimension", dimension);
    if (selectedYM) search.set("ledger_ym", selectedYM);
    else search.delete("ledger_ym");
    if (dimension === "year") {
      if (selectedYear) search.set("year", selectedYear);
    } else {
      search.delete("year");
    }
    window.history.replaceState(null, "", `${window.location.pathname}?${search.toString()}`);
  }, [dimension, selectedYM, selectedYear]);

  const monthOptions = useMemo(
    () => (stats?.availableYms || []).map((ym) => ({ label: ym.replace(".", "-"), key: ym })),
    [stats]
  );
  const yearOptions = useMemo(
    () => (stats?.availableYears || []).map((year) => ({ label: `${year}年`, key: year })),
    [stats]
  );
  const periodTabItems = dimension === "month" ? monthOptions : yearOptions;
  const periodActiveKey =
    dimension === "month"
      ? stats?.ym || selectedYM || periodTabItems[periodTabItems.length - 1]?.key
      : stats?.year || selectedYear || periodTabItems[periodTabItems.length - 1]?.key;

  const chartData = useMemo(
    () =>
      (stats?.trendPoints || []).map((point) => ({
        label: formatPointLabel(point.key, dimension),
        value: point.expense,
        rawKey: point.key
      })),
    [stats, dimension]
  );

  const chartConfig = useMemo(
    () => ({
      data: chartData,
      xField: "label",
      yField: "value",
      smooth: false,
      animation: false,
      height: 280,
      line: { style: { stroke: "#1677ff", lineWidth: chartData.length > 80 ? 1.4 : 2 } },
      point: {
        shapeField: "circle",
        sizeField: chartData.length > 120 ? 1.4 : 2.4,
        style: { fill: "#fff", stroke: "#1677ff", lineWidth: 1 }
      },
      axis: {
        x: {
          tick: false,
          title: false,
          labelAutoHide: true,
          labelAutoRotate: false
        },
        y: {
          grid: true,
          labelFormatter: (v) => MONEY_FORMAT.format(Number(v))
        }
      },
      tooltip: {
        title: "rawKey",
        items: [
          (d) => ({
            name: "支出",
            value: MONEY_FORMAT.format(Number(d.value || 0)),
            color: "#1677ff"
          })
        ]
      }
    }),
    [chartData]
  );
  const statsContent = renderStatsState(loading, chartData, chartConfig, stats);

  return (
    <div className="page-wrap">
      <Card className="main-card" styles={{ body: { padding: 24 } }}>
        <div className="top-row">
          <Typography.Title level={2} className="page-title">
            账单统计
          </Typography.Title>
          <Button onClick={() => toBillList(stats?.ym || selectedYM || "")}>返回账单清单</Button>
        </div>

        <div className="ledger-stats-filter-row">
          <Tabs
            className="ledger-dimension-tabs"
            activeKey={dimension}
            items={DIMENSION_TAB_ITEMS}
            onChange={(key) => setDimension(`${key}`)}
            animated={false}
          />
          {periodTabItems.length > 0 ? (
            <Tabs
              className="ledger-period-tabs"
              activeKey={periodActiveKey}
              items={periodTabItems}
              onChange={(key) => {
                if (dimension === "month") {
                  setSelectedYM(`${key}`);
                  return;
                }
                setSelectedYear(`${key}`);
              }}
              animated={false}
              tabBarGutter={8}
            />
          ) : (
            <Typography.Text type="secondary">暂无可选范围</Typography.Text>
          )}
        </div>

        {statsContent}
      </Card>
    </div>
  );
}
