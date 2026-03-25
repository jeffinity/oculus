import { Suspense, lazy } from "react";

const ReconcilePage = lazy(() => import("./pages/ReconcilePage"));
const WorkspacePage = lazy(() => import("./pages/WorkspacePage"));

export default function App() {
  const query = new URLSearchParams(window.location.search);
  const view = query.get("view");
  const Page = view === "reconcile" ? ReconcilePage : WorkspacePage;

  return (
    <Suspense fallback={<div className="app-loading">页面加载中...</div>}>
      <Page />
    </Suspense>
  );
}
