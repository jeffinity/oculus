package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/app_init"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	flagMongoURI string
	flagMongoDB  string
	flagPGDSN    string
	flagTruncate bool
)

func newMongoToPGCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mongo-to-pg",
		Short: "将 MongoDB(bookkeeping) 全量迁移到 PostgreSQL",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMongoToPG()
		},
	}

	c.Flags().StringVar(&flagMongoURI, "mongo-uri", "mongodb://root:jeff1992@10.10.10.4:27017/bookkeeping?authSource=admin", "MongoDB 连接串")
	c.Flags().StringVar(&flagMongoDB, "mongo-db", "bookkeeping", "MongoDB 数据库名")
	c.Flags().StringVar(&flagPGDSN, "pg-dsn", "", "PostgreSQL DSN，未设置则读取 --conf 中 data.postgres.dsn")
	c.Flags().BoolVar(&flagTruncate, "truncate", true, "迁移前是否清空 PG 目标表")
	return c
}

func runMongoToPG() error {
	m, cleanup, logger, err := buildMigrator()
	if err != nil {
		return err
	}
	defer cleanup()

	hl := log.NewHelper(logger)
	if m == nil || m.core == nil {
		hl.Warn("不支持 migrate")
		return nil
	}

	hl.Infof("开始执行 PG 表结构迁移 ...")
	if err = m.MigrateAll(); err != nil {
		return errors.WithStack(err)
	}

	bc, err := app_init.LoadConf(file.NewSource(flagConf))
	if err != nil {
		return errors.Wrap(err, "加载配置失败")
	}
	bc, err = app_init.LoadNacosConf(bc)
	if err != nil {
		return errors.Wrap(err, "加载 Nacos 配置失败")
	}

	pgDSN := strings.TrimSpace(flagPGDSN)
	if pgDSN == "" {
		pgDSN = strings.TrimSpace(bc.GetData().GetPostgres().GetDsn())
	}
	if pgDSN == "" {
		return errors.New("postgres dsn 为空，请使用 --pg-dsn 或在配置中设置 data.postgres.dsn")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	pgDB, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return errors.Wrap(err, "连接 PG 失败")
	}
	defer pgDB.Close()
	if err = pgDB.PingContext(ctx); err != nil {
		return errors.Wrap(err, "PG ping 失败")
	}

	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(strings.TrimSpace(flagMongoURI)))
	if err != nil {
		return errors.Wrap(err, "连接 MongoDB 失败")
	}
	defer mongoClient.Disconnect(context.Background())
	if err = mongoClient.Ping(ctx, readpref.Primary()); err != nil {
		return errors.Wrap(err, "MongoDB ping 失败")
	}
	mdb := mongoClient.Database(strings.TrimSpace(flagMongoDB))

	if flagTruncate {
		hl.Infof("清空 PG 目标表 ...")
		if err = truncatePGTables(ctx, pgDB); err != nil {
			return errors.Wrap(err, "清空 PG 目标表失败")
		}
	}

	hl.Infof("开始迁移 Mongo -> PG ...")
	assetsN, err := migrateAssets(ctx, mdb, pgDB)
	if err != nil {
		return errors.Wrap(err, "迁移 assets 失败")
	}
	ledgersN, err := migrateLedgers(ctx, mdb, pgDB)
	if err != nil {
		return errors.Wrap(err, "迁移 ledgers 失败")
	}
	loansN, err := migrateLoans(ctx, mdb, pgDB)
	if err != nil {
		return errors.Wrap(err, "迁移 loans 失败")
	}
	adjN, err := migrateLoanRateAdjustments(ctx, mdb, pgDB)
	if err != nil {
		return errors.Wrap(err, "迁移 loan_rate_adjustments 失败")
	}
	prepayN, err := migrateLoanPrepayments(ctx, mdb, pgDB)
	if err != nil {
		return errors.Wrap(err, "迁移 loan_prepayments 失败")
	}

	hl.Infof("迁移完成 assets=%d ledgers=%d loans=%d adjustments=%d prepayments=%d", assetsN, ledgersN, loansN, adjN, prepayN)
	return nil
}

func truncatePGTables(ctx context.Context, pg *sql.DB) error {
	_, err := pg.ExecContext(ctx, `TRUNCATE TABLE loan_prepayments, loan_rate_adjustments, loans, ledgers, assets`)
	return err
}

