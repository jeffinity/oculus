package biz

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-kratos/kratos/v2/log"

	"github.com/jeffinity/oculus/app/mh/internal/conf"
)

type ComicRepo interface {
	ListBooks(ctx context.Context, page, size int) ([]Book, int64, error)
	FindBookByOrgID(ctx context.Context, orgID int64) (*Book, error)
	UpsertBook(ctx context.Context, book Book) error
	UpdateBookFields(ctx context.Context, id int64, fields map[string]any) error
	ListMissingBookSeedsFromLatest(ctx context.Context, startOrgID, endOrgID int64, limit int) ([]BookSeed, error)

	ListLatest(ctx context.Context, page, size int) ([]Latest, int64, error)
	InsertLatest(ctx context.Context, latest Latest) error

	UpsertHistory(ctx context.Context, orgID, cid int64) error
	GetHistoryCID(ctx context.Context, orgID int64) (int64, error)

	GetSyncCID(ctx context.Context, stateID string) (int64, error)
	UpsertSyncCID(ctx context.Context, stateID string, cid int64) error

	ListBookOrgIDs(ctx context.Context, page, size int) ([]int64, error)
	GetProofedOrgIDs(ctx context.Context, orgIDs []int64) (map[int64]struct{}, error)
	InsertProofed(ctx context.Context, orgID int64) error

	ListChapterDirs(ctx context.Context, orgID int64) ([]ChapterMeta, error)
	ListChapterImages(ctx context.Context, prefix string) ([]string, error)
	SaveImageFromURL(ctx context.Context, imageURL, objectName string) (map[string]string, error)
}

type Book struct {
	ID        int64
	Title     string
	URL       string
	Desc      string
	OrgID     int64
	Cover     map[string]string
	CreatedAt time.Time
}

type BookSeed struct {
	OrgID     int64
	Title     string
	URL       string
	CreatedAt time.Time
}

type Latest struct {
	Prefix    string
	CID       int64
	CName     string
	OrgID     int64
	Title     string
	CreatedAt time.Time
}

type ChapterMeta struct {
	CID    int64
	Name   string
	Prefix string
}

type BookListItem struct {
	Title      string            `json:"title"`
	OrgID      int64             `json:"org_id"`
	Desc       string            `json:"desc"`
	CoverURL   string            `json:"cover_url"`
	UpdateTime string            `json:"update_time"`
	Cover      map[string]string `json:"cover"`
}

type LatestListItem struct {
	CName      string `json:"cname"`
	Title      string `json:"title"`
	OrgID      int64  `json:"org_id"`
	CID        int64  `json:"cid"`
	Prefix     string `json:"prefix"`
	CoverURL   string `json:"cover_url"`
	UpdateTime string `json:"update_time"`
}

type ChapterPage struct {
	ChapterName string   `json:"chapter_name"`
	CID         int64    `json:"cid"`
	Index       int      `json:"index"`
	Page        int      `json:"page"`
	TotalPages  int      `json:"total_pages"`
	PrevCID     int64    `json:"prev_cid"`
	NextCID     int64    `json:"next_cid"`
	Prefix      string   `json:"prefix"`
	Images      []string `json:"images"`
}

type ComicUseCase struct {
	repo   ComicRepo
	cfg    *conf.Bootstrap
	log    *log.Helper
	httpc  *http.Client
	jitter func() time.Duration
}

func NewComicUseCase(repo ComicRepo, cfg *conf.Bootstrap, logger log.Logger) *ComicUseCase {
	timeout := time.Duration(cfg.GetMh().GetSpider().GetRequestTimeoutSeconds()) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &ComicUseCase{
		repo: repo,
		cfg:  cfg,
		log:  log.NewHelper(log.With(logger, "module", "mh/ComicUseCase")),
		httpc: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{ //nolint:gosec
				InsecureSkipVerify: true,
			}},
		},
		jitter: func() time.Duration {
			minMs := cfg.GetMh().GetSpider().GetMinDelayMs()
			maxMs := cfg.GetMh().GetSpider().GetMaxDelayMs()
			if minMs < 0 {
				minMs = 0
			}
			if maxMs <= minMs {
				maxMs = minMs + 1
			}
			return time.Duration(rand.Int32N(maxMs-minMs)+minMs) * time.Millisecond
		},
	}
}

