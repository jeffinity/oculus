//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.
//
//go:generate wire
package server

import (
	"context"
	"sync"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jeffinity/singularity/kratosx"
	"github.com/jeffinity/singularity/nacosx"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/app_init"
	"github.com/jeffinity/oculus/app/bookkeeping/internal/conf"
	"github.com/jeffinity/oculus/app/bookkeeping/internal/data"
	"github.com/jeffinity/oculus/app/bookkeeping/internal/server"
	"github.com/jeffinity/oculus/app/bookkeeping/internal/service"
)

// initApp init kratos application.
func initApp(pcID kratosx.ServiceID, root context.Context, c *conf.Bootstrap, wg *sync.WaitGroup, logger log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		newApp,
		app_init.NewNacosConf,
		data.ProviderSet,
		data.AssetProviderSet,
		server.ProviderSet,
		service.ProviderSet,
		nacosx.ProviderSet,
	))
}
