import { Empty, Spin } from "antd";
import { Suspense, lazy, useMemo } from "react";

const ROUTES = {
  "/": lazy(() => import("./pages/HomePage")),
  "/index.html": lazy(() => import("./pages/HomePage")),
  "loan-detail": lazy(() => import("./pages/LoanDetailPage")),
  "asset-manage": lazy(() => import("./pages/AssetManagePage")),
  "asset-category-select": lazy(() => import("./pages/AssetCategorySelectPage")),
  "asset-preset-select": lazy(() => import("./pages/AssetPresetSelectPage")),
  "asset-detail-form": lazy(() => import("./pages/AssetDetailFormPage")),
  "ledger-month": lazy(() => import("./pages/LedgerMonthPage")),
  "ledger-stats": lazy(() => import("./pages/LedgerStatsPage"))
};

function PageFallback() {
  return (
    <div className="state-wrap app-loading">
      <Spin size="large" />
    </div>
  );
}

export default function App() {
  const {pathname} = window.location;
  const search = new URLSearchParams(window.location.search);
  const view = search.get("view");
  const CurrentPage = useMemo(() => {
    if (pathname === "/" || pathname === "/index.html") {
      if (view === "loan-detail") return ROUTES["loan-detail"];
      if (view === "asset-manage") return ROUTES["asset-manage"];
      if (view === "asset-category-select") return ROUTES["asset-category-select"];
      if (view === "asset-preset-select") return ROUTES["asset-preset-select"];
      if (view === "asset-detail-form") return ROUTES["asset-detail-form"];
      if (view === "ledger-month") return ROUTES["ledger-month"];
      if (view === "ledger-stats") return ROUTES["ledger-stats"];
    }
    return ROUTES[pathname] || ROUTES["/"];
  }, [pathname, view]);

  if (!CurrentPage) {
    return (
      <div className="state-wrap app-loading">
        <Empty description="页面不存在" />
      </div>
    );
  }

  return (
    <Suspense fallback={<PageFallback />}>
      <CurrentPage />
    </Suspense>
  );
}
