package data

import (
	"context"
	"database/sql"

	"github.com/go-kratos/kratos/v2/log"
)

const assetsCollectionName = "assets"

type Asset struct {
	YM        string `gorm:"column:ym;type:varchar(7);primaryKey;comment:年月(YYYY.MM)"`
	Asset     string `gorm:"column:asset;type:varchar(64);not null;comment:资产总额"`
	NetAsset  string `gorm:"column:net_asset;type:varchar(64);not null;comment:净资产"`
	Liability string `gorm:"column:liability;type:varchar(64);not null;comment:负债"`
	Remark    string `gorm:"column:remark;type:text;not null;default:'';comment:备注"`
}

func (*Asset) TableName() string { return assetsCollectionName }

type AssetRepo struct {
	data *Data
	log  *log.Helper
}

func NewAssetRepo(data *Data, logger log.Logger) *AssetRepo {
	repo := &AssetRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "bookkeeping/assetRepo")),
	}
	repo.ensureSchema(context.Background())
	return repo
}

func (r *AssetRepo) List(ctx context.Context, startYM, endYM string) ([]Asset, error) {
	args := make([]any, 0, 2)
	where := ""
	if startYM != "" {
		args = append(args, startYM)
		where = " WHERE ym >= $1"
	}
	if endYM != "" {
		args = append(args, endYM)
		if where == "" {
			where = " WHERE ym <= $1"
		} else {
			where += " AND ym <= $2"
		}
	}

	query := "SELECT ym, asset, net_asset, liability, remark FROM assets" + where + " ORDER BY ym ASC"
	rows, err := r.data.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Asset, 0)
	for rows.Next() {
		var item Asset
		if err = rows.Scan(&item.YM, &item.Asset, &item.NetAsset, &item.Liability, &item.Remark); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *AssetRepo) FindByYM(ctx context.Context, ym string) (*Asset, error) {
	var item Asset
	err := r.data.db.QueryRowContext(
		ctx,
		`SELECT ym, asset, net_asset, liability, remark
		 FROM assets
		 WHERE ym = $1`,
		ym,
	).Scan(&item.YM, &item.Asset, &item.NetAsset, &item.Liability, &item.Remark)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *AssetRepo) Upsert(ctx context.Context, item *Asset) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO assets (ym, asset, net_asset, liability, remark)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (ym)
		 DO UPDATE SET asset = EXCLUDED.asset, net_asset = EXCLUDED.net_asset, liability = EXCLUDED.liability, remark = EXCLUDED.remark`,
		item.YM, item.Asset, item.NetAsset, item.Liability, item.Remark,
	)
	return err
}

func (r *AssetRepo) ensureSchema(ctx context.Context) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS assets (
			ym TEXT PRIMARY KEY,
			asset TEXT NOT NULL,
			net_asset TEXT NOT NULL,
			liability TEXT NOT NULL,
			remark TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_ym ON assets (ym)`,
	}
	for i := range stmts {
		if _, err := r.data.db.ExecContext(ctx, stmts[i]); err != nil {
			r.log.Warnf("ensure assets schema failed: %v", err)
		}
	}
}

func IsNotFound(err error) bool {
	return err == sql.ErrNoRows
}
