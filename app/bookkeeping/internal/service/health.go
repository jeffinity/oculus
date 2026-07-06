package service

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jeffinity/singularity/buildinfo"
	"github.com/jeffinity/singularity/kratosx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/conf"
	healthv1 "github.com/jeffinity/oculus/pkg/health"
)

var (
	startTime = time.Now()
)

const (
	checkTimeout        = 800 * time.Millisecond // 单项检查超时
	kafkaDialTimeout    = 700 * time.Millisecond // Kafka 握手超时
	kafkaControllerWait = 500 * time.Millisecond // 调 Controller() 的超时
)

type HealthService struct {
	healthv1.UnimplementedHealthServiceServer

	logger *log.Helper
}

func NewHealthService(
	logger log.Logger,
	_ *conf.Bootstrap,
) *HealthService {

	return &HealthService{
		logger: log.NewHelper(log.With(logger, "module", "bookkeeping/HealthService")),
	}
}

func (s *HealthService) Liveness(_ context.Context, _ *healthv1.HealthCheckRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (s *HealthService) Readiness(ctx context.Context, _ *healthv1.HealthCheckRequest) (*emptypb.Empty, error) {
	var errs []string

	// TODO add service availability check
	//if err := s.checkPostgres(ctx); err != nil {
	//	errs = append(errs, "postgres: "+err.Error())
	//}
	//if err := s.checkRedis(ctx); err != nil {
	//	errs = append(errs, "redis: "+err.Error())
	//}
	//if err := s.checkKafka(ctx); err != nil {
	//	errs = append(errs, "kafka: "+err.Error())
	//}

	if len(errs) > 0 {
		s.logger.Errorf("readiness not ready: %s", strings.Join(errs, "; "))
		return nil, status.Error(codes.Unavailable, strings.Join(errs, "; "))
	}
	return &emptypb.Empty{}, nil
}

func (s *HealthService) Status(ctx context.Context, _ *healthv1.StatusRequest) (*healthv1.StatusReply, error) {
	now := time.Now()

	type item struct {
		name   string
		checkF func(context.Context) error
	}

	items := []item{
		//{name: "postgres", checkF: s.checkPostgres},
		//{name: "redis", checkF: s.checkRedis},
		//{name: "kafka", checkF: s.checkKafka},
	}

	var checks []*healthv1.Check
	overall := healthv1.Status_STATUS_UP

	for _, it := range items {
		start := time.Now()
		ctxi, cancel := context.WithTimeout(ctx, checkTimeout)
		err := it.checkF(ctxi)
		cancel()

		chk := &healthv1.Check{
			Name:      it.name,
			Status:    healthv1.Status_STATUS_UP,
			Reason:    "",
			LatencyMs: int64(time.Since(start) / time.Millisecond),
			Since:     timestamppb.New(startTime),
			Metadata:  map[string]string{},
		}
		if err != nil {
			chk.Status = healthv1.Status_STATUS_DOWN
			chk.Reason = err.Error()
			overall = healthv1.Status_STATUS_DOWN
		}
		checks = append(checks, chk)
	}

	reply := &healthv1.StatusReply{
		Overall: overall,
		Service: kratosx.ServiceNameProbeCenter,
		VersionInfo: &healthv1.Version{
			Version:   buildinfo.Version,
			BuildTime: buildinfo.BuildTime,
			BuildUser: buildinfo.BuildUser,
			CommitId:  buildinfo.CommitID,
			GoVersion: buildinfo.GoVersion,
			GoArch:    buildinfo.GoArch,
			BuildOs:   buildinfo.BuildOS,
		},
		UptimeSeconds: int64(time.Since(startTime).Seconds()),
		Now:           timestamppb.New(now),
		Checks:        checks,
	}

	return reply, nil
}

//func (s *HealthService) checkPostgres(ctx context.Context) error {
//	if s.data == nil || s.data.Pg == nil {
//		return errors.New("gorm db is nil")
//	}
//	sqlDB, err := s.data.Pg.DB()
//	if err != nil {
//		return fmt.Errorf("get sqldb: %w", err)
//	}
//	if err := sqlDB.PingContext(ctx); err != nil {
//		return fmt.Errorf("ping: %w", err)
//	}
//	return nil
//}
//
//func (s *HealthService) checkRedis(ctx context.Context) error {
//	if s.data == nil || s.data.RDB == nil {
//		return errors.New("redis client is nil")
//	}
//	if err := s.data.RDB.Ping(ctx).Err(); err != nil {
//		return fmt.Errorf("ping: %w", err)
//	}
//	return nil
//}
//
//func (s *HealthService) checkKafka(ctx context.Context) error {
//	if len(s.kafkaBrokers) == 0 {
//		return errors.New("no kafka brokers provided")
//	}
//
//	var lastErr error
//	for _, addr := range s.kafkaBrokers {
//		if err := tcpPing(ctx, addr, kafkaDialTimeout); err != nil {
//			lastErr = fmt.Errorf("tcp dial %s: %w", addr, err)
//			continue
//		}
//
//		var conn *kafka.Conn
//		var err error
//
//		dctx, cancel := context.WithTimeout(ctx, kafkaDialTimeout)
//		conn, err = kafka.DialContext(dctx, "tcp", addr)
//		cancel()
//
//		if err != nil {
//			lastErr = fmt.Errorf("kafka dial %s: %w", addr, err)
//			continue
//		}
//
//		_ = conn.SetDeadline(time.Now().Add(kafkaControllerWait))
//		_, err = conn.Controller()
//		_ = conn.Close()
//
//		if err != nil {
//			lastErr = fmt.Errorf("controller query %s: %w", addr, err)
//			continue
//		}
//		return nil
//	}
//
//	if lastErr == nil {
//		lastErr = errors.New("unknown kafka error")
//	}
//	return lastErr
//}

func tcpPing(ctx context.Context, addr string, timeout time.Duration) error {
	var d net.Dialer
	if timeout > 0 {
		d.Timeout = timeout
	}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}