func (u *ComicUseCase) ListBooks(ctx context.Context, page, size int) ([]BookListItem, int64, int, error) {
	books, total, err := u.repo.ListBooks(ctx, page, size)
	if err != nil {
		return nil, 0, 0, err
	}
	items := make([]BookListItem, 0, len(books))
	for _, bk := range books {
		desc := bk.Desc
		if len(desc) > 80 {
			desc = desc[:80] + " ..."
		}
		items = append(items, BookListItem{
			Title:      bk.Title,
			OrgID:      bk.OrgID,
			Desc:       desc,
			CoverURL:   u.coverURL(bk.Cover, bk.OrgID),
			UpdateTime: bk.CreatedAt.Format("2006-01-02 15:04:05"),
			Cover:      bk.Cover,
		})
	}
	pages := int(math.Ceil(float64(total) / float64(max(size, 1))))
	return items, total, pages, nil
}

func (u *ComicUseCase) ListLatest(ctx context.Context, page, size int) ([]LatestListItem, int64, int, error) {
	rows, total, err := u.repo.ListLatest(ctx, page, size)
	if err != nil {
		return nil, 0, 0, err
	}
	items := make([]LatestListItem, 0, len(rows))
	for _, row := range rows {
		root := strings.Split(strings.TrimPrefix(row.Prefix, "jj-"), "/")
		cover := ""
		if len(root) > 0 {
			cover = u.ossURL(path.Join(u.bucketName(), "jj-"+root[0], "cover.jpg"))
		}
		items = append(items, LatestListItem{
			CName:      row.CName,
			Title:      row.Title,
			OrgID:      row.OrgID,
			CID:        row.CID,
			Prefix:     row.Prefix,
			CoverURL:   cover,
			UpdateTime: row.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	pages := int(math.Ceil(float64(total) / float64(max(size, 1))))
	return items, total, pages, nil
}

func (u *ComicUseCase) GetBookDetail(ctx context.Context, orgID int64) (*BookListItem, []ChapterMeta, int64, error) {
	bk, err := u.repo.FindBookByOrgID(ctx, orgID)
	if err != nil {
		return nil, nil, 0, err
	}
	if bk == nil {
		return nil, nil, 0, nil
	}
	chapters, err := u.repo.ListChapterDirs(ctx, orgID)
	if err != nil {
		return nil, nil, 0, err
	}
	lastCID, err := u.repo.GetHistoryCID(ctx, orgID)
	if err != nil {
		return nil, nil, 0, err
	}
	return &BookListItem{
		Title:      bk.Title,
		OrgID:      bk.OrgID,
		Desc:       bk.Desc,
		CoverURL:   u.coverURL(bk.Cover, bk.OrgID),
		UpdateTime: bk.CreatedAt.Format("2006-01-02 15:04:05"),
		Cover:      bk.Cover,
	}, chapters, lastCID, nil
}

func (u *ComicUseCase) GetChapterPage(ctx context.Context, orgID, cid int64, index int, prefix string) (*ChapterPage, error) {
	chapters, err := u.repo.ListChapterDirs(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, errors.New("chapter not found")
	}

	inx := resolveChapterIndex(chapters, cid, index)
	selected := chapters[inx]
	reqPrefix := normalizeChapterPrefix(prefix)
	imgs, usedPrefix, err := u.loadChapterImages(ctx, orgID, selected, reqPrefix)
	if err != nil {
		return nil, err
	}
	if err := u.repo.UpsertHistory(ctx, orgID, selected.CID); err != nil {
		u.log.Warnf("save history failed: %v", err)
	}
	return u.buildChapterPage(chapters, inx, usedPrefix, imgs), nil
}

func (u *ComicUseCase) Sync(ctx context.Context) error {
	stateID := strings.TrimSpace(u.cfg.GetMh().GetSync().GetStateId())
	if stateID == "" {
		stateID = "mh-default-sync"
	}
	stop := int(u.cfg.GetMh().GetSync().GetStopAfterConsecutiveErrors())
	if stop <= 0 {
		stop = 2
	}

	lastCID, err := u.repo.GetSyncCID(ctx, stateID)
	if err != nil {
		return err
	}

	errCnt := 0
	hasNew := false
	for errCnt < stop {
		nextCID := lastCID + 1
		u.log.Infof("Try to sync with chapter=%d", nextCID)
		if err := u.processChapter(ctx, nextCID); err != nil {
			errCnt++
			lastCID = nextCID
			u.log.Warnf("sync chapter=%d failed: %v", nextCID, err)
			continue
		}
		errCnt = 0
		hasNew = true
		lastCID = nextCID
		u.log.Infof("sync chapter=%d finished, updating sync state", lastCID)
		if err := u.repo.UpsertSyncCID(ctx, stateID, lastCID); err != nil {
			return err
		}
		u.log.Infof("sync chapter=%d sync state updated", lastCID)
	}

	if hasNew {
		pages := int(u.cfg.GetMh().GetSpider().GetBootstrapBookListPages())
		if pages <= 0 {
			pages = 1
		}
		for i := 1; i <= pages; i++ {
			if err := u.CrawlBookListPage(ctx, i); err != nil {
				u.log.Warnf("crawl book list page=%d failed: %v", i, err)
			}
		}
	}

	return nil
}

func (u *ComicUseCase) Proof(ctx context.Context) error {
	const pageSize = 1000
	for page := 1; ; page++ {
		books, _, _, err := u.ListBooksRaw(ctx, page, pageSize)
		if err != nil {
			return err
		}
		if len(books) == 0 {
			break
		}

		orgIDs := make([]int64, 0, len(books))
		for _, b := range books {
			orgIDs = append(orgIDs, b.OrgID)
		}
		proofed, err := u.repo.GetProofedOrgIDs(ctx, orgIDs)
		if err != nil {
			return err
		}

		for _, bk := range books {
			if _, ok := proofed[bk.OrgID]; ok {
				continue
			}
			if err := u.proofOne(ctx, bk); err != nil {
				u.log.Warnf("proof org_id=%d failed: %v", bk.OrgID, err)
			}
			_ = u.repo.InsertProofed(ctx, bk.OrgID)
		}

		if len(books) < pageSize {
			break
		}
	}
	return nil
}

func (u *ComicUseCase) CrawlBookListPage(ctx context.Context, page int) error {
	urlStr := u.formatURL(u.cfg.GetMh().GetSpider().GetBookListUrlTemplate(), int64(page))
	resp, err := u.fetchWithRetry(ctx, urlStr, map[string]string{
		"authority": u.cfg.GetMh().GetSpider().GetAuthority(),
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}

	doc.Find(".mh-item").Each(func(_ int, s *goquery.Selection) {
		a := s.Find("a").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		orgID := parseOrgID(href)
		if orgID <= 0 {
			return
		}

		existing, err := u.repo.FindBookByOrgID(ctx, orgID)
		if err != nil || existing != nil {
			return
		}

		bgURL := extractCoverURL(s.Find(".mh-cover").AttrOr("style", ""))
		cover := map[string]string{}
		if bgURL != "" {
			if v, err := u.repo.SaveImageFromURL(ctx, toAbsURL(resp.Request.URL, bgURL), fmt.Sprintf("jj-%d/cover.jpg", orgID)); err == nil {
				cover = v
			}
		}

		title := strings.TrimSpace(a.AttrOr("title", ""))
		desc := strings.TrimSpace(s.Find(".chapter").Text())
		bookURL := toAbsURL(resp.Request.URL, href)
		if err := u.repo.UpsertBook(ctx, Book{
			Title:     title,
			URL:       bookURL,
			Desc:      desc,
			OrgID:     orgID,
			Cover:     cover,
			CreatedAt: time.Now(),
		}); err != nil {
			u.log.Warnf("upsert book from list failed: org_id=%d err=%v", orgID, err)
		}
	})
	return nil
}

func (u *ComicUseCase) processChapter(ctx context.Context, cid int64) error {
	urlStr := u.formatURL(u.cfg.GetMh().GetSpider().GetChapterUrlTemplate(), cid)
	referer := u.formatURL(u.cfg.GetMh().GetSpider().GetChapterRefererTemplate(), 591)
	u.log.Infof("process chapter start cid=%d url=%s", cid, urlStr)
	resp, err := u.fetchWithRetry(ctx, urlStr, map[string]string{
		"authority": u.cfg.GetMh().GetSpider().GetAuthority(),
		"referer":   referer,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}
	u.log.Infof("process chapter parsed html cid=%d", cid)

	orgID, cName, bookURL, title, chapterDir, images, err := parseChapterMeta(doc, resp.Request.URL, cid)
	if err != nil {
		return err
	}
	if err := u.ensureBookExists(ctx, orgID, cName, bookURL); err != nil {
		u.log.Warnf("ensure book exists failed: org_id=%d err=%v", orgID, err)
	}
	u.log.Infof("process chapter resolved meta cid=%d org_id=%d cname=%s title=%s image_count=%d chapter_dir=%s", cid, orgID, cName, title, images.Length(), chapterDir)

	count := u.syncChapterImages(ctx, cid, orgID, chapterDir, resp.Request.URL, images)
	u.log.Infof("process chapter images completed cid=%d total_images=%d", cid, count)
	if err := u.insertLatestRecord(ctx, cid, orgID, cName, title, chapterDir); err != nil {
		return err
	}
	u.log.Infof("process chapter done cid=%d org_id=%d", cid, orgID)
	return nil
}

func resolveChapterIndex(chapters []ChapterMeta, cid int64, index int) int {
	if index >= 0 && index < len(chapters) {
		return index
	}
	for i, c := range chapters {
		if c.CID == cid {
			return i
		}
	}
	return len(chapters) - 1
}

func normalizeChapterPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	// Some clients send raw query without encodeURIComponent, so '+' in base64
	// chapter dir becomes ' ' after query parsing.
	return strings.ReplaceAll(prefix, " ", "+")
}

func (u *ComicUseCase) loadChapterImages(ctx context.Context, orgID int64, selected ChapterMeta, reqPrefix string) ([]string, string, error) {
	usedPrefix := reqPrefix
	if usedPrefix == "" {
		usedPrefix = selected.Prefix
	}
	imgs, err := u.repo.ListChapterImages(ctx, usedPrefix)
	if err != nil {
		return nil, "", err
	}
	if len(imgs) == 0 && usedPrefix != selected.Prefix {
		u.log.Warnf("chapter images empty with request prefix, fallback to selected prefix: org_id=%d cid=%d req_prefix=%s selected_prefix=%s", orgID, selected.CID, reqPrefix, selected.Prefix)
		usedPrefix = selected.Prefix
		imgs, err = u.repo.ListChapterImages(ctx, usedPrefix)
		if err != nil {
			return nil, "", err
		}
	}
	for i := range imgs {
		imgs[i] = u.normalizeFrontURL(imgs[i])
	}
	return imgs, usedPrefix, nil
}

func (u *ComicUseCase) buildChapterPage(chapters []ChapterMeta, index int, prefix string, imgs []string) *ChapterPage {
	selected := chapters[index]
	ret := &ChapterPage{
		ChapterName: selected.Name,
		CID:         selected.CID,
		Index:       index,
		Page:        index + 1,
		TotalPages:  len(chapters),
		Prefix:      prefix,
		Images:      imgs,
	}
	if index > 0 {
		ret.PrevCID = chapters[index-1].CID
	}
	if index+1 < len(chapters) {
		ret.NextCID = chapters[index+1].CID
	}
	return ret
}

func parseChapterMeta(doc *goquery.Document, baseURL *url.URL, cid int64) (int64, string, string, string, string, *goquery.Selection, error) {
	meta := doc.Find(".comic-name").First()
	href, ok := meta.Attr("href")
	if !ok {
		return 0, "", "", "", "", nil, errors.New("chapter page missing comic-name href")
	}
	orgID := parseOrgID(href)
	if orgID <= 0 {
		return 0, "", "", "", "", nil, errors.New("resolve org id failed")
	}
	cName := strings.TrimSpace(meta.Text())
	bookURL := toAbsURL(baseURL, href)

	titleRaw := strings.TrimSpace(doc.Find(".header .title").First().Text())
	titleParts := strings.Split(titleRaw, " ")
	title := strings.TrimSpace(titleParts[len(titleParts)-1])
	chapterDir := fmt.Sprintf("%d", cid)
	if title != "" {
		chapterDir = fmt.Sprintf("%d-%s", cid, strings.ReplaceAll(base64.StdEncoding.EncodeToString([]byte(title)), "/", "_"))
	}
	images := doc.Find(".comiclist .lazy")
	return orgID, cName, bookURL, title, chapterDir, images, nil
}

func (u *ComicUseCase) syncChapterImages(ctx context.Context, cid, orgID int64, chapterDir string, baseURL *url.URL, images *goquery.Selection) int {
	count := 0
	images.Each(func(_ int, s *goquery.Selection) {
		count++
		imgURL := strings.TrimSpace(s.AttrOr("data-original", ""))
		if imgURL == "" {
			u.log.Debugf("skip chapter image cid=%d idx=%d reason=empty_url", cid, count)
			return
		}
		objectName := fmt.Sprintf("jj-%d/%s/%03d.jpg", orgID, chapterDir, count)
		absURL := toAbsURL(baseURL, imgURL)
		u.log.Debugf("process chapter image start cid=%d idx=%d object=%s url=%s", cid, count, objectName, absURL)
		if _, err := u.repo.SaveImageFromURL(ctx, absURL, objectName); err != nil {
			u.log.Warnf("download chapter image failed cid=%d idx=%d err=%v", cid, count, err)
			return
		}
		u.log.Debugf("process chapter image done cid=%d idx=%d object=%s", cid, count, objectName)
	})
	return count
}

func (u *ComicUseCase) insertLatestRecord(ctx context.Context, cid, orgID int64, cName, title, chapterDir string) error {
	prefix := fmt.Sprintf("jj-%d/%s", orgID, chapterDir)
	u.log.Infof("process chapter insert latest cid=%d org_id=%d prefix=%s", cid, orgID, prefix)
	return u.repo.InsertLatest(ctx, Latest{
		Prefix: prefix,
		CID:    cid,
		CName:  cName,
		OrgID:  orgID,
		Title:  title,
	})
}

func (u *ComicUseCase) ensureBookExists(ctx context.Context, orgID int64, title, bookURL string) error {
	existing, err := u.repo.FindBookByOrgID(ctx, orgID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = fmt.Sprintf("org-%d", orgID)
	}
	if strings.TrimSpace(bookURL) == "" {
		bookURL = u.formatURL(u.cfg.GetMh().GetSpider().GetBookDetailUrlTemplate(), orgID)
	}
	return u.repo.UpsertBook(ctx, Book{
		Title:     title,
		URL:       bookURL,
		Desc:      "",
		OrgID:     orgID,
		Cover:     map[string]string{},
		CreatedAt: time.Now(),
	})
}

func (u *ComicUseCase) ReconcileBooksFromLatest(ctx context.Context, startOrgID, endOrgID int64, limit int, dryRun bool) (int, int, error) {
	seeds, err := u.repo.ListMissingBookSeedsFromLatest(ctx, startOrgID, endOrgID, limit)
	if err != nil {
		return 0, 0, err
	}

	created := 0
	for _, seed := range seeds {
		title := strings.TrimSpace(seed.Title)
		if title == "" {
			title = fmt.Sprintf("org-%d", seed.OrgID)
		}
		bookURL := strings.TrimSpace(seed.URL)
		if bookURL == "" {
			bookURL = u.formatURL(u.cfg.GetMh().GetSpider().GetBookDetailUrlTemplate(), seed.OrgID)
		}
		if dryRun {
			u.log.Infof("dry-run reconcile book: org_id=%d title=%s url=%s", seed.OrgID, title, bookURL)
			continue
		}
		if err := u.repo.UpsertBook(ctx, Book{
			Title:     title,
			URL:       bookURL,
			Desc:      "",
			OrgID:     seed.OrgID,
			Cover:     map[string]string{},
			CreatedAt: seed.CreatedAt,
		}); err != nil {
			u.log.Warnf("reconcile upsert book failed: org_id=%d err=%v", seed.OrgID, err)
			continue
		}
		created++
	}
	return len(seeds), created, nil
}

func (u *ComicUseCase) proofOne(ctx context.Context, bk Book) error {
	urlStr := u.formatURL(u.cfg.GetMh().GetSpider().GetBookDetailUrlTemplate(), bk.OrgID)
	resp, err := u.fetchWithRetry(ctx, urlStr, map[string]string{
		"authority": u.cfg.GetMh().GetSpider().GetAuthority(),
		"referer":   urlStr,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}

	descSel := doc.Find(".banner_detail_form .info .content").First()
	if descSel.Length() == 0 {
		return nil
	}

	fields := map[string]any{"desc": strings.TrimSpace(descSel.Text())}
	if strings.TrimSpace(bk.Cover["file"]) == "" {
		coverURL := strings.TrimSpace(doc.Find(".banner_detail_form .cover img").First().AttrOr("src", ""))
		if coverURL != "" {
			if ret, e := u.repo.SaveImageFromURL(ctx, toAbsURL(resp.Request.URL, coverURL), fmt.Sprintf("jj-%d/cover.jpg", bk.OrgID)); e == nil {
				fields["cover"] = ret
			}
		}
	}
	return u.repo.UpdateBookFields(ctx, bk.ID, fields)
}

func (u *ComicUseCase) ListBooksRaw(ctx context.Context, page, size int) ([]Book, int64, int, error) {
	books, total, err := u.repo.ListBooks(ctx, page, size)
	if err != nil {
		return nil, 0, 0, err
	}
	pages := int(math.Ceil(float64(total) / float64(max(size, 1))))
	return books, total, pages, nil
}

func (u *ComicUseCase) fetchWithRetry(ctx context.Context, urlStr string, extraHeaders map[string]string) (*http.Response, error) {
	retries := int(u.cfg.GetMh().GetSpider().GetRetryTimes())
	if retries <= 0 {
		retries = 3
	}

	var lastErr error
	for i := 0; i <= retries; i++ {
		delay := u.jitter()
		u.log.Debugf("fetch attempt=%d/%d url=%s delay=%s", i+1, retries+1, urlStr, delay)
		time.Sleep(delay)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
		for k, v := range extraHeaders {
			if strings.TrimSpace(v) != "" {
				req.Header.Set(k, v)
			}
		}
		resp, err := u.httpc.Do(req)
		if err != nil {
			lastErr = err
			u.log.Debugf("fetch failed attempt=%d/%d url=%s err=%v", i+1, retries+1, urlStr, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
			u.log.Debugf("fetch failed attempt=%d/%d url=%s status=%d", i+1, retries+1, urlStr, resp.StatusCode)
			_ = resp.Body.Close()
			continue
		}
		u.log.Debugf("fetch success attempt=%d/%d url=%s", i+1, retries+1, urlStr)
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("unknown request error")
	}
	return nil, lastErr
}

func (u *ComicUseCase) formatURL(tpl string, v int64) string {
	if strings.Contains(tpl, "%") {
		return fmt.Sprintf(tpl, v)
	}
	return tpl
}

func (u *ComicUseCase) coverURL(cover map[string]string, orgID int64) string {
	if file := strings.TrimSpace(cover["file"]); file != "" {
		bucket := strings.TrimSpace(cover["bucket"])
		if bucket == "" {
			bucket = u.bucketName()
		}
		return u.ossURL(path.Join(bucket, file))
	}
	fallback := strings.TrimSpace(u.cfg.GetMh().GetStorage().GetDefaultCoverUrl())
	if fallback != "" {
		return u.normalizeFrontURL(fallback)
	}
	return u.ossURL(path.Join(u.bucketName(), fmt.Sprintf("jj-%d/cover.jpg", orgID)))
}

func (u *ComicUseCase) publicBaseURL() string {
	return strings.TrimSuffix(u.cfg.GetMh().GetStorage().GetPublicBaseUrl(), "/")
}
func (u *ComicUseCase) bucketName() string {
	bucket := strings.TrimSpace(u.cfg.GetMh().GetStorage().GetBucket())
	if bucket == "" {
		return "ihm"
	}
	return bucket
}

func (u *ComicUseCase) ossURL(objectPath string) string {
	cleaned := strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(objectPath)), "/")
	if cleaned == "" || cleaned == "." {
		return "/oss/"
	}
	return "/oss/" + cleaned
}

func (u *ComicUseCase) normalizeFrontURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/oss/") {
		return raw
	}
	if strings.HasPrefix(raw, "oss/") {
		return "/" + raw
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.IsAbs() {
		return u.ossURL(strings.TrimPrefix(parsed.Path, "/"))
	}
	if strings.HasPrefix(raw, "/") {
		return u.ossURL(strings.TrimPrefix(raw, "/"))
	}
	return u.ossURL(raw)
}

func toAbsURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err == nil && u.IsAbs() {
		return raw
	}
	if base == nil {
		return raw
	}
	return base.ResolveReference(&url.URL{Path: raw}).String()
}

func parseOrgID(href string) int64 {
	href = strings.TrimSpace(href)
	href = strings.TrimSuffix(href, "/")
	parts := strings.Split(href, "/")
	if len(parts) == 0 {
		return 0
	}
	id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func extractCoverURL(style string) string {
	start := strings.Index(style, "url(")
	if start < 0 {
		return ""
	}
	rest := style[start+4:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(rest[:end]), `"'`)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
