package data

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jeffinity/singularity/friendly"
	"github.com/jeffinity/singularity/migratex"
	"github.com/jeffinity/singularity/pgx"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jeffinity/oculus/app/mh/internal/conf"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewPostgres,
	NewRedis,
	NewMinio,
	NewComicRepo,
	NewAllMigrator,
)

// Data .
type Data struct {
	pg *gorm.DB
	mc *minio.Client
}

func NewAllMigrator(data *Data) *migratex.Migrator {
	return migratex.NewAllMigrator(data.pg, []any{
		&MHBook{},
		&MHLatest{},
		&MHHistory{},
		&MHProofHistory{},
		&MHSyncState{},
		&MHStar{},
	})
}

// NewData .
func NewData(c *conf.Bootstrap, pg *gorm.DB, mc *minio.Client, logger log.Logger) (*Data, func(), error) {

	mLog := log.NewHelper(log.With(logger, "module", "mh/data"))
	cleanup := func() {
		mLog.Info("closing the data resources")
	}

	return &Data{
		pg: pg,
		mc: mc,
	}, cleanup, nil
}

func NewPostgres(c *conf.Bootstrap, logger log.Logger) (*gorm.DB, error) {
	return pgx.NewPostgres(c.GetLog().GetLevel(), c.GetData().GetPostgres().GetDsn(), logger)
}

func NewRedis(rootCtx context.Context, c *conf.Bootstrap, mLogger log.Logger) (*redis.ClusterClient, func(), error) {
	rc := c.GetData().GetRedisCluster()
	return friendly.NewRedisCluster(rootCtx, mLogger, rc.GetSeeds(), rc.GetPassword(), rc.GetReadOnly())
}
