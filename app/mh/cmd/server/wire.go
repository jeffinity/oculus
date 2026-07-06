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

	"github.com/jeffinity/oculus/app/mh/internal/app_init"
	"github.com/jeffinity/oculus/app/mh/internal/biz"
	"github.com/jeffinity/oculus/app/mh/internal/conf"
	"github.com/jeffinity/oculus/app/mh/internal/data"
	"github.com/jeffinity/oculus/app/mh/internal/server"
	"github.com/jeffinity/oculus/app/mh/internal/service"
)

// initApp init kratos application.
func initApp(pcID kratosx.ServiceID, root context.Context, c *conf.Bootstrap, wg *sync.WaitGroup, logger log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		newApp,
		app_init.NewNacosConf,
		data.ProviderSet,
		server.ProviderSet,
		biz.ProviderSet,
		service.ProviderSet,
		nacosx.ProviderSet,
	))
}
