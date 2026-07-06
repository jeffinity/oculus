package data

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const ledgersCollectionName = "ledgers"

var ledgerYMRegex = regexp.MustCompile(`^\d{4}\.\d{2}$`)

type Ledger struct {
	EntryDate string    `gorm:"column:entry_date;type:varchar(10);not null;uniqueIndex:uk_ledgers_entry_category_amount,priority:1;index:idx_ledgers_ym_entry_date,priority:2;comment:记账日期(YYYY-MM-DD)"`
	YM        string    `gorm:"column:ym;type:varchar(7);not null;index:idx_ledgers_ym_entry_date,priority:1;comment:年月(YYYY.MM)"`
	Amount    string    `gorm:"column:amount;type:varchar(64);not null;uniqueIndex:uk_ledgers_entry_category_amount,priority:3;comment:金额(收入为正,支出为负)"`
	Category  string    `gorm:"column:category;type:varchar(32);not null;uniqueIndex:uk_ledgers_entry_category_amount,priority:2;comment:分类"`
	Remark    string    `gorm:"column:remark;type:text;not null;default:'';comment:备注"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;index:idx_ledgers_created_at;comment:创建时间"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*Ledger) TableName() string { return ledgersCollectionName }

type LedgerRepo struct {
	data *Data
	log  *log.Helper
}

type LedgerExpensePoint struct {
	Key   string
	Total float64
}

type LedgerExpenseRank struct {
	Category string
	Total    float64
}

func NewLedgerRepo(data *Data, logger log.Logger) *LedgerRepo {
	repo := &LedgerRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "bookkeeping/ledgerRepo")),
	}
	repo.ensureSchema(context.Background())
	return repo
}

func (r *LedgerRepo) Upsert(ctx context.Context, item *Ledger) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO ledgers (entry_date, ym, amount, category, remark, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (entry_date, category, amount)
		 DO UPDATE SET ym = EXCLUDED.ym, remark = EXCLUDED.remark, updated_at = EXCLUDED.updated_at`,
		item.EntryDate, item.YM, item.Amount, item.Category, item.Remark, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (r *LedgerRepo) ListByYM(ctx context.Context, ym string) ([]Ledger, error) {
	rows, err := r.data.db.QueryContext(
		ctx,
		`SELECT entry_date, ym, amount, category, remark, created_at, updated_at
		 FROM ledgers
		 WHERE ym = $1
		 ORDER BY entry_date DESC, created_at ASC`,
		ym,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Ledger, 0)
	for rows.Next() {
		var item Ledger
		if err = rows.Scan(&item.EntryDate, &item.YM, &item.Amount, &item.Category, &item.Remark, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *LedgerRepo) ListAvailableYMs(ctx context.Context) ([]string, error) {
	rows, err := r.data.db.QueryContext(ctx, `SELECT DISTINCT ym FROM ledgers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var ym string
		if err = rows.Scan(&ym); err != nil {
			return nil, err
		}
		ym = strings.TrimSpace(ym)
		if ledgerYMRegex.MatchString(ym) {
			items = append(items, ym)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(items)
	return items, nil
}

func (r *LedgerRepo) ExpenseTrendByYM(ctx context.Context, ym string) ([]LedgerExpensePoint, error) {
	return r.aggregateExpenseTrend(ctx,
		`SELECT entry_date AS key, SUM(ABS(CAST(amount AS DOUBLE PRECISION))) AS total
		 FROM ledgers
		 WHERE ym = $1 AND CAST(amount AS DOUBLE PRECISION) < 0
		 GROUP BY entry_date
		 ORDER BY key ASC`,
		ym,
	)
}

func (r *LedgerRepo) ExpenseTrendByYear(ctx context.Context, year string) ([]LedgerExpensePoint, error) {
	return r.aggregateExpenseTrend(ctx,
		`SELECT ym AS key, SUM(ABS(CAST(amount AS DOUBLE PRECISION))) AS total
		 FROM ledgers
		 WHERE ym LIKE $1 AND CAST(amount AS DOUBLE PRECISION) < 0
		 GROUP BY ym
		 ORDER BY key ASC`,
		year+".%",
	)
}

func (r *LedgerRepo) ExpenseRankingByYM(ctx context.Context, ym string) ([]LedgerExpenseRank, error) {
	return r.aggregateExpenseRanking(ctx,
		`SELECT category, SUM(ABS(CAST(amount AS DOUBLE PRECISION))) AS total
		 FROM ledgers
		 WHERE ym = $1 AND CAST(amount AS DOUBLE PRECISION) < 0 AND btrim(category) <> ''
		 GROUP BY category
		 ORDER BY total DESC, category ASC`,
		ym,
	)
}

func (r *LedgerRepo) ExpenseRankingByYear(ctx context.Context, year string) ([]LedgerExpenseRank, error) {
	return r.aggregateExpenseRanking(ctx,
		`SELECT category, SUM(ABS(CAST(amount AS DOUBLE PRECISION))) AS total
		 FROM ledgers
		 WHERE ym LIKE $1 AND CAST(amount AS DOUBLE PRECISION) < 0 AND btrim(category) <> ''
		 GROUP BY category
		 ORDER BY total DESC, category ASC`,
		year+".%",
	)
}

func (r *LedgerRepo) aggregateExpenseTrend(ctx context.Context, query string, arg string) ([]LedgerExpensePoint, error) {
	rows, err := r.data.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LedgerExpensePoint, 0)
	for rows.Next() {
		var row LedgerExpensePoint
		if err = rows.Scan(&row.Key, &row.Total); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *LedgerRepo) aggregateExpenseRanking(ctx context.Context, query string, arg string) ([]LedgerExpenseRank, error) {
	rows, err := r.data.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LedgerExpenseRank, 0)
	for rows.Next() {
		var row LedgerExpenseRank
		if err = rows.Scan(&row.Category, &row.Total); err != nil {
			return nil, err
		}
		items = append(items, LedgerExpenseRank{
			Category: strings.TrimSpace(row.Category),
			Total:    row.Total,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *LedgerRepo) ensureSchema(ctx context.Context) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ledgers (
			entry_date TEXT NOT NULL,
			ym TEXT NOT NULL,
			amount TEXT NOT NULL,
			category TEXT NOT NULL,
			remark TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			CONSTRAINT ledgers_unique_entry UNIQUE (entry_date, category, amount)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ledgers_ym_entry_date ON ledgers (ym, entry_date DESC)`,
	}
	for i := range stmts {
		if _, err := r.data.db.ExecContext(ctx, stmts[i]); err != nil {
			r.log.Warnf("ensure ledgers schema failed: %v", err)
		}
	}
}
