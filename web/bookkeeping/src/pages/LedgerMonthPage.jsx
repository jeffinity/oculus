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
import { Button, Card, Empty, Spin, Typography, message } from "antd";
import dayjs from "dayjs";
import { useEffect, useMemo, useRef, useState } from "react";

import MobileHeader from "../components/MobileHeader";

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
const SUPPORTED_CATEGORIES = new Set(Object.keys(CATEGORY_ICON_MAP));
const CATEGORY_ALIAS_MAP = {
  美食: "餐饮",
  零嘴: "零食",
  零食饮料: "零食",
  打车: "交通",
  出行: "交通",
  学费: "学习",
  书本: "书籍",
  宠物用品: "宠物",
  红包: "礼金",
  人情: "礼金",
  礼品: "礼物",
  快递费: "快递"
};

const CSV_CONCURRENCY = 5;

function parseAmount(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function normalizeEntryDate(raw) {
  const base = `${raw || ""}`.trim();
  if (!base) return "";
  const normalized = base
    .replaceAll("年", "-")
    .replaceAll("月", "-")
    .replaceAll("日", "")
    .replaceAll(".", "-")
    .replaceAll("/", "-");
  const d = dayjs(normalized);
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
  let sign = "";
  if (amount > 0) {
    sign = "+";
  } else if (amount < 0) {
    sign = "-";
  }
  return `${sign}${MONEY_FORMAT.format(Math.abs(amount))}`;
}

function getAmountClassName(amount) {
  if (amount > 0) {
    return "ledger-item-amount up";
  }
  if (amount < 0) {
    return "ledger-item-amount down";
  }
  return "ledger-item-amount";
}

function renderLedgerState(loading, groups) {
  if (loading) {
    return (
      <div className="state-wrap">
        <Spin size="large" />
      </div>
    );
  }
  if (groups.length === 0) {
    return (
      <div className="state-wrap">
        <Empty description="该月份暂无账单数据" />
      </div>
    );
  }
  return null;
}

async function requestLedgerList(ym) {
  const query = new URLSearchParams({ ym });
  const res = await fetch(`/api/v1/bookkeeping/ledgers?${query.toString()}`);
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  const json = await res.json();
  return (json.items || []).map(normalizeItem);
}

async function requestUpsertLedgerEntry(payload) {
  const res = await fetch("/api/v1/bookkeeping/ledgers", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error(`request failed: ${res.status}`);
  return res.json();
}

function splitCsvLine(line) {
  const result = [];
  let current = "";
  let inQuote = false;
  for (let i = 0; i < line.length; i++) {
    const char = line[i];
    if (char === "\"") {
      if (inQuote && line[i + 1] === "\"") {
        current += "\"";
        i++;
        continue;
      }
      inQuote = !inQuote;
      continue;
    }
    if (char === "," && !inQuote) {
      result.push(current.trim());
      current = "";
      continue;
    }
    current += char;
  }
  result.push(current.trim());
  return result;
}

function countChineseChars(text) {
  const matches = `${text || ""}`.match(/[\u4e00-\u9fa5]/g);
  return matches ? matches.length : 0;
}

function decodeCsvBuffer(buffer) {
  const utf8 = new TextDecoder("utf-8", { fatal: false }).decode(buffer);
  const utf8Score = countChineseChars(utf8) - (utf8.match(/�/g) || []).length * 10;
  let gbk = "";
  let gbkScore = Number.NEGATIVE_INFINITY;
  try {
    gbk = new TextDecoder("gb18030", { fatal: false }).decode(buffer);
    gbkScore = countChineseChars(gbk) - (gbk.match(/�/g) || []).length * 10;
  } catch {
    gbkScore = Number.NEGATIVE_INFINITY;
  }
  return gbkScore > utf8Score ? gbk : utf8;
}

function normalizeCategory(raw) {
  const name = `${raw || ""}`.trim();
  if (!name) return "";
  if (SUPPORTED_CATEGORIES.has(name)) return name;
  const alias = CATEGORY_ALIAS_MAP[name];
  if (alias && SUPPORTED_CATEGORIES.has(alias)) return alias;
  return "";
}

function parseLedgerRowsFromCsvText(text) {
  const lines = `${text || ""}`
    .replace(/^\uFEFF/, "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  if (lines.length === 0) return [];

  const rows = [];
  for (const line of lines) {
    const cols = splitCsvLine(line).map((x) => x.replace(/^"|"$/g, "").trim());
    if (cols.length < 5) continue;
    const entryDate = normalizeEntryDate(cols[0]);
    if (!/^\d{4}-\d{2}-\d{2}$/.test(entryDate)) continue;
    const incomeExpense = `${cols[1] || ""}`.trim();
    const category = normalizeCategory(cols[2]);
    const amountRaw = `${cols[4] || ""}`.replaceAll(",", "").trim();
    const amountAbs = Number(amountRaw);
    if (!category || !Number.isFinite(amountAbs) || amountAbs <= 0) continue;
    const isIncome = incomeExpense === "收入";
    const amount = isIncome ? amountAbs : -amountAbs;
    rows.push({
      entry_date: entryDate,
      category,
      amount: amount.toFixed(2),
      remark: `${cols[5] || ""}`.trim()
    });
  }
  return rows;
}

async function importLedgerRows(rows) {
  let imported = 0;
  let failed = 0;
  let index = 0;
  const workers = Array.from({ length: Math.min(CSV_CONCURRENCY, rows.length) }, async () => {
    while (index < rows.length) {
      const currentIndex = index;
      index++;
      try {
        await requestUpsertLedgerEntry(rows[currentIndex]);
        imported++;
      } catch {
        failed++;
      }
    }
  });
  await Promise.all(workers);
  return { imported, failed };
}

export default function LedgerMonthPage() {
  const search = new URLSearchParams(window.location.search);
  const ym = `${search.get("ledger_ym") || ""}`.trim();
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState([]);
  const [importing, setImporting] = useState(false);
  const fileInputRef = useRef(null);

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
      } catch {
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
  const pageState = renderLedgerState(loading, groups);

  async function refreshCurrentMonth() {
    if (!/^\d{4}\.\d{2}$/.test(ym)) return;
    try {
      const list = await requestLedgerList(ym);
      setItems(list);
    } catch {
      // ignore
    }
  }

  async function onCsvUploadChange(event) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    if (importing) return;

    setImporting(true);
    try {
      const buffer = await file.arrayBuffer();
      const text = decodeCsvBuffer(buffer);
      const rows = parseLedgerRowsFromCsvText(text);
      if (rows.length === 0) {
        message.warning("未识别到可导入的明细，请检查 CSV 格式");
        return;
      }
      const { imported, failed } = await importLedgerRows(rows);
      await refreshCurrentMonth();
      if (failed > 0) {
        message.warning(`导入完成：成功 ${imported} 条，失败 ${failed} 条`);
      } else {
        message.success(`导入完成：成功 ${imported} 条`);
      }
    } catch {
      message.error("导入失败，请检查文件格式后重试");
    } finally {
      setImporting(false);
    }
  }

  return (
    <div className="mobile-page-wrap">
      <MobileHeader title="消费明细" onBack={toBack} right={<Button onClick={() => openStats(ym)}>账单统计</Button>} />
      <div className="mobile-page-content">
        <div className="page-wrap">
      <Card className="main-card" styles={{ body: { padding: 24 } }}>
        <div className="ledger-month-head">
          <Typography.Text className="ledger-month-subtitle">{ym || "--"}</Typography.Text>
          <Button
            className="change-action-btn"
            loading={importing}
            disabled={loading}
            onClick={() => fileInputRef.current?.click()}
          >
            上传明细
          </Button>
          <input
            ref={fileInputRef}
            type="file"
            accept=".csv,text/csv"
            style={{ display: "none" }}
            onChange={onCsvUploadChange}
          />
        </div>

        {pageState || (
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
                    {group.items.map((item) => {
                      const Icon = CATEGORY_ICON_MAP[item.category] || CoffeeOutlined;
                      const label = item.remark || item.category || "未分类";
                      return (
                        <div key={item.key} className="ledger-item-row">
                          <span className="ledger-item-icon-wrap">
                            <Icon className="ledger-item-icon" />
                          </span>
                          <span className="ledger-item-label" title={label}>
                            {label}
                          </span>
                          <span className={getAmountClassName(item.amount)}>
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
      </div>
    </div>
  );
}
