package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/jeffinity/singularity/buildinfo"
	"github.com/jeffinity/singularity/friendly"
	"github.com/jeffinity/singularity/kratosx"
	"github.com/jeffinity/singularity/nacosx"
	"github.com/jeffinity/singularity/pprof"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/encoding"

	"github.com/jeffinity/oculus/app/squirrel/internal/app_init"
	"github.com/jeffinity/oculus/app/squirrel/internal/conf"
)

var (
	ServiceName = kratosx.ServiceNameAppLayout

	flagConf string
)

func Command() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run gRPC & HTTP server",
		Run: func(cmd *cobra.Command, args []string) {
			runServer()
		},
	}

	cmd.Flags().StringVar(&flagConf, "conf", "./config.yaml", "config path, eg: --conf config.yaml")
	return cmd
}

func LoadFileConf() (*conf.Bootstrap, error) {
	return app_init.LoadConf(file.NewSource(flagConf))
}

func runServer() {

	bc, err := LoadFileConf()
	if err != nil {
		log.Errorf("Load config failed: %+v", err)
		return
	}

	bc, err = app_init.LoadNacosConf(bc)
	if err != nil {
		log.Errorf("Load nacos config failed: %+v", err)
		return
	}

	logger, logCleanup, err := app_init.NewLogger(bc, ServiceName)
	if err != nil {
		log.Errorf("Init logger failed: %+v", err)
		return
	}
	defer logCleanup()

	mLog := log.NewHelper(logger)
	rootCtx, cancel := context.WithCancel(context.Background())
	mLog.Infof("Init app...")
	if strings.ToUpper(bc.Log.Level) == "DEBUG" {
		pprof.ListenInBackground(mLog)
	}

	wg := new(sync.WaitGroup)
	encoding.RegisterCodec(kratosx.Codec)
	pcID := fmt.Sprintf("pc-%s", ulid.Make())
	app, cleanup, err := initApp(kratosx.ServiceID(pcID), rootCtx, bc, wg, logger)
	if err != nil {
		mLog.Errorf("Init app failed: %+v", err)
		cancel()
		return
	}

	defer cleanup()
	if err := app.Run(); err != nil {
		mLog.Errorf("App run failed: %+v", err)
	}
	defer func() {
		if e := recover(); e != nil {
			mLog.Errorf("app panic: %+v", e)
		}
	}()
	mLog.Infof("Exiting app & waiting for all goroutines to exit")
	cancel()
	wg.Wait()
	mLog.Infof("Exited & Bye-bye")
}

func newApp(
	pcID kratosx.ServiceID,
	logger log.Logger,
	c *conf.Bootstrap,
	gs *grpc.Server,
	hs *http.Server,
	nr *nacosx.Registry,
) (*kratos.App, error) {

	endpoints, err := kratosx.ParseEndpoints(c.GetServer().GetHttp(), c.GetServer().GetGrpc())
	if err != nil {
		return nil, err
	}

	opts := []kratos.Option{
		kratos.Endpoint(endpoints...),
		kratos.ID(string(pcID)),
		kratos.Name(ServiceName),
		kratos.Version(buildinfo.Version),
		kratos.Metadata(map[string]string{
			"pc_id":  string(pcID),
			"weight": fmt.Sprintf("%d", friendly.GetOrDefault(c.GetNacos().GetWeight(), 100)),
		}),
		kratos.Logger(logger),
		kratos.Server(
			gs,
			hs,
		),
	}

	if nr != nil {
		opts = append(opts, kratos.Registrar(nr))
	}
	return kratos.New(opts...), nil
}
