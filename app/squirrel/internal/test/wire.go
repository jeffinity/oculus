//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.
//
//go:generate wire
package test

import (
	"context"
	"sync"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jeffinity/singularity/kratosx"

	"github.com/jeffinity/oculus/app/squirrel/internal/biz"
	"github.com/jeffinity/oculus/app/squirrel/internal/conf"
	"github.com/jeffinity/oculus/app/squirrel/internal/data"
)

func InitTestResource(pcID kratosx.ServiceID, logger log.Logger, root context.Context, wg *sync.WaitGroup, c *conf.Bootstrap) (*Resource, func(), error) {
	panic(wire.Build(
		newTestResource,
		//app_init.NewNacosConf,
		data.ProviderSet,
		biz.ProviderSet,
	))
}
