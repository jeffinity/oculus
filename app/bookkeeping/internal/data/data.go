package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jeffinity/singularity/migratex"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/conf"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewPostgres,
)

var AssetProviderSet = wire.NewSet(
	NewAssetRepo,
	NewLoanRepo,
	NewLedgerRepo,
)

var MigratorProviderSet = wire.NewSet(
	NewGormPostgres,
	NewAllMigrator,
)

// Data .
type Data struct {
	db *sql.DB
}

func NewAllMigrator(pg *gorm.DB) *migratex.Migrator {
	return migratex.NewAllMigrator(pg, []any{
		&Asset{},
		&Ledger{},
		&Loan{},
		&LoanRateAdjustment{},
		&LoanPrepayment{},
	})
}

// NewData .
func NewData(_ *conf.Bootstrap, db *sql.DB, logger log.Logger) (*Data, func(), error) {
	mLog := log.NewHelper(log.With(logger, "module", "bookkeeping/data"))
	d := &Data{db: db}

	cleanup := func() {
		mLog.Info("closing the data resources")
		if db != nil {
			if err := db.Close(); err != nil {
				mLog.Errorf("postgres close failed: %v", err)
			}
		}
	}

	return d, cleanup, nil
}

func NewPostgres(c *conf.Bootstrap, _ log.Logger) (*sql.DB, error) {
	dsn := c.GetData().GetPostgres().GetDsn()
	if dsn == "" {
		return nil, errors.New("postgres dsn is empty")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func NewGormPostgres(c *conf.Bootstrap, _ log.Logger) (*gorm.DB, func(), error) {
	dsn := c.GetData().GetPostgres().GetDsn()
	if dsn == "" {
		return nil, nil, errors.New("postgres dsn is empty")
	}
	pg, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		sqlDB, e := pg.DB()
		if e == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	}
	return pg, cleanup, nil
}
