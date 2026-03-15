//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.
//
//go:generate wire
package migrate

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"

	"github.com/jeffinity/oculus/app/mh/internal/conf"
	"github.com/jeffinity/oculus/app/mh/internal/data"
)

// initMigrator init db migrator.
func initMigrator(root context.Context, c *conf.Bootstrap, logger log.Logger) (*CmdMigrator, func(), error) {
	panic(wire.Build(NewMigrator, data.ProviderSet))
}
