import { useEffect, useMemo, useState } from "react";
import {
  AppstoreOutlined,
  AppleOutlined,
  BankOutlined,
  BookOutlined,
  BugOutlined,
  CarOutlined,
  CoffeeOutlined,
  CustomerServiceOutlined,
  DribbbleOutlined,
  ForkOutlined,
  GiftOutlined,
  HeartOutlined,
  HomeOutlined,
  InboxOutlined,
  MedicineBoxOutlined,
  MessageOutlined,
  MobileOutlined,
  PhoneOutlined,
  ReadOutlined,
  RocketOutlined,
  SettingOutlined,
  ShoppingOutlined,
  SkinOutlined,
  SmileOutlined,
  TagsOutlined,
  TeamOutlined,
  ToolOutlined,
  WalletOutlined
} from "@ant-design/icons";
import { Button, Card, Empty, Space, Spin, Typography } from "antd";
import dayjs from "dayjs";

const MONEY_FORMAT = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});

const CATEGORY_ICON_MAP = {
  餐饮: ForkOutlined,
  购物: ShoppingOutlined,
  日用: InboxOutlined,
  交通: CarOutlined,
  汽车: CarOutlined,
  蔬菜: BugOutlined,
  水果: AppleOutlined,
  零食: GiftOutlined,
  运动: DribbbleOutlined,
  娱乐: CustomerServiceOutlined,
  通讯: PhoneOutlined,
  服饰: SkinOutlined,
  美容: SmileOutlined,
  住房: HomeOutlined,
  居家: AppstoreOutlined,
  孩子: SmileOutlined,
  长辈: TeamOutlined,
  社交: MessageOutlined,
  旅行: RocketOutlined,
  烟酒: CoffeeOutlined,
  数码: MobileOutlined,
  医疗: MedicineBoxOutlined,
  书籍: BookOutlined,
  学习: ReadOutlined,
  宠物: BugOutlined,
  礼金: WalletOutlined,
  礼物: GiftOutlined,
  办公: BankOutlined,
  维修: ToolOutlined,
  捐赠: HeartOutlined,
  彩票: TagsOutlined,
  亲友: TeamOutlined,
  快递: InboxOutlined,
  设置: SettingOutlined
};

function parseAmount(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function normalizeEntryDate(raw) {
  const base = `${raw || ""}`.trim();
  if (!base) return "";
  const d = dayjs(base.replaceAll(".", "-").replaceAll("/", "-"));
  return d.isValid() ? d.format("YYYY-MM-DD") : base;
}

function normalizeItem(it, idx) {
  return {
    key: `${it.entryDate ?? it.entry_date}-${it.category}-${it.amount}-${idx}`,
    entryDate: normalizeEntryDate(it.entryDate ?? it.entry_date),
    ym: `${it.ym || ""}`,
    amount: parseAmount(it.amount),
    category: `${it.category || ""}`,
    remark: `${it.remark || ""}`.trim()
  };
}

function buildGroups(items) {
  const groups = new Map();
  for (const item of items) {
    const date = item.entryDate;
    if (!date) continue;
    if (!groups.has(date)) {
      groups.set(date, { entryDate: date, total: 0, items: [] });
    }
    const current = groups.get(date);
    current.items.push(item);
    current.total += item.amount;
  }
  return [...groups.values()].sort((a, b) => (a.entryDate < b.entryDate ? 1 : -1));
}

function toBack() {
  const search = new URLSearchParams(window.location.search);
  search.delete("view");
  search.delete("ledger_ym");
  const q = search.toString();
  window.location.href = `${window.location.pathname}${q ? `?${q}` : ""}`;
}

function openStats(ym) {
  const search = new URLSearchParams(window.location.search);
  search.set("view", "ledger-stats");
  if (/^\d{4}\.\d{2}$/.test(ym)) {
    search.set("ledger_ym", ym);
  }
  search.set("dimension", "month");
  window.location.search = search.toString();
}

function formatDayTitle(entryDate) {
  const d = dayjs(entryDate);
  if (!d.isValid()) return entryDate;
  return d.format("MM月DD日 dddd");
}

function renderDaySubtotal(total) {
  if (total < 0) {
    return {
      label: "支出",
      value: MONEY_FORMAT.format(Math.abs(total)),
      className: "ledger-day-total down"
    };
  }
  if (total > 0) {
    return {
      label: "收入",
      value: MONEY_FORMAT.format(total),
      className: "ledger-day-total up"
    };
  }
  return { label: "小计", value: "0.00", className: "ledger-day-total" };
}

function renderAmount(amount) {
  const sign = amount > 0 ? "+" : amount < 0 ? "-" : "";
  return `${sign}${MONEY_FORMAT.format(Math.abs(amount))}`;
}

async function requestLedgerList(ym) {
  const query = new URLSearchParams({ ym });
  const res = await fetch(`/api/v1/bookkeeping/ledgers?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return (json.items || []).map(normalizeItem);
}

export default function LedgerMonthPage() {
  const search = new URLSearchParams(window.location.search);
  const ym = `${search.get("ledger_ym") || ""}`.trim();
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState([]);

  useEffect(() => {
    let active = true;
    if (!/^\d{4}\.\d{2}$/.test(ym)) {
      setItems([]);
      setLoading(false);
      return () => {
        active = false;
      };
    }

    setLoading(true);
    (async () => {
      try {
        const list = await requestLedgerList(ym);
        if (!active) return;
        setItems(list);
      } catch (_) {
        if (!active) return;
        setItems([]);
      } finally {
        if (active) setLoading(false);
      }
    })();

    return () => {
      active = false;
    };
  }, [ym]);

  const groups = useMemo(() => buildGroups(items), [items]);

  return (
    <div className="page-wrap">
      <Card className="main-card" styles={{ body: { padding: 24 } }}>
        <div className="top-row">
          <Typography.Title level={2} className="page-title">
            账单清单
          </Typography.Title>
          <Space>
            <Button onClick={() => openStats(ym)}>账单统计</Button>
            <Button onClick={toBack}>返回资产总览</Button>
          </Space>
        </div>
        <Typography.Text className="ledger-month-subtitle">{ym || "--"}</Typography.Text>

        {loading ? (
          <div className="state-wrap">
            <Spin size="large" />
          </div>
        ) : groups.length === 0 ? (
          <div className="state-wrap">
            <Empty description="该月份暂无账单数据" />
          </div>
        ) : (
          <div className="ledger-group-list">
            {groups.map((group) => {
              const dayTotal = renderDaySubtotal(group.total);
              return (
                <div key={group.entryDate} className="ledger-day-group">
                  <div className="ledger-day-header">
                    <span className="ledger-day-date">{formatDayTitle(group.entryDate)}</span>
                    <span className={dayTotal.className}>{`${dayTotal.label}: ${dayTotal.value}`}</span>
                  </div>
                  <div className="ledger-item-list">
                    {group.items.map((item, idx) => {
                      const Icon = CATEGORY_ICON_MAP[item.category] || CoffeeOutlined;
                      const label = item.remark || item.category || "未分类";
                      return (
                        <div key={`${item.key}-${idx}`} className="ledger-item-row">
                          <span className="ledger-item-icon-wrap">
                            <Icon className="ledger-item-icon" />
                          </span>
                          <span className="ledger-item-label" title={label}>
                            {label}
                          </span>
                          <span className={`ledger-item-amount ${item.amount > 0 ? "up" : item.amount < 0 ? "down" : ""}`}>
                            {renderAmount(item.amount)}
                          </span>
                        </div>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>
    </div>
  );
}
