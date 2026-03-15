package data

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jeffinity/oculus/app/mh/internal/biz"
	"github.com/jeffinity/oculus/app/mh/internal/conf"
)

func NewMinio(c *conf.Bootstrap) (*minio.Client, error) {
	cfg := c.GetMh().GetStorage()
	if strings.TrimSpace(cfg.GetEndpoint()) == "" {
		return nil, nil
	}

	mc, err := minio.New(cfg.GetEndpoint(), &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.GetAccessKey(), cfg.GetSecretKey(), ""),
		Secure: cfg.GetSecure(),
	})
	if err != nil {
		return nil, err
	}
	return mc, nil
}

func NewComicRepo(data *Data, c *conf.Bootstrap, logger log.Logger) biz.ComicRepo {
	return NewComicRepoWithDeps(data.pg, data.mc, c, logger)
}

func NewComicRepoWithDeps(pg *gorm.DB, mc *minio.Client, c *conf.Bootstrap, logger log.Logger) biz.ComicRepo {
	return &comicRepo{
		pg:     pg,
		mc:     mc,
		cfg:    c,
		httpc:  &http.Client{Timeout: 60 * time.Second},
		logger: log.NewHelper(log.With(logger, "module", "mh/comicRepo")),
	}
}

type comicRepo struct {
	pg     *gorm.DB
	mc     *minio.Client
	cfg    *conf.Bootstrap
	httpc  *http.Client
	logger *log.Helper
}

