import dayjs from "dayjs";

export function getSearch() {
  return new URLSearchParams(window.location.search);
}

export function getCurrentYM() {
  const search = getSearch();
  const raw = `${search.get("ym") || ""}`.trim();
  if (/^\d{4}\.\d{2}$/.test(raw)) return raw;
  return dayjs().format("YYYY.MM");
}

export function shiftYM(ym, diff) {
  const parsed = dayjs(`${ym}.01`, "YYYY.MM.DD");
  if (!parsed.isValid()) return ym;
  return parsed.add(diff, "month").format("YYYY.MM");
}

export function navigateToView(view, patch = {}) {
  const search = getSearch();
  search.set("view", view);
  for (const [key, value] of Object.entries(patch)) {
    if (value === undefined || value === null || value === "") {
      search.delete(key);
      continue;
    }
    search.set(key, `${value}`);
  }
  window.location.search = search.toString();
}

export function backToAssetManage(ym) {
  navigateToView("asset-manage", { ym, detail_id: "", sub_type: "", asset_type: "", preset_code: "", mode: "" });
}

export function backToCategorySelect(ym) {
  navigateToView("asset-category-select", {
    ym,
    detail_id: "",
    sub_type: "",
    asset_type: "",
    preset_code: "",
    mode: ""
  });
}

export function backToHome() {
  const search = getSearch();
  for (const key of ["view", "ym", "detail_id", "sub_type", "asset_type", "preset_code", "mode"]) search.delete(key);
  const next = search.toString();
  window.location.href = `${window.location.pathname}${next ? `?${next}` : ""}`;
}
