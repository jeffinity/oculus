package server

import (
	"context"
	"time"

	validate "github.com/go-kratos/kratos/contrib/middleware/validate/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/metrics"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/jeffinity/singularity/kratosx"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/jeffinity/oculus/app/squirrel/internal/conf"
	"github.com/jeffinity/oculus/app/squirrel/internal/service"
	healthv1 "github.com/jeffinity/oculus/pkg/health"
	squirrelv1 "github.com/jeffinity/oculus/proto/squirrel/v1"
)

const maxMsgSize = 50 * 1024 * 1024

// NewGRPCServer new a gRPC server.
func NewGRPCServer(c *conf.Bootstrap, hs *service.HealthService, ss *service.SquirrelService, logger log.Logger) *grpc.Server {

	kaep := keepalive.EnforcementPolicy{
		MinTime:             20 * time.Second,
		PermitWithoutStream: true,
	}

	kasp := keepalive.ServerParameters{
		Time:    40 * time.Second,
		Timeout: 15 * time.Second,
	}

	mLog := log.NewHelper(logger)
	var opts = []grpc.ServerOption{
		grpc.Middleware(
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
		grpc.Options(
			ggrpc.KeepaliveEnforcementPolicy(kaep),
			ggrpc.KeepaliveParams(kasp),
			ggrpc.MaxRecvMsgSize(maxMsgSize),
			ggrpc.MaxSendMsgSize(maxMsgSize),
			ggrpc.MaxConcurrentStreams(1000),
		),
	}
	if c.GetServer().GetGrpc().GetNetwork() != "" {
		opts = append(opts, grpc.Network(c.GetServer().GetGrpc().Network))
	}
	if c.GetServer().GetGrpc().GetAddr() != "" {
		opts = append(opts, grpc.Address(c.GetServer().GetGrpc().Addr))
	}
	if c.GetServer().GetGrpc().GetTimeout() != nil {
		opts = append(opts, grpc.Timeout(c.GetServer().GetGrpc().Timeout.AsDuration()))
	}
	srv := grpc.NewServer(opts...)

	healthv1.RegisterHealthServiceServer(srv, hs)
	squirrelv1.RegisterSquirrelServiceServer(srv, ss)
	return srv
}
