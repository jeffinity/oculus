package server

import (
	"context"
	stdhttp "net/http"
	"net/url"

	"github.com/go-kratos/kratos/contrib/middleware/validate/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/metrics"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/jeffinity/singularity/kratosx"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jeffinity/oculus/app/squirrel/internal/conf"
	"github.com/jeffinity/oculus/app/squirrel/internal/service"
	healthv1 "github.com/jeffinity/oculus/pkg/health"
	squirrelv1 "github.com/jeffinity/oculus/proto/squirrel/v1"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Bootstrap, hs *service.HealthService, ss *service.SquirrelService, logger log.Logger) *khttp.Server {

	mLog := log.NewHelper(logger)
	var opts = []khttp.ServerOption{
		khttp.Middleware(
			middleware.Chain(
				recovery.Recovery(recovery.WithHandler(func(ctx context.Context, req, err any) error {
					mLog.Errorf("[Recovery] catch an err: %+v", err)
					return recovery.ErrUnknownRequest
				})),
				metrics.Server(
					metrics.WithSeconds(_metricSeconds),
					metrics.WithRequests(_metricRequests),
				),
				validate.ProtoValidate(),
				kratosx.ServerLogger(logger),
			),
		),
	}
	if c.GetServer().GetHttp().GetNetwork() != "" {
		opts = append(opts, khttp.Network(c.GetServer().GetHttp().Network))
	}
	if c.GetServer().GetHttp().GetAddr() != "" {
		opts = append(opts, khttp.Address(c.GetServer().GetHttp().Addr))
	}
	if c.GetServer().GetHttp().GetTimeout() != nil {
		opts = append(opts, khttp.Timeout(c.GetServer().GetHttp().Timeout.AsDuration()))
	}
	srv := khttp.NewServer(opts...)
	srv.Handle("/metrics", promhttp.Handler())
	srv.Route("/api/v1/squirrel/tasks").GET("/{task_id}/reconcile/export", func(ctx khttp.Context) error {
		taskID := ctx.Vars().Get("task_id")
		runID := ctx.Query().Get("run_id")
		content, filename, err := ss.ExportReconcileDetail(ctx, taskID, runID)
		if err != nil {
			return err
		}
		ctx.Response().Header().Set("Content-Disposition", buildAttachmentHeader(filename))
		return ctx.Blob(stdhttp.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
	})

	healthv1.RegisterHealthServiceHTTPServer(srv, hs)
	squirrelv1.RegisterSquirrelServiceHTTPServer(srv, ss)
	return srv
}

func buildAttachmentHeader(filename string) string {
	escaped := url.PathEscape(filename)
	return `attachment; filename="` + filename + `"; filename*=UTF-8''` + escaped
}
