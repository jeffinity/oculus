/* eslint-disable max-lines-per-function, complexity, max-statements, max-lines */
import { useEffect, useMemo, useRef, useState } from "react";

const LAST_LIST_ROUTE_KEY = "mh:last-list-route";
const SCROLL_CACHE_KEY = "mh:scroll-cache";
const PENDING_LIST_RESTORE_KEY = "mh:pending-list-restore";

function getSearch() {
  return window.location.search || "";
}

function buildURL(query) {
  return query ? `/?${query}` : "/";
}

function navigate(query, options = {}) {
  const { replace = false } = options;
  const url = buildURL(query);
  const method = replace ? "replaceState" : "pushState";
  window.history[method]({}, "", url);
  window.dispatchEvent(new Event("popstate"));
}

function readScrollCache() {
  try {
    return JSON.parse(window.sessionStorage.getItem(SCROLL_CACHE_KEY) || "{}");
  } catch {
    return {};
  }
}

function writeScrollCache(cache) {
  window.sessionStorage.setItem(SCROLL_CACHE_KEY, JSON.stringify(cache));
}

function saveScrollPosition(key) {
  if (!key) {
    return;
  }
  const cache = readScrollCache();
  cache[key] = window.scrollY;
  writeScrollCache(cache);
}

function restoreScrollPosition(key) {
  if (!key) {
    return false;
  }
  const cache = readScrollCache();
  if (typeof cache[key] !== "number") {
    return false;
  }
  window.scrollTo({ top: cache[key], behavior: "auto" });
  return true;
}

function restoreScrollPositionRobust(key) {
  if (!key) {
    return () => {};
  }
  const cache = readScrollCache();
  const target = cache[key];
  if (typeof target !== "number") {
    return () => {};
  }

  let canceled = false;
  let attempts = 0;
  let frameId = 0;
  let timerId = 0;

  const tryRestore = () => {
    if (canceled) {
      return;
    }
    window.scrollTo({ top: target, behavior: "auto" });
    attempts += 1;

    const current = Math.round(window.scrollY);
    const maxScroll = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
    const closeEnough = Math.abs(current - Math.min(target, maxScroll)) <= 2;
    const hasEnoughHeight = maxScroll >= target;

    if ((closeEnough && hasEnoughHeight) || attempts >= 12) {
      return;
    }

    timerId = window.setTimeout(() => {
      frameId = window.requestAnimationFrame(tryRestore);
    }, 120);
  };

  frameId = window.requestAnimationFrame(tryRestore);

  return () => {
    canceled = true;
    if (frameId) {
      window.cancelAnimationFrame(frameId);
    }
    if (timerId) {
      window.clearTimeout(timerId);
    }
  };
}

function rememberListRoute(search) {
  window.sessionStorage.setItem(LAST_LIST_ROUTE_KEY, search || "");
}

function setPendingListRestore(payload) {
  window.sessionStorage.setItem(PENDING_LIST_RESTORE_KEY, JSON.stringify(payload));
}

