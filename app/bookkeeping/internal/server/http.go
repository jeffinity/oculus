package server

import (
	"context"

	"github.com/go-kratos/kratos/contrib/middleware/validate/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/metrics"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/jeffinity/singularity/kratosx"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/conf"
	"github.com/jeffinity/oculus/app/bookkeeping/internal/service"
	healthv1 "github.com/jeffinity/oculus/pkg/health"
	bookkeepingv1 "github.com/jeffinity/oculus/proto/bookkeeping/v1"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Bootstrap, hs *service.HealthService, bs *service.BookkeepingService, logger log.Logger) *http.Server {

	mLog := log.NewHelper(logger)
	var opts = []http.ServerOption{
		http.Middleware(
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
		opts = append(opts, http.Network(c.GetServer().GetHttp().Network))
	}
	if c.GetServer().GetHttp().GetAddr() != "" {
		opts = append(opts, http.Address(c.GetServer().GetHttp().Addr))
	}
	if c.GetServer().GetHttp().GetTimeout() != nil {
		opts = append(opts, http.Timeout(c.GetServer().GetHttp().Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	srv.Handle("/metrics", promhttp.Handler())

	healthv1.RegisterHealthServiceHTTPServer(srv, hs)
	bookkeepingv1.RegisterBookkeepingServiceHTTPServer(srv, bs)
	return srv
}
