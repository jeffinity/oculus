package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	loansCollectionName               = "loans"
	loanRateAdjustmentsCollectionName = "loan_rate_adjustments"
	loanPrepaymentsCollectionName     = "loan_prepayments"

	LoanTypeMortgage = "mortgage"

	PrepaymentModeKeepPaymentShortenTerm = "keep_payment_shorten_term"
	PrepaymentModeKeepTermReducePayment  = "keep_term_reduce_payment"
)

type Loan struct {
	LoanID           string    `gorm:"column:loan_id;type:varchar(26);primaryKey;comment:贷款ID(ULID)"`
	LoanName         string    `gorm:"column:loan_name;type:varchar(64);not null;comment:贷款名称"`
	LoanType         string    `gorm:"column:loan_type;type:varchar(32);not null;index:idx_loans_type;comment:贷款类型"`
	LoanDate         string    `gorm:"column:loan_date;type:varchar(10);not null;default:'';comment:贷款日期"`
	InitialPrincipal string    `gorm:"column:initial_principal;type:varchar(64);not null;comment:初始本金"`
	AnnualRate       string    `gorm:"column:annual_rate;type:varchar(32);not null;comment:年利率"`
	TermMonths       int32     `gorm:"column:term_months;type:int;not null;comment:总期数(月)"`
	StartDate        string    `gorm:"column:start_date;type:varchar(10);not null;default:'';comment:开始还款日期"`
	RepaymentDay     int32     `gorm:"column:repayment_day;type:int;not null;default:0;comment:每月还款日"`
	StartYM          string    `gorm:"column:start_ym;type:varchar(7);not null;default:'';comment:开始还款年月(兼容旧字段)"` // legacy field
	CreatedAt        time.Time `gorm:"column:created_at;type:timestamptz;not null;index:idx_loans_created_at;comment:创建时间"`
	UpdatedAt        time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

type LoanRateAdjustment struct {
	LoanID        string    `gorm:"column:loan_id;type:varchar(26);not null;uniqueIndex:uk_loan_rate_adjustments_loan_effective,priority:1;index:idx_loan_rate_adjustments_loan,comment:贷款ID"`
	EffectiveDate string    `gorm:"column:effective_date;type:varchar(10);not null;uniqueIndex:uk_loan_rate_adjustments_loan_effective,priority:2;index:idx_loan_rate_adjustments_effective_date;comment:生效日期"`
	EffectiveYM   string    `gorm:"column:effective_ym;type:varchar(7);not null;default:'';comment:生效年月(兼容旧字段)"` // legacy field
	AnnualRate    string    `gorm:"column:annual_rate;type:varchar(32);not null;comment:调整后年利率"`
	CreatedAt     time.Time `gorm:"column:created_at;type:timestamptz;not null;index:idx_loan_rate_adjustments_created_at;comment:创建时间"`
	UpdatedAt     time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

type LoanPrepayment struct {
	LoanID         string    `gorm:"column:loan_id;type:varchar(26);not null;index:idx_loan_prepayments_loan_date_created,priority:1;uniqueIndex:uk_loan_prepayments_dedup,priority:1;comment:贷款ID"`
	PrepaymentDate string    `gorm:"column:prepayment_date;type:varchar(10);not null;index:idx_loan_prepayments_loan_date_created,priority:2;uniqueIndex:uk_loan_prepayments_dedup,priority:2;comment:提前还款日期"`
	Amount         string    `gorm:"column:amount;type:varchar(64);not null;uniqueIndex:uk_loan_prepayments_dedup,priority:3;comment:提前还款金额"`
	Mode           string    `gorm:"column:mode;type:varchar(64);not null;uniqueIndex:uk_loan_prepayments_dedup,priority:4;comment:提前还款模式"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null;index:idx_loan_prepayments_loan_date_created,priority:3;uniqueIndex:uk_loan_prepayments_dedup,priority:5;comment:创建时间"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*Loan) TableName() string               { return loansCollectionName }
func (*LoanRateAdjustment) TableName() string { return loanRateAdjustmentsCollectionName }
func (*LoanPrepayment) TableName() string     { return loanPrepaymentsCollectionName }

type LoanRepo struct {
	data *Data
	log  *log.Helper
}

func NewLoanRepo(data *Data, logger log.Logger) *LoanRepo {
	repo := &LoanRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "bookkeeping/loanRepo")),
	}
	repo.ensureSchema(context.Background())
	return repo
}

func (r *LoanRepo) Create(ctx context.Context, loan *Loan) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO loans (
			loan_id, loan_name, loan_type, loan_date, initial_principal, annual_rate, term_months,
			start_date, repayment_day, start_ym, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		loan.LoanID, loan.LoanName, loan.LoanType, loan.LoanDate, loan.InitialPrincipal, loan.AnnualRate,
		loan.TermMonths, loan.StartDate, loan.RepaymentDay, loan.StartYM, loan.CreatedAt, loan.UpdatedAt,
	)
	return err
}

func (r *LoanRepo) FindByLoanID(ctx context.Context, loanID string) (*Loan, error) {
	var item Loan
	err := r.data.db.QueryRowContext(
		ctx,
		`SELECT loan_id, loan_name, loan_type, loan_date, initial_principal, annual_rate, term_months,
			start_date, repayment_day, start_ym, created_at, updated_at
		 FROM loans WHERE loan_id = $1`,
		loanID,
	).Scan(
		&item.LoanID, &item.LoanName, &item.LoanType, &item.LoanDate, &item.InitialPrincipal,
		&item.AnnualRate, &item.TermMonths, &item.StartDate, &item.RepaymentDay, &item.StartYM,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *LoanRepo) UpdateRepaymentProfile(ctx context.Context, loanID, startDate string, repaymentDay int32, updatedAt time.Time) error {
	legacyYM := ""
	if len(startDate) >= 7 {
		legacyYM = strings.ReplaceAll(startDate[:7], "-", ".")
	}
	_, err := r.data.db.ExecContext(
		ctx,
		`UPDATE loans SET start_date = $1, repayment_day = $2, start_ym = $3, updated_at = $4 WHERE loan_id = $5`,
		startDate, repaymentDay, legacyYM, updatedAt, loanID,
	)
	return err
}

func (r *LoanRepo) UpsertRateAdjustment(ctx context.Context, item *LoanRateAdjustment) error {
	effectiveDate := strings.TrimSpace(item.EffectiveDate)
	if effectiveDate == "" {
		effectiveDate = strings.TrimSpace(item.EffectiveYM)
	}
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO loan_rate_adjustments (loan_id, effective_date, effective_ym, annual_rate, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (loan_id, effective_date)
		 DO UPDATE SET annual_rate = EXCLUDED.annual_rate, updated_at = EXCLUDED.updated_at, effective_ym = EXCLUDED.effective_ym`,
		item.LoanID, effectiveDate, item.EffectiveYM, item.AnnualRate, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (r *LoanRepo) List(ctx context.Context) ([]Loan, error) {
	rows, err := r.data.db.QueryContext(
		ctx,
		`SELECT loan_id, loan_name, loan_type, loan_date, initial_principal, annual_rate, term_months,
			start_date, repayment_day, start_ym, created_at, updated_at
		 FROM loans
		 ORDER BY created_at ASC, loan_id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Loan, 0)
	for rows.Next() {
		var item Loan
		if err = rows.Scan(
			&item.LoanID, &item.LoanName, &item.LoanType, &item.LoanDate, &item.InitialPrincipal,
			&item.AnnualRate, &item.TermMonths, &item.StartDate, &item.RepaymentDay, &item.StartYM,
			&item.CreatedAt, &item.UpdatedAt,
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

func (r *LoanRepo) ListRateAdjustments(ctx context.Context, loanIDs []string) ([]LoanRateAdjustment, error) {
	if len(loanIDs) == 0 {
		return []LoanRateAdjustment{}, nil
	}
	inClause, args := buildInClause(1, loanIDs)
	query := fmt.Sprintf(`SELECT loan_id, effective_date, effective_ym, annual_rate, created_at, updated_at
		FROM loan_rate_adjustments
		WHERE loan_id IN (%s)
		ORDER BY loan_id ASC, effective_date ASC, effective_ym ASC, created_at ASC`, inClause)
	rows, err := r.data.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LoanRateAdjustment, 0)
	for rows.Next() {
		var item LoanRateAdjustment
		if err = rows.Scan(&item.LoanID, &item.EffectiveDate, &item.EffectiveYM, &item.AnnualRate, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *LoanRepo) AddPrepayment(ctx context.Context, item *LoanPrepayment) error {
	_, err := r.data.db.ExecContext(
		ctx,
		`INSERT INTO loan_prepayments (loan_id, prepayment_date, amount, mode, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		item.LoanID, item.PrepaymentDate, item.Amount, item.Mode, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (r *LoanRepo) ListPrepayments(ctx context.Context, loanIDs []string) ([]LoanPrepayment, error) {
	if len(loanIDs) == 0 {
		return []LoanPrepayment{}, nil
	}
	inClause, args := buildInClause(1, loanIDs)
	query := fmt.Sprintf(`SELECT loan_id, prepayment_date, amount, mode, created_at, updated_at
		FROM loan_prepayments
		WHERE loan_id IN (%s)
		ORDER BY loan_id ASC, prepayment_date ASC, created_at ASC`, inClause)
	rows, err := r.data.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LoanPrepayment, 0)
	for rows.Next() {
		var item LoanPrepayment
		if err = rows.Scan(&item.LoanID, &item.PrepaymentDate, &item.Amount, &item.Mode, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *LoanRepo) ensureSchema(ctx context.Context) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS loans (
			loan_id TEXT PRIMARY KEY,
			loan_name TEXT NOT NULL,
			loan_type TEXT NOT NULL,
			loan_date TEXT NOT NULL DEFAULT '',
			initial_principal TEXT NOT NULL,
			annual_rate TEXT NOT NULL,
			term_months INTEGER NOT NULL,
			start_date TEXT NOT NULL DEFAULT '',
			repayment_day INTEGER NOT NULL DEFAULT 0,
			start_ym TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS loan_rate_adjustments (
			loan_id TEXT NOT NULL,
			effective_date TEXT NOT NULL,
			effective_ym TEXT NOT NULL DEFAULT '',
			annual_rate TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			CONSTRAINT loan_rate_adjustments_unique UNIQUE (loan_id, effective_date)
		)`,
		`CREATE TABLE IF NOT EXISTS loan_prepayments (
			loan_id TEXT NOT NULL,
			prepayment_date TEXT NOT NULL,
			amount TEXT NOT NULL,
			mode TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_loan_prepayments_dedup ON loan_prepayments (loan_id, prepayment_date, amount, mode, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_loan_prepayments_loan_date_created ON loan_prepayments (loan_id, prepayment_date, created_at)`,
	}
	for i := range stmts {
		if _, err := r.data.db.ExecContext(ctx, stmts[i]); err != nil {
			r.log.Warnf("ensure loans schema failed: %v", err)
		}
	}
}

func buildInClause(start int, values []string) (string, []any) {
	parts := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for i := range values {
		parts = append(parts, fmt.Sprintf("$%d", start+i))
		args = append(args, values[i])
	}
	return strings.Join(parts, ","), args
}
