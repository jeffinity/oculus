import { Area } from "@ant-design/charts";
import { Card, Space, Typography } from "antd";
import { useMemo } from "react";

const NUMBER_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 0,
  maximumFractionDigits: 2
});

function escapeHtml(text) {
  return `${text}`
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function buildChartData(items, key) {
  return items.map((it, idx) => ({
    idx,
    ym: it.ym,
    value: it[key],
    remark: it.remark || ""
  }));
}

function pickPointSize(length) {
  if (length > 180) {
    return 1.4;
  }
  if (length > 80) {
    return 2;
  }
  return 2.6;
}

function pickLineWidth(length) {
  if (length > 180) {
    return 1;
  }
  if (length > 80) {
    return 1.4;
  }
  return 2;
}

function buildTooltipHtml(title, color, options) {
  const titleText = options?.title || "";
  const first = options?.items?.[0] || {};
  const value = NUMBER_FORMAT.format(Number(first.value || 0));
  const remarkText = `${first.remark || ""}`.trim();
  const remarkLine = remarkText !== "" ? `备注: ${escapeHtml(remarkText)}` : "&nbsp;";

  return `
    <div style="padding:2px 0 0 0;min-width:220px;">
      <div style="font-size:12px;color:rgba(0,0,0,0.45);margin-bottom:8px;">${escapeHtml(titleText)}</div>
      <div style="display:flex;align-items:center;gap:8px;color:rgba(0,0,0,0.9);line-height:1.4;">
        <span style="width:10px;height:10px;border-radius:50%;background:${color};display:inline-block;"></span>
        <span>${escapeHtml(`${title}: ${value}`)}</span>
      </div>
      <div style="margin-top:8px;min-height:20px;color:rgba(0,0,0,0.65);line-height:1.5;word-break:break-all;">
        ${remarkLine}
      </div>
    </div>
  `;
}

export default function TrendChartCard({ title, color, items, metricKey }) {
  const latest = items[items.length - 1];
  const data = useMemo(() => buildChartData(items, metricKey), [items, metricKey]);
  const pointSize = pickPointSize(data.length);
  const lineWidth = pickLineWidth(data.length);

  const config = useMemo(
    () => ({
      data,
      xField: "ym",
      yField: "value",
      smooth: false,
      animation: false,
      height: 260,
      style: {
        fill: `l(270) 0:rgba(255,255,255,0.94) 1:${color}16`
      },
      line: {
        style: {
          stroke: color,
          lineWidth
        }
      },
      point: {
        shapeField: "circle",
        sizeField: pointSize,
        style: {
          fill: "#fff",
          stroke: color,
          lineWidth: 1
        }
      },
      axis: {
        x: {
          tick: false,
          title: false,
          labelAutoRotate: false,
          labelAutoHide: true
        },
        y: {
          grid: true,
          labelFormatter: (v) => NUMBER_FORMAT.format(Number(v))
        }
      },
      tooltip: {
        title: "ym",
        items: [
          (d) => ({
            name: title,
            value: d.value,
            color,
            remark: d.remark || ""
          })
        ]
      },
      interaction: {
        tooltip: {
          render: (_event, options) => buildTooltipHtml(title, color, options)
        }
      }
    }),
    [color, data, lineWidth, pointSize, title]
  );

  return (
    <Card className="trend-card" styles={{ body: { padding: 18 } }}>
      <Space direction="vertical" size={6} className="card-header">
        <Typography.Text className="metric-title">{title}</Typography.Text>
        <Typography.Text className="metric-value">
          {latest ? NUMBER_FORMAT.format(latest[metricKey]) : "--"}
        </Typography.Text>
      </Space>
      <div className="chart-touch-layer">
        <Area {...config} />
      </div>
    </Card>
  );
}
