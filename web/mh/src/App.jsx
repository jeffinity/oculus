/* eslint-disable max-lines-per-function, complexity, max-statements, max-lines */
import { useEffect, useMemo, useState } from "react";

const LAST_LIST_ROUTE_KEY = "mh:last-list-route";
const SCROLL_CACHE_KEY = "mh:scroll-cache";
const PENDING_LIST_RESTORE_KEY = "mh:pending-list-restore";
const READER_CONTROLS_SIDE_KEY = "mh.reader.controls.side";

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

async function postJSON(url, payload) {
  const resp = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}));
    throw new Error(body.error || `request failed: ${resp.status}`);
  }
  return resp.json();
}

function getOrgID(item) {
  return item.orgId ?? item.org_id ?? 0;
}

function getCoverURL(item) {
  return item.coverUrl || item.cover_url || "";
}

function isStarred(item) {
  return Boolean(item.starred);
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

function buildListQuery(mode, page, starredOnly) {
  const query = [`mode=${mode}`, `page=${page}`];
  if (starredOnly) {
    query.push("starred=1");
  }
  return query.join("&");
}

function ListPage({ route }) {
  const mode = route.get("mode") || "latest";
  const page = Number(route.get("page") || 1);
  const starredOnly = route.get("starred") === "1";
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
        const baseURL = mode === "latest" ? "/api/v1/mh/latest" : "/api/v1/mh/books";
        const url = `${baseURL}?page=${page}&starred_only=${starredOnly ? "true" : "false"}`;
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
  }, [mode, page, starredOnly]);

  const handleToggleStarredFilter = () => {
    navigate(buildListQuery(mode, 1, !starredOnly));
  };

  const handleSetStar = async (event, item) => {
    event.stopPropagation();
    const orgID = getOrgID(item);
    if (!orgID) {
      return;
    }
    const previous = isStarred(item);
    const target = !previous;

    setData((current) => ({
      ...current,
      items: (current.items || []).map((entry) => (getOrgID(entry) === orgID ? { ...entry, starred: target } : entry)),
    }));

    try {
      await postJSON("/api/v1/mh/star", {
        org_id: orgID,
        starred: target,
      });
      if (starredOnly && !target) {
        const baseURL = mode === "latest" ? "/api/v1/mh/latest" : "/api/v1/mh/books";
        const ret = await loadJSON(`${baseURL}?page=${page}&starred_only=true`);
        setData(ret);
      }
    } catch (error) {
      setData((current) => ({
        ...current,
        items: (current.items || []).map((entry) => (getOrgID(entry) === orgID ? { ...entry, starred: previous } : entry)),
      }));
      setErr(String(error.message || error));
    }
  };

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
              onClick={() => navigate(buildListQuery("latest", 1, starredOnly))}
            >
              最新漫画
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={mode === "books"}
              className={mode === "books" ? "active" : ""}
              onClick={() => navigate(buildListQuery("books", 1, starredOnly))}
            >
              全部漫画
            </button>
            <button
              type="button"
              className={`tab-star-filter ${starredOnly ? "active" : ""}`}
              onClick={handleToggleStarredFilter}
              aria-label={starredOnly ? "显示全部漫画" : "仅显示已星标漫画"}
            >
              {starredOnly ? "★" : "☆"}
            </button>
          </div>
        </header>

        {stateNode}

        {!loading && !err && mode === "books" ? (
          <section className="book-grid" aria-label="全部漫画">
            {items.map((item) => (
              <div
                key={`${getOrgID(item)}-${item.cid || 0}`}
                className="book-card"
                role="button"
                tabIndex={0}
                onClick={() => openBookRoute(routeKey, item)}
                onKeyDown={(event) => handleActionKeyDown(event, () => openBookRoute(routeKey, item))}
              >
                <button
                  type="button"
                  className={`item-star ${isStarred(item) ? "active" : ""}`}
                  aria-label={isStarred(item) ? "取消星标" : "添加星标"}
                  onClick={(event) => void handleSetStar(event, item)}
                  onKeyDown={(event) => event.stopPropagation()}
                >
                  {isStarred(item) ? "★" : "☆"}
                </button>
                <div className="cover-frame">
                  <img src={getCoverURL(item)} alt={getBookTitle(item)} />
                </div>
                <div className="book-copy">
                  <h3>{getBookTitle(item)}</h3>
                  <p>{getBookMeta(item)}</p>
                </div>
              </div>
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
                  <button
                    type="button"
                    className={`item-star latest-star ${isStarred(item) ? "active" : ""}`}
                    aria-label={isStarred(item) ? "取消星标" : "添加星标"}
                    onClick={(event) => void handleSetStar(event, item)}
                    onKeyDown={(event) => event.stopPropagation()}
                  >
                    {isStarred(item) ? "★" : "☆"}
                  </button>
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
            <button type="button" disabled={page <= 1} onClick={() => navigate(buildListQuery(mode, page - 1, starredOnly))}>
              上一页
            </button>
            <span>{page} / {totalPages}</span>
            <button
              type="button"
              disabled={page >= totalPages}
              onClick={() => navigate(buildListQuery(mode, page + 1, starredOnly))}
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
  const [readerControlsSide, setReaderControlsSide] = useState(() => {
    const side = window.localStorage.getItem(READER_CONTROLS_SIDE_KEY);
    return side === "right" ? "right" : "left";
  });
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

  const toggleReaderControlsSide = () => {
    setReaderControlsSide((current) => {
      const next = current === "left" ? "right" : "left";
      window.localStorage.setItem(READER_CONTROLS_SIDE_KEY, next);
      return next;
    });
  };

  return (
    <main className="reader-shell">
      {loading ? <div className="reader-state">加载中...</div> : null}
      {err ? <div className="reader-state err">{err}</div> : null}
      {data ? (
        <>
          <header className="reader-topbar">
            <button type="button" className="reader-icon-button" onClick={() => goBack(`view=book&org_id=${orgID}&title=${encodeURIComponent(fallbackTitle)}`)}>
              <span className="hero-back-icon" aria-hidden="true" />
            </button>
            <button type="button" className="reader-icon-button home" onClick={goHome} aria-label="返回首页">
              <span className="home-icon" aria-hidden="true" />
            </button>
          </header>

          <div className="reader-images">
            {data.images?.map((img) => (
              <img key={img} src={img} alt={chapterName} />
            ))}
          </div>

          <footer className={`reader-bottombar ${readerControlsSide === "right" ? "side-right" : "side-left"}`}>
            <button type="button" className="reader-nav-button reader-side-toggle" onClick={toggleReaderControlsSide} aria-label="切换按钮停靠位置">
              {"<->"}
            </button>
            <button
              type="button"
              className="reader-nav-button"
              disabled={!hasPrev}
              onClick={() => openSiblingChapter(prevCid, currentIndex - 1)}
            >
              {"<"}
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
              {">"}
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