function readPendingListRestore() {
  try {
    const raw = window.sessionStorage.getItem(PENDING_LIST_RESTORE_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function clearPendingListRestore() {
  window.sessionStorage.removeItem(PENDING_LIST_RESTORE_KEY);
}

function readLastListRoute() {
  return window.sessionStorage.getItem(LAST_LIST_ROUTE_KEY) || "?mode=latest&page=1";
}

function useRoute() {
  const [search, setSearch] = useState(getSearch());

  useEffect(() => {
    const handlePopState = () => {
      setSearch(getSearch());
    };
    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, []);

  return useMemo(() => new URLSearchParams(search), [search]);
}

function useScrollMemory(key, ready) {
  useEffect(() => {
    if (!key) {
      return noop;
    }

    let ticking = false;
    const persist = () => {
      saveScrollPosition(key);
      ticking = false;
    };

    const handleScroll = () => {
      if (ticking) {
        return;
      }
      ticking = true;
      window.requestAnimationFrame(persist);
    };

    const handlePageHide = () => {
      saveScrollPosition(key);
    };

    window.addEventListener("scroll", handleScroll, { passive: true });
    window.addEventListener("pagehide", handlePageHide);

    return () => {
      window.removeEventListener("scroll", handleScroll);
      window.removeEventListener("pagehide", handlePageHide);
    };
  }, [key]);

  useEffect(() => {
    if (!ready) {
      return noop;
    }
    const restored = restoreScrollPosition(key);
    if (!restored) {
      window.scrollTo({ top: 0, behavior: "auto" });
      return noop;
    }
    return restoreScrollPositionRobust(key);
  }, [key, ready]);
}

async function loadJSON(url) {
  const resp = await fetch(url);
  if (!resp.ok) {
    const payload = await resp.json().catch(() => ({}));
    throw new Error(payload.error || `request failed: ${resp.status}`);
  }
  return resp.json();
}

function getOrgID(item) {
  return item.orgId ?? item.org_id ?? 0;
}

function getCoverURL(item) {
  return item.coverUrl || item.cover_url || "";
}

function getBookTitle(item) {
  return item.title || item.cname || "未命名漫画";
}

function getLatestName(item) {
  return item.cname || item.title || "未命名漫画";
}

function getLatestChapter(item) {
  if (item.cname && item.title && item.cname !== item.title) {
    return item.title;
  }
  if (item.title) {
    return `更新至 ${item.title}`;
  }
  return "最新章节已上线";
}

function getBookMeta(item) {
  const desc = item.desc?.trim();
  if (desc) {
    return desc;
  }
  const updateTime = item.updateTime || item.update_time;
  return updateTime ? `更新于 ${updateTime}` : "持续连载中";
}

function getLatestMeta(item) {
  const updateTime = item.updateTime || item.update_time;
  return updateTime ? `${updateTime} 更新` : "最新更新";
}

function renderState(loading, err, empty) {
  if (loading) {
    return <div className="status-panel">加载中...</div>;
  }
  if (err) {
    return <div className="status-panel err">{err}</div>;
  }
  if (empty) {
    return <div className="status-panel">暂无内容</div>;
  }
  return null;
}

function goBack(fallbackQuery) {
  if (window.history.length > 1) {
    window.history.back();
    return;
  }
  navigate(fallbackQuery, { replace: true });
}

function goHome() {
  const route = readLastListRoute();
  navigate(route.startsWith("?") ? route.slice(1) : route.replace(/^\//, "").replace(/^\?/, ""));
}

function noop() {}

function handleActionKeyDown(event, action) {
  if (event.key !== "Enter" && event.key !== " ") {
    return;
  }
  event.preventDefault();
  action();
}

function openBookRoute(routeKey, item) {
  saveScrollPosition(routeKey);
  setPendingListRestore({
    routeKey,
    scrollY: window.scrollY,
  });
  const title = getBookTitle(item);
  navigate(`view=book&org_id=${getOrgID(item)}${title ? `&title=${encodeURIComponent(title)}` : ""}`);
}

function openLatestChapterRoute(routeKey, item) {
  saveScrollPosition(routeKey);
  setPendingListRestore({
    routeKey,
    scrollY: window.scrollY,
  });
  const title = getLatestName(item);
  const chapterTitle = getLatestChapter(item);
  navigate(
    `view=chapter&org_id=${getOrgID(item)}&cid=${item.cid || 0}&index=-1&prefix=${encodeURIComponent(item.prefix || "")}${title ? `&title=${encodeURIComponent(title)}` : ""}${chapterTitle ? `&chapter_title=${encodeURIComponent(chapterTitle)}` : ""}`,
  );
}

function ListPage({ route }) {
  const mode = route.get("mode") || "latest";
  const page = Number(route.get("page") || 1);
  const [data, setData] = useState({ items: [], totalPages: 1 });
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const routeKey = `list:${route.toString() || "mode=latest&page=1"}`;

  useScrollMemory(routeKey, !loading);

  useEffect(() => {
    rememberListRoute(`?${route.toString() || "mode=latest&page=1"}`);
  }, [route]);

  useEffect(() => {
    if (loading) {
      return noop;
    }

    const pending = readPendingListRestore();
    if (!pending || pending.routeKey !== routeKey || typeof pending.scrollY !== "number") {
      return noop;
    }

    let canceled = false;
    let attempts = 0;
    let timerId = 0;
    let frameId = 0;

    const tryRestore = () => {
      if (canceled) {
        return;
      }

      attempts += 1;
      window.scrollTo({ top: pending.scrollY, behavior: "auto" });

      const current = Math.round(window.scrollY);
      const maxScroll = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
      const expected = Math.min(pending.scrollY, maxScroll);
      const closeEnough = Math.abs(current - expected) <= 2;
      const hasEnoughHeight = maxScroll >= pending.scrollY;

      if ((closeEnough && hasEnoughHeight) || attempts >= 18) {
        clearPendingListRestore();
        return;
      }

      timerId = window.setTimeout(() => {
        frameId = window.requestAnimationFrame(tryRestore);
      }, 160);
    };

    frameId = window.requestAnimationFrame(tryRestore);

    return () => {
      canceled = true;
      if (frameId) {
        window.cancelAnimationFrame(frameId);
      }
      if (timerId) {
        window.clearTimeout(timerId);
      }
    };
  }, [loading, routeKey]);

  useEffect(() => {
    let canceled = false;
    setLoading(true);
    setErr("");
    const loadPage = async () => {
      try {
        const url = mode === "latest" ? `/api/v1/mh/latest?page=${page}` : `/api/v1/mh/books?page=${page}`;
        const ret = await loadJSON(url);
        if (!canceled) {
          setData(ret);
        }
      } catch (error) {
        if (!canceled) {
          setErr(String(error.message || error));
        }
      } finally {
        if (!canceled) {
          setLoading(false);
        }
      }
    };
    void loadPage();
    return () => {
      canceled = true;
    };
  }, [mode, page]);

  const items = data.items || [];
  const totalPages = data.totalPages || data.total_pages || 1;
  const stateNode = renderState(loading, err, !items.length);

  return (
    <main className="app-shell">
      <section className="catalog-shell">
        <header className="catalog-header">
          <div className="mode-tabs" role="tablist" aria-label="漫画列表切换">
            <button
              type="button"
              role="tab"
              aria-selected={mode === "latest"}
              className={mode === "latest" ? "active" : ""}
              onClick={() => navigate("mode=latest&page=1")}
            >
              最新漫画
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={mode === "books"}
              className={mode === "books" ? "active" : ""}
              onClick={() => navigate("mode=books&page=1")}
            >
              全部漫画
            </button>
          </div>
        </header>

        {stateNode}

        {!loading && !err && mode === "books" ? (
          <section className="book-grid" aria-label="全部漫画">
            {items.map((item) => (
              <button
                key={`${getOrgID(item)}-${item.cid || 0}`}
                type="button"
                className="book-card"
                onClick={() => openBookRoute(routeKey, item)}
              >
                <div className="cover-frame">
                  <img src={getCoverURL(item)} alt={getBookTitle(item)} />
                </div>
                <div className="book-copy">
                  <h3>{getBookTitle(item)}</h3>
                  <p>{getBookMeta(item)}</p>
                </div>
              </button>
            ))}
          </section>
        ) : null}

        {!loading && !err && mode === "latest" ? (
          <section className="latest-stack" aria-label="最新漫画">
            {items.map((item) => (
              <div
                key={`${getOrgID(item)}-${item.cid || 0}`}
                className="latest-card"
                role="button"
                tabIndex={0}
                onClick={() => openBookRoute(routeKey, item)}
                onKeyDown={(event) => handleActionKeyDown(event, () => openBookRoute(routeKey, item))}
              >
                <div className="latest-cover-wrap">
                  <span className="badge-spot">最新</span>
                  <img src={getCoverURL(item)} alt={getLatestName(item)} className="latest-cover" />
                </div>
                <div className="latest-copy">
                  <h3>{getLatestName(item)}</h3>
                  <div className="latest-subline">
                    <strong>{getLatestMeta(item)}</strong>
                    <span>{getLatestChapter(item)}</span>
                  </div>
                  <p>{`本次更新：${getLatestChapter(item)}`}</p>
                  <button
                    type="button"
                    className="cta-button arco-btn arco-btn-primary arco-btn-size-default arco-btn-shape-square"
                    onClick={(event) => {
                      event.stopPropagation();
                      openLatestChapterRoute(routeKey, item);
                    }}
                    onKeyDown={(event) => event.stopPropagation()}
                  >
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="12"
                      height="12"
                      fill="none"
                      viewBox="0 0 12 12"
                      className="cta-icon"
                      aria-hidden="true"
                    >
                      <g stroke="#fff" strokeWidth="1.294" clipPath="url(#mh-cta-icon)">
                        <path strokeLinejoin="round" d="M1.5 3.75h9L10 10.5H2z" clipRule="evenodd" />
                        <path strokeLinecap="round" strokeLinejoin="round" d="M4 4.75V1.5h4v3.25" />
                        <path strokeLinecap="round" d="M4 8.5h4" />
                      </g>
                      <defs>
                        <clipPath id="mh-cta-icon">
                          <path fill="#fff" d="M0 0h12v12H0z" />
                        </clipPath>
                      </defs>
                    </svg>
                    <span>追漫</span>
                  </button>
                </div>
              </div>
            ))}
          </section>
        ) : null}

        {!loading && !err && items.length ? (
          <footer className="pager">
            <button type="button" disabled={page <= 1} onClick={() => navigate(`mode=${mode}&page=${page - 1}`)}>
              上一页
            </button>
            <span>{page} / {totalPages}</span>
            <button
              type="button"
              disabled={page >= totalPages}
              onClick={() => navigate(`mode=${mode}&page=${page + 1}`)}
            >
              下一页
            </button>
          </footer>
        ) : null}
      </section>
    </main>
  );
}

function BookPage({ route }) {
  const orgID = Number(route.get("org_id") || 0);
  const fallbackTitle = route.get("title") || "漫画详情";
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const routeKey = `book:${route.toString()}`;

  useScrollMemory(routeKey, !loading);

  useEffect(() => {
    let canceled = false;
    setLoading(true);
    const loadBook = async () => {
      try {
        const ret = await loadJSON(`/api/v1/mh/book?org_id=${orgID}`);
        if (!canceled) {
          setData(ret);
        }
      } catch (error) {
        if (!canceled) {
          setErr(String(error.message || error));
        }
      } finally {
        if (!canceled) {
          setLoading(false);
        }
      }
    };
    void loadBook();
    return () => {
      canceled = true;
    };
  }, [orgID]);

  const openChapter = (idx, cid, prefix, chapterName) => {
    const title = data?.book?.title || fallbackTitle;
    navigate(
      `view=chapter&org_id=${orgID}&index=${idx}&cid=${cid}&prefix=${encodeURIComponent(prefix)}&title=${encodeURIComponent(title)}&chapter_title=${encodeURIComponent(chapterName)}`,
    );
  };

  return (
    <main className="app-shell">
      <section className="detail-shell detail-shell-book">
        {loading ? <div className="status-panel">加载中...</div> : null}
        {err ? <div className="status-panel err">{err}</div> : null}
        {data?.book ? (
          <section className="book-detail-content">
            <div className="book-hero">
              <img src={getCoverURL(data.book)} alt={data.book.title} className="book-hero-image" />
              <button type="button" className="hero-back-button" onClick={() => goBack(readLastListRoute().replace(/^\?/, ""))} aria-label="返回列表">
                <span className="hero-back-icon" aria-hidden="true" />
              </button>
              <button type="button" className="hero-home-button" onClick={goHome} aria-label="返回首页">
                <span className="home-icon" aria-hidden="true" />
              </button>
            </div>
            <div className="book-title-block">
              <h1>{data.book.title || fallbackTitle}</h1>
            </div>
            <div className="book-info-panel">
              <p className="book-desc">{data.book.desc || "暂无简介"}</p>
              <p className="book-update-time">
                最后更新时间：{data.book.updateTime || data.book.update_time || "暂无更新信息"}
              </p>
            </div>
            <div className="chapter-list">
              {data.chapters?.map((c, idx) => (
                <button
                  key={c.cid}
                  type="button"
                  className={c.cid === (data.lastCid ?? data.last_cid) ? "active" : ""}
                  onClick={() => openChapter(idx, c.cid, c.prefix, c.name)}
                >
                  {c.name}
                </button>
              ))}
            </div>
          </section>
        ) : null}
      </section>
    </main>
  );
}

function ChapterPage({ route }) {
  const orgID = Number(route.get("org_id") || 0);
  const cid = Number(route.get("cid") || 0);
  const index = Number(route.get("index") || -1);
  const prefix = route.get("prefix") || "";
  const fallbackTitle = route.get("title") || "漫画";
  const fallbackChapterTitle = route.get("chapter_title") || "章节";

  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [chromeVisible, setChromeVisible] = useState(true);
  const toggleLockRef = useRef(false);
  const routeKey = `chapter:${route.toString()}`;

  useScrollMemory(routeKey, !loading);

  useEffect(() => {
    let canceled = false;
    setLoading(true);
    const loadChapter = async () => {
      try {
        const ret = await loadJSON(`/api/v1/mh/chapter?org_id=${orgID}&cid=${cid}&index=${index}&prefix=${encodeURIComponent(prefix)}`);
        if (!canceled) {
          setData(ret);
        }
      } catch (error) {
        if (!canceled) {
          setErr(String(error.message || error));
        }
      } finally {
        if (!canceled) {
          setLoading(false);
        }
      }
    };
    void loadChapter();
    return () => {
      canceled = true;
    };
  }, [orgID, cid, index, prefix]);

  const chapterName = data?.chapterName || data?.chapter_name || fallbackChapterTitle;
  const titleText = `${chapterName} ${fallbackTitle}`.trim();
  const prevCid = Number(data?.prevCid ?? data?.prev_cid ?? 0);
  const nextCid = Number(data?.nextCid ?? data?.next_cid ?? 0);
  const currentIndex = Number(data?.index ?? index);
  const totalPages = Number(data?.totalPages ?? data?.total_pages ?? 0);
  const hasPrev = Number.isFinite(prevCid) && prevCid > 0;
  const hasNext = Number.isFinite(nextCid) && nextCid > 0;

  const openSiblingChapter = (targetCid, nextIndex) => {
    if (!Number.isFinite(targetCid) || targetCid <= 0) {
      return;
    }
    const title = route.get("title") || fallbackTitle;
    navigate(
      `view=chapter&org_id=${orgID}&cid=${targetCid}&index=${nextIndex}&title=${encodeURIComponent(title)}&chapter_title=${encodeURIComponent(chapterName)}`,
    );
  };

  const handleReaderTap = () => {
    if (toggleLockRef.current) {
      return;
    }
    setChromeVisible((visible) => !visible);
  };

  const stopToggle = (event) => {
    toggleLockRef.current = true;
    event.stopPropagation();
    window.setTimeout(() => {
      toggleLockRef.current = false;
    }, 0);
  };

  return (
    <main className="reader-shell">
      {loading ? <div className="reader-state">加载中...</div> : null}
      {err ? <div className="reader-state err">{err}</div> : null}
      {data ? (
        <>
          <header className={`reader-topbar ${chromeVisible ? "visible" : "hidden"}`} onPointerDown={stopToggle}>
            <button type="button" className="reader-icon-button" onClick={() => goBack(`view=book&org_id=${orgID}&title=${encodeURIComponent(fallbackTitle)}`)}>
              <span className="hero-back-icon" aria-hidden="true" />
            </button>
            <h1>{titleText}</h1>
            <button type="button" className="reader-icon-button home" onClick={goHome} aria-label="返回首页">
              <span className="home-icon" aria-hidden="true" />
            </button>
          </header>

          <button
            type="button"
            className="reader-images reader-images-button"
            onClick={handleReaderTap}
            aria-label="切换阅读工具栏显示"
          >
            {data.images?.map((img) => (
              <img key={img} src={img} alt={chapterName} />
            ))}
          </button>

          <footer className={`reader-bottombar ${chromeVisible ? "visible" : "hidden"}`} onPointerDown={stopToggle}>
            <button
              type="button"
              className="reader-nav-button"
              disabled={!hasPrev}
              onClick={() => openSiblingChapter(prevCid, currentIndex - 1)}
            >
              上一页
            </button>
            <button
              type="button"
              className="reader-page-indicator"
              onClick={() => navigate(`view=book&org_id=${orgID}&title=${encodeURIComponent(fallbackTitle)}`)}
              aria-label="返回漫画目录"
            >
              {data.page} / {totalPages}
            </button>
            <button
              type="button"
              className="reader-nav-button"
              disabled={!hasNext}
              onClick={() => openSiblingChapter(nextCid, currentIndex + 1)}
            >
              下一页
            </button>
          </footer>
        </>
      ) : null}
    </main>
  );
}

export default function App() {
  const route = useRoute();
  const view = route.get("view") || "list";

  if (view === "book") {
    return <BookPage route={route} />;
  }
  if (view === "chapter") {
    return <ChapterPage route={route} />;
  }
  return <ListPage route={route} />;
}