func migrateAssets(ctx context.Context, mdb *mongo.Database, pg *sql.DB) (int64, error) {
	cur, err := mdb.Collection("assets").Find(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	var n int64
	for cur.Next(ctx) {
		var doc bson.M
		if err = cur.Decode(&doc); err != nil {
			return n, err
		}
		ym := trimString(doc["ym"])
		if ym == "" {
			continue
		}
		_, err = pg.ExecContext(ctx,
			`INSERT INTO assets (ym, asset, net_asset, liability, remark)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (ym)
			 DO UPDATE SET asset = EXCLUDED.asset, net_asset = EXCLUDED.net_asset, liability = EXCLUDED.liability, remark = EXCLUDED.remark`,
			ym, trimString(doc["asset"]), trimString(doc["net_asset"]), trimString(doc["liability"]), trimString(doc["remark"]),
		)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, cur.Err()
}

func migrateLedgers(ctx context.Context, mdb *mongo.Database, pg *sql.DB) (int64, error) {
	cur, err := mdb.Collection("ledgers").Find(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	var n int64
	for cur.Next(ctx) {
		var doc bson.M
		if err = cur.Decode(&doc); err != nil {
			return n, err
		}
		entryDate := normalizeDateString(trimString(doc["entry_date"]))
		category := trimString(doc["category"])
		amount := trimString(doc["amount"])
		if entryDate == "" || category == "" || amount == "" {
			continue
		}
		createdAt := valueAsTime(doc["created_at"])
		updatedAt := valueAsTime(doc["updated_at"])
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		_, err = pg.ExecContext(ctx,
			`INSERT INTO ledgers (entry_date, ym, amount, category, remark, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (entry_date, category, amount)
			 DO UPDATE SET ym = EXCLUDED.ym, remark = EXCLUDED.remark, updated_at = EXCLUDED.updated_at`,
			entryDate,
			trimString(doc["ym"]),
			amount,
			category,
			trimString(doc["remark"]),
			createdAt,
			updatedAt,
		)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, cur.Err()
}

func migrateLoans(ctx context.Context, mdb *mongo.Database, pg *sql.DB) (int64, error) {
	cur, err := mdb.Collection("loans").Find(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	var n int64
	for cur.Next(ctx) {
		var doc bson.M
		if err = cur.Decode(&doc); err != nil {
			return n, err
		}
		loanID := trimString(doc["loan_id"])
		if loanID == "" {
			continue
		}
		createdAt := valueAsTime(doc["created_at"])
		updatedAt := valueAsTime(doc["updated_at"])
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		_, err = pg.ExecContext(ctx,
			`INSERT INTO loans (
				loan_id, loan_name, loan_type, loan_date, initial_principal, annual_rate, term_months,
				start_date, repayment_day, start_ym, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (loan_id)
			DO UPDATE SET
				loan_name = EXCLUDED.loan_name,
				loan_type = EXCLUDED.loan_type,
				loan_date = EXCLUDED.loan_date,
				initial_principal = EXCLUDED.initial_principal,
				annual_rate = EXCLUDED.annual_rate,
				term_months = EXCLUDED.term_months,
				start_date = EXCLUDED.start_date,
				repayment_day = EXCLUDED.repayment_day,
				start_ym = EXCLUDED.start_ym,
				updated_at = EXCLUDED.updated_at`,
			loanID,
			trimString(doc["loan_name"]),
			trimString(doc["loan_type"]),
			normalizeDateString(trimString(doc["loan_date"])),
			trimString(doc["initial_principal"]),
			trimString(doc["annual_rate"]),
			valueAsInt32(doc["term_months"]),
			normalizeDateString(trimString(doc["start_date"])),
			valueAsInt32(doc["repayment_day"]),
			trimString(doc["start_ym"]),
			createdAt,
			updatedAt,
		)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, cur.Err()
}

func migrateLoanRateAdjustments(ctx context.Context, mdb *mongo.Database, pg *sql.DB) (int64, error) {
	cur, err := mdb.Collection("loan_rate_adjustments").Find(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	var n int64
	for cur.Next(ctx) {
		var doc bson.M
		if err = cur.Decode(&doc); err != nil {
			return n, err
		}
		loanID := trimString(doc["loan_id"])
		effectiveDate := normalizeDateString(trimString(doc["effective_date"]))
		effectiveYM := trimString(doc["effective_ym"])
		if effectiveDate == "" {
			effectiveDate = effectiveYM
		}
		if loanID == "" || effectiveDate == "" {
			continue
		}
		createdAt := valueAsTime(doc["created_at"])
		updatedAt := valueAsTime(doc["updated_at"])
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		_, err = pg.ExecContext(ctx,
			`INSERT INTO loan_rate_adjustments (loan_id, effective_date, effective_ym, annual_rate, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (loan_id, effective_date)
			 DO UPDATE SET annual_rate = EXCLUDED.annual_rate, effective_ym = EXCLUDED.effective_ym, updated_at = EXCLUDED.updated_at`,
			loanID, effectiveDate, effectiveYM, trimString(doc["annual_rate"]), createdAt, updatedAt,
		)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, cur.Err()
}

func migrateLoanPrepayments(ctx context.Context, mdb *mongo.Database, pg *sql.DB) (int64, error) {
	cur, err := mdb.Collection("loan_prepayments").Find(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	var n int64
	for cur.Next(ctx) {
		var doc bson.M
		if err = cur.Decode(&doc); err != nil {
			return n, err
		}
		loanID := trimString(doc["loan_id"])
		prepaymentDate := normalizeDateString(trimString(doc["prepayment_date"]))
		if loanID == "" || prepaymentDate == "" {
			continue
		}
		amount := trimString(doc["amount"])
		mode := trimString(doc["mode"])
		createdAt := valueAsTime(doc["created_at"])
		updatedAt := valueAsTime(doc["updated_at"])
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}

		res, e := pg.ExecContext(ctx,
			`INSERT INTO loan_prepayments (loan_id, prepayment_date, amount, mode, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (loan_id, prepayment_date, amount, mode, created_at) DO NOTHING`,
			loanID, prepaymentDate, amount, mode, createdAt, updatedAt,
		)
		if e != nil {
			return n, e
		}
		affected, _ := res.RowsAffected()
		n += affected
	}
	return n, cur.Err()
}

func trimString(v any) string {
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "<nil>" {
		return ""
	}
	return s
}

func valueAsInt32(v any) int32 {
	s := trimString(v)
	if s == "" {
		return 0
	}
	var out int32
	_, _ = fmt.Sscanf(s, "%d", &out)
	return out
}

func valueAsTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return time.Now().UTC()
		}
		return t
	case primitive.DateTime:
		tm := t.Time()
		if tm.IsZero() {
			return time.Now().UTC()
		}
		return tm
	case string:
		raw := strings.TrimSpace(t)
		if raw == "" {
			return time.Now().UTC()
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts
		}
		if ts, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
			return ts
		}
	}
	return time.Now().UTC()
}

func normalizeDateString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) >= 10 {
		raw = raw[:10]
	}
	return strings.ReplaceAll(raw, "/", "-")
}