type missingBookSeedRow struct {
	OrgID     int64     `gorm:"column:org_id"`
	CName     string    `gorm:"column:cname"`
	Title     string    `gorm:"column:title"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (r *comicRepo) ListBooks(ctx context.Context, page, size int) ([]biz.Book, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	var total int64
	if err := r.pg.WithContext(ctx).Model(&MHBook{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var books []MHBook
	err := r.pg.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&books).Error
	if err != nil {
		return nil, 0, err
	}

	ret := make([]biz.Book, 0, len(books))
	for _, bk := range books {
		ret = append(ret, biz.Book{
			ID:        bk.ID,
			Title:     bk.Title,
			URL:       bk.URL,
			Desc:      bk.Desc,
			OrgID:     bk.OrgID,
			Cover:     jsonCoverToMap(bk.Cover),
			CreatedAt: bk.CreatedAt,
		})
	}
	return ret, total, nil
}

func (r *comicRepo) ListMissingBookSeedsFromLatest(ctx context.Context, startOrgID, endOrgID int64, limit int) ([]biz.BookSeed, error) {
	if limit <= 0 {
		limit = 200
	}
	rows := make([]missingBookSeedRow, 0, limit)
	query := `
WITH ranked AS (
  SELECT
    l.org_id,
    l.cname,
    l.title,
    l.created_at,
    ROW_NUMBER() OVER (PARTITION BY l.org_id ORDER BY l.created_at DESC, l.cid DESC) AS rn
  FROM latest l
  LEFT JOIN books b ON b.org_id = l.org_id
  WHERE b.org_id IS NULL
    AND ($1::bigint <= 0 OR l.org_id >= $1::bigint)
    AND ($2::bigint <= 0 OR l.org_id <= $2::bigint)
)
SELECT
  org_id,
  cname,
  title,
  created_at
FROM ranked
WHERE rn = 1
ORDER BY org_id ASC
LIMIT $3
`
	if err := r.pg.WithContext(ctx).Raw(query, startOrgID, endOrgID, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ret := make([]biz.BookSeed, 0, len(rows))
	for _, row := range rows {
		title := strings.TrimSpace(row.CName)
		if title == "" {
			title = strings.TrimSpace(row.Title)
		}
		ret = append(ret, biz.BookSeed{
			OrgID:     row.OrgID,
			Title:     title,
			URL:       "",
			CreatedAt: row.CreatedAt,
		})
	}
	return ret, nil
}

func (r *comicRepo) FindBookByOrgID(ctx context.Context, orgID int64) (*biz.Book, error) {
	var bk MHBook
	if err := r.pg.WithContext(ctx).Where("org_id = ?", orgID).Take(&bk).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &biz.Book{
		ID:        bk.ID,
		Title:     bk.Title,
		URL:       bk.URL,
		Desc:      bk.Desc,
		OrgID:     bk.OrgID,
		Cover:     jsonCoverToMap(bk.Cover),
		CreatedAt: bk.CreatedAt,
	}, nil
}

func (r *comicRepo) UpsertBook(ctx context.Context, book biz.Book) error {
	payload := mapCoverToJSON(book.Cover)
	m := &MHBook{
		Title:     book.Title,
		URL:       book.URL,
		Desc:      book.Desc,
		OrgID:     book.OrgID,
		Cover:     payload,
		CreatedAt: book.CreatedAt,
		UpdatedAt: time.Now(),
	}
	if book.ID > 0 {
		m.ID = book.ID
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}

	return r.pg.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "url", "desc", "cover", "updated_at"}),
	}).Create(m).Error
}

func (r *comicRepo) UpdateBookFields(ctx context.Context, id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now()
	if cover, ok := fields["cover"]; ok {
		if v, ok2 := cover.(map[string]string); ok2 {
			fields["cover"] = mapCoverToJSON(v)
		}
	}
	return r.pg.WithContext(ctx).Model(&MHBook{}).Where("id = ?", id).Updates(fields).Error
}

func (r *comicRepo) ListLatest(ctx context.Context, page, size int) ([]biz.Latest, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	var total int64
	if err := r.pg.WithContext(ctx).Model(&MHLatest{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []MHLatest
	err := r.pg.WithContext(ctx).
		Order("id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	ret := make([]biz.Latest, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, biz.Latest{
			Prefix:    row.Prefix,
			CID:       row.CID,
			CName:     row.CName,
			OrgID:     row.OrgID,
			Title:     row.Title,
			CreatedAt: row.CreatedAt,
		})
	}
	return ret, total, nil
}

func (r *comicRepo) InsertLatest(ctx context.Context, latest biz.Latest) error {
	row := &MHLatest{
		Prefix:    latest.Prefix,
		CID:       latest.CID,
		CName:     latest.CName,
		OrgID:     latest.OrgID,
		Title:     latest.Title,
		CreatedAt: time.Now(),
	}
	return r.pg.WithContext(ctx).Create(row).Error
}

func (r *comicRepo) UpsertHistory(ctx context.Context, orgID, cid int64) error {
	row := &MHHistory{
		OrgID:     orgID,
		CID:       cid,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return r.pg.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "org_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"cid":        cid,
			"updated_at": time.Now(),
		}),
	}).Create(row).Error
}

func (r *comicRepo) GetHistoryCID(ctx context.Context, orgID int64) (int64, error) {
	var row MHHistory
	if err := r.pg.WithContext(ctx).Where("org_id = ?", orgID).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return row.CID, nil
}

func (r *comicRepo) GetSyncCID(ctx context.Context, stateID string) (int64, error) {
	var row MHSyncState
	if err := r.pg.WithContext(ctx).Where("id = ?", stateID).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return row.CID, nil
}

func (r *comicRepo) UpsertSyncCID(ctx context.Context, stateID string, cid int64) error {
	row := &MHSyncState{
		ID:        stateID,
		CID:       cid,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return r.pg.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"cid":        cid,
			"updated_at": time.Now(),
		}),
	}).Create(row).Error
}

func (r *comicRepo) ListBookOrgIDs(ctx context.Context, page, size int) ([]int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 100
	}
	var rows []int64
	err := r.pg.WithContext(ctx).Model(&MHBook{}).
		Order("id ASC").
		Offset((page-1)*size).
		Limit(size).
		Pluck("org_id", &rows).Error
	return rows, err
}

func (r *comicRepo) GetProofedOrgIDs(ctx context.Context, orgIDs []int64) (map[int64]struct{}, error) {
	ret := make(map[int64]struct{})
	if len(orgIDs) == 0 {
		return ret, nil
	}
	var rows []int64
	err := r.pg.WithContext(ctx).Model(&MHProofHistory{}).
		Where("org_id IN ?", orgIDs).
		Pluck("org_id", &rows).Error
	if err != nil {
		return nil, err
	}
	for _, id := range rows {
		ret[id] = struct{}{}
	}
	return ret, nil
}

func (r *comicRepo) InsertProofed(ctx context.Context, orgID int64) error {
	row := &MHProofHistory{OrgID: orgID, CreatedAt: time.Now()}
	return r.pg.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

func (r *comicRepo) ListChapterDirs(ctx context.Context, orgID int64) ([]biz.ChapterMeta, error) {
	if r.mc == nil {
		return nil, fmt.Errorf("minio is not configured")
	}

	prefix := fmt.Sprintf("jj-%d/", orgID)
	objCh := r.mc.ListObjects(ctx, r.cfg.GetMh().GetStorage().GetBucket(), minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	})

	byCID := map[int64]biz.ChapterMeta{}
	for obj := range objCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "cover.jpg") {
			continue
		}
		rel := strings.TrimPrefix(obj.Key, prefix)
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" {
			continue
		}

		cid, name, ok := parseChapterDir(rel)
		if !ok {
			continue
		}
		byCID[cid] = biz.ChapterMeta{CID: cid, Name: name, Prefix: prefix + rel}
	}

	ret := make([]biz.ChapterMeta, 0, len(byCID))
	for _, c := range byCID {
		ret = append(ret, c)
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].CID < ret[j].CID })
	for i := range ret {
		if strings.TrimSpace(ret[i].Name) == "" {
			ret[i].Name = fmt.Sprintf("第 %d 话", i+1)
		}
	}
	return ret, nil
}

func (r *comicRepo) ListChapterImages(ctx context.Context, prefix string) ([]string, error) {
	if r.mc == nil {
		return nil, fmt.Errorf("minio is not configured")
	}
	bucket := r.cfg.GetMh().GetStorage().GetBucket()
	baseURL := strings.TrimSuffix(r.cfg.GetMh().GetStorage().GetPublicBaseUrl(), "/")

	objCh := r.mc.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	ret := make([]string, 0, 64)
	for obj := range objCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		ret = append(ret, fmt.Sprintf("%s/%s/%s", baseURL, bucket, obj.Key))
	}
	sort.Strings(ret)
	return ret, nil
}

func (r *comicRepo) SaveImageFromURL(ctx context.Context, imageURL, objectName string) (map[string]string, error) {
	if r.mc == nil {
		return nil, fmt.Errorf("minio is not configured")
	}

	maxAttempts := r.downloadAttempts()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := r.downloadAndUploadImage(ctx, imageURL, objectName, attempt, maxAttempts); err != nil {
			lastErr = err
			continue
		}
		return map[string]string{"bucket": r.cfg.GetMh().GetStorage().GetBucket(), "file": objectName}, nil
	}
	return nil, lastErr
}

func (r *comicRepo) downloadAttempts() int {
	retries := int(r.cfg.GetMh().GetSpider().GetRetryTimes())
	if retries <= 0 {
		retries = 3
	}
	return retries + 1
}

func (r *comicRepo) downloadAndUploadImage(ctx context.Context, imageURL, objectName string, attempt, maxAttempts int) error {
	r.logger.Debugf("download image attempt=%d/%d object=%s url=%s", attempt, maxAttempts, objectName, imageURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return err
	}
	resp, err := r.httpc.Do(req)
	if err != nil {
		r.logger.Debugf("download image failed attempt=%d/%d object=%s url=%s err=%v", attempt, maxAttempts, objectName, imageURL, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("download image status=%d", resp.StatusCode)
		r.logger.Debugf("download image failed attempt=%d/%d object=%s url=%s status=%d", attempt, maxAttempts, objectName, imageURL, resp.StatusCode)
		return err
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		r.logger.Debugf("read image payload failed attempt=%d/%d object=%s url=%s err=%v", attempt, maxAttempts, objectName, imageURL, err)
		return err
	}

	_, err = r.mc.PutObject(
		ctx,
		r.cfg.GetMh().GetStorage().GetBucket(),
		objectName,
		bytes.NewReader(payload),
		int64(len(payload)),
		minio.PutObjectOptions{ContentType: "image/jpeg"},
	)
	if err != nil {
		r.logger.Debugf("upload image failed attempt=%d/%d object=%s url=%s err=%v", attempt, maxAttempts, objectName, imageURL, err)
		return err
	}
	r.logger.Debugf("download image success attempt=%d/%d object=%s url=%s bytes=%d", attempt, maxAttempts, objectName, imageURL, len(payload))
	return nil
}

func parseChapterDir(rel string) (int64, string, bool) {
	parts := strings.SplitN(rel, "-", 2)
	if len(parts) == 0 {
		return 0, "", false
	}
	cid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	if len(parts) == 1 {
		return cid, "", true
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(parts[1], "_", "/"))
	if err != nil {
		return cid, "", true
	}
	return cid, string(decoded), true
}

func jsonCoverToMap(j datatypes.JSON) map[string]string {
	if len(j) == 0 {
		return map[string]string{}
	}
	var ret map[string]string
	if err := json.Unmarshal(j, &ret); err != nil {
		return map[string]string{}
	}
	if ret == nil {
		return map[string]string{}
	}
	return ret
}

func mapCoverToJSON(m map[string]string) datatypes.JSON {
	if m == nil {
		m = map[string]string{}
	}
	payload, _ := json.Marshal(m)
	return payload
}
