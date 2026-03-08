import { Suspense, lazy, useMemo } from "react";
import { Empty, Spin } from "antd";

const ROUTES = {
  "/": lazy(() => import("./pages/HomePage")),
  "/index.html": lazy(() => import("./pages/HomePage")),
  "loan-detail": lazy(() => import("./pages/LoanDetailPage")),
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
  const pathname = window.location.pathname;
  const search = new URLSearchParams(window.location.search);
  const view = search.get("view");
  const CurrentPage = useMemo(() => {
    if (pathname === "/" || pathname === "/index.html") {
      if (view === "loan-detail") return ROUTES["loan-detail"];
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
