package test

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jeffinity/singularity/kratosx"
	"github.com/oklog/ulid/v2"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/app_init"
	"github.com/jeffinity/oculus/app/bookkeeping/internal/data"
)

type Resource struct {
	ctx    context.Context
	logger *log.Helper

	data *data.Data
}

func newTestResource(
	ctx context.Context,
	logger log.Logger,
	data *data.Data,
) *Resource {
	return &Resource{
		ctx:    ctx,
		data:   data,
		logger: log.NewHelper(logger),
	}
}

func getConfigPathEnv() string {
	return os.Getenv("APP_LAYOUT_CONFIG_PATH")
}

// InitTest 针对 data or biz 层依赖 conf 加载后实现的单测，可以执行 InitTest 来获取相关的资源进行测试
func InitTest() *Resource {

	configPath := getConfigPathEnv()
	bc, err := app_init.LoadConf(file.NewSource(configPath))
	if err != nil {
		log.Fatal(err)
	}

	bc, err = app_init.LoadNacosConf(bc)
	if err != nil {
		log.Fatal(err)
	}

	rootCtx := context.Background()
	logger, logCleanup, err := app_init.NewLogger(bc, kratosx.ServiceNameProbeCenter)
	if err != nil {
		log.Fatal(err)
	}
	defer logCleanup()

	wg := sync.WaitGroup{}
	pcID := fmt.Sprintf("pc-%s", ulid.Make().String())
	resource, cleanup, err := InitTestResource(kratosx.ServiceID(pcID), logger, rootCtx, &wg, bc)
	if err != nil {
		log.Fatal(err)
	}

	defer cleanup()
	return resource
}
