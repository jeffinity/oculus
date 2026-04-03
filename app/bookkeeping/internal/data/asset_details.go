package data

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const assetDetailsCollectionName = "asset_details"

type AssetDetail struct {
	DetailID   string    `gorm:"column:detail_id;type:varchar(26);primaryKey;comment:资产明细ID(ULID)"`
	YM         string    `gorm:"column:ym;type:varchar(7);not null;index:idx_asset_details_ym_type_sub_type,priority:1;comment:年月(YYYY.MM)"`
	AssetType  string    `gorm:"column:asset_type;type:varchar(16);not null;index:idx_asset_details_ym_type_sub_type,priority:2;comment:资产类型(asset/liability)"`
	SubType    string    `gorm:"column:sub_type;type:varchar(32);not null;index:idx_asset_details_ym_type_sub_type,priority:3;comment:二级分类"`
	PresetCode string    `gorm:"column:preset_code;type:varchar(64);not null;default:'';comment:预置编码"`
	AssetName  string    `gorm:"column:asset_name;type:varchar(128);not null;comment:资产名称"`
	Remark     string    `gorm:"column:remark;type:text;not null;default:'';comment:备注"`
	Amount     string    `gorm:"column:amount;type:varchar(64);not null;comment:金额"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamptz;not null;index:idx_asset_details_created_at;comment:创建时间"`
	UpdatedAt  time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*AssetDetail) TableName() string { return assetDetailsCollectionName }

type AssetDetailRepo struct {
	data *Data
	log  *log.Helper
}

func NewAssetDetailRepo(data *Data, logger log.Logger) *AssetDetailRepo {
	repo := &AssetDetailRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "bookkeeping/assetDetailRepo")),
	}
	repo.ensureSchema(context.Background())
	return repo
}

func (r *AssetDetailRepo) ListByYM(ctx context.Context, ym string) ([]AssetDetail, error) {
	rows, err := r.data.db.QueryContext(
		ctx,
		`SELECT detail_id, ym, asset_type, sub_type, preset_code, asset_name, remark, amount, created_at, updated_at
		 FROM asset_details
		 WHERE ym = $1
		 ORDER BY asset_type ASC, sub_type ASC, created_at ASC, detail_id ASC`,
		ym,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AssetDetail, 0)
	for rows.Next() {
		var item AssetDetail
		if err = rows.Scan(
			&item.DetailID, &item.YM, &item.AssetType, &item.SubType, &item.PresetCode,
			&item.AssetName, &item.Remark, &item.Amount, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *AssetDetailRepo) FindByID(ctx context.Context, detailID string) (*AssetDetail, error) {
	var item AssetDetail
	err := r.data.db.QueryRowContext(
		ctx,
		`SELECT detail_id, ym, asset_type, sub_type, preset_code, asset_name, remark, amount, created_at, updated_at
		 FROM asset_details
		 WHERE detail_id = $1`,
		detailID,
	).Scan(
		&item.DetailID, &item.YM, &item.AssetType, &item.SubType, &item.PresetCode,
		&item.AssetName, &item.Remark, &item.Amount, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *AssetDetailRepo) Create(ctx context.Context, item *AssetDetail) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO asset_details (detail_id, ym, asset_type, sub_type, preset_code, asset_name, remark, amount, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		item.DetailID, item.YM, item.AssetType, item.SubType, item.PresetCode, item.AssetName, item.Remark, item.Amount, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (r *AssetDetailRepo) Update(ctx context.Context, item *AssetDetail) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`UPDATE asset_details
		 SET ym = $1, asset_type = $2, sub_type = $3, preset_code = $4, asset_name = $5, remark = $6, amount = $7, updated_at = $8
		 WHERE detail_id = $9`,
		item.YM, item.AssetType, item.SubType, item.PresetCode, item.AssetName, item.Remark, item.Amount, item.UpdatedAt, item.DetailID,
	)
	return err
}

func (r *AssetDetailRepo) DeleteByID(ctx context.Context, detailID string) error {
	_, err := r.data.db.ExecContext(ctx, `DELETE FROM asset_details WHERE detail_id = $1`, detailID)
	return err
}

func (r *AssetDetailRepo) FindLatestYMBefore(ctx context.Context, ym string) (string, error) {
	var value string
	err := r.data.db.QueryRowContext(
		ctx,
		`SELECT ym
		 FROM asset_details
		 WHERE ym < $1
		 GROUP BY ym
		 ORDER BY ym DESC
		 LIMIT 1`,
		ym,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (r *AssetDetailRepo) ensureSchema(ctx context.Context) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS asset_details (
			detail_id TEXT PRIMARY KEY,
			ym TEXT NOT NULL,
			asset_type TEXT NOT NULL,
			sub_type TEXT NOT NULL,
			preset_code TEXT NOT NULL DEFAULT '',
			asset_name TEXT NOT NULL,
			remark TEXT NOT NULL DEFAULT '',
			amount TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`ALTER TABLE asset_details ADD COLUMN IF NOT EXISTS remark TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_asset_details_ym_type_sub_type ON asset_details (ym, asset_type, sub_type)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_details_created_at ON asset_details (created_at)`,
	}
	for i := range stmts {
		if _, err := r.data.db.ExecContext(ctx, stmts[i]); err != nil {
			r.log.Warnf("ensure asset_details schema failed: %v", err)
		}
	}
}
