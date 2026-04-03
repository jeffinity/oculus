package data

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const consumerLoansCollectionName = "consumer_loans"

type ConsumerLoan struct {
	DetailID    string    `gorm:"column:detail_id;type:varchar(26);primaryKey;comment:资产明细ID(ULID)"`
	TotalAmount string    `gorm:"column:total_amount;type:varchar(64);not null;comment:消费贷总金额"`
	StartYM     string    `gorm:"column:start_ym;type:varchar(7);not null;comment:开始月份(YYYY.MM)"`
	TermMonths  int32     `gorm:"column:term_months;type:int;not null;comment:总期数(月)"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamptz;not null;comment:创建时间"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

type ConsumerLoanWithDetail struct {
	ConsumerLoan
	AssetName  string
	Remark     string
	PresetCode string
	AssetType  string
	SubType    string
}

func (*ConsumerLoan) TableName() string { return consumerLoansCollectionName }

type ConsumerLoanRepo struct {
	data *Data
	log  *log.Helper
}

func NewConsumerLoanRepo(data *Data, logger log.Logger) *ConsumerLoanRepo {
	repo := &ConsumerLoanRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "bookkeeping/consumerLoanRepo")),
	}
	repo.ensureSchema(context.Background())
	return repo
}

func (r *ConsumerLoanRepo) Upsert(ctx context.Context, item *ConsumerLoan) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO consumer_loans (detail_id, total_amount, start_ym, term_months, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (detail_id)
		 DO UPDATE SET total_amount = EXCLUDED.total_amount, start_ym = EXCLUDED.start_ym, term_months = EXCLUDED.term_months, updated_at = EXCLUDED.updated_at`,
		item.DetailID, item.TotalAmount, item.StartYM, item.TermMonths, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (r *ConsumerLoanRepo) FindByDetailID(ctx context.Context, detailID string) (*ConsumerLoan, error) {
	var item ConsumerLoan
	err := r.data.db.QueryRowContext(
		ctx,
		`SELECT detail_id, total_amount, start_ym, term_months, created_at, updated_at
		 FROM consumer_loans
		 WHERE detail_id = $1`,
		detailID,
	).Scan(&item.DetailID, &item.TotalAmount, &item.StartYM, &item.TermMonths, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ConsumerLoanRepo) DeleteByDetailID(ctx context.Context, detailID string) error {
	_, err := r.data.db.ExecContext(ctx, `DELETE FROM consumer_loans WHERE detail_id = $1`, detailID)
	return err
}

func (r *ConsumerLoanRepo) ListWithDetail(ctx context.Context) ([]ConsumerLoanWithDetail, error) {
	rows, err := r.data.db.QueryContext(
		ctx,
		`SELECT cl.detail_id, cl.total_amount, cl.start_ym, cl.term_months, cl.created_at, cl.updated_at,
		        ad.asset_name, ad.remark, ad.preset_code, ad.asset_type, ad.sub_type
		 FROM consumer_loans cl
		 JOIN asset_details ad ON ad.detail_id = cl.detail_id
		 ORDER BY cl.created_at ASC, cl.detail_id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ConsumerLoanWithDetail, 0)
	for rows.Next() {
		var item ConsumerLoanWithDetail
		if err = rows.Scan(
			&item.DetailID, &item.TotalAmount, &item.StartYM, &item.TermMonths, &item.CreatedAt, &item.UpdatedAt,
			&item.AssetName, &item.Remark, &item.PresetCode, &item.AssetType, &item.SubType,
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

func (r *ConsumerLoanRepo) ensureSchema(ctx context.Context) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS consumer_loans (
			detail_id TEXT PRIMARY KEY,
			total_amount TEXT NOT NULL,
			start_ym TEXT NOT NULL,
			term_months INTEGER NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_consumer_loans_start_ym ON consumer_loans (start_ym)`,
	}
	for i := range stmts {
		if _, err := r.data.db.ExecContext(ctx, stmts[i]); err != nil {
			r.log.Warnf("ensure consumer_loans schema failed: %v", err)
		}
	}
}
