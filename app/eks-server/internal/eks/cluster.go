package eks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/pkg/errors"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
)

type Cluster struct {
	name string
	envs map[string]*Env
}

type Env struct {
	name       string
	queryPool  *Pool
	deployPool *Pool
}

func NewCluster(c *conf.EKSCluster, logger *log.Helper) (*Cluster, error) {
	if err := validateJumpServer(c.GetJumpserver()); err != nil {
		return nil, err
	}
	if len(c.GetEnvs()) == 0 {
		return nil, errors.New("at least one eks env is required")
	}

	cluster := &Cluster{
		name: c.GetName(),
		envs: make(map[string]*Env, len(c.GetEnvs())),
	}
	for _, envConf := range c.GetEnvs() {
		if envConf.GetName() == "" {
			cluster.Close()
			return nil, errors.New("eks env name is required")
		}
		if _, ok := cluster.envs[envConf.GetName()]; ok {
			cluster.Close()
			return nil, fmt.Errorf("duplicate eks env: %s", envConf.GetName())
		}
		env, err := NewEnv(c.GetName(), c.GetJumpserver(), envConf, logger)
		if err != nil {
			cluster.Close()
			return nil, errors.WithMessagef(err, "init env %s", envConf.GetName())
		}
		cluster.envs[envConf.GetName()] = env
	}
	return cluster, nil
}

func validateJumpServer(c *conf.JumpServer) error {
	if c.GetHost() == "" {
		return errors.New("jumpserver host is required")
	}
	if c.GetUser() == "" {
		return errors.New("jumpserver user is required")
	}
	if c.GetIdentityFile() == "" {
		return errors.New("jumpserver identity_file is required")
	}
	return nil
}

func NewEnv(cluster string, jumpserver *conf.JumpServer, c *conf.EKSEnv, logger *log.Helper) (*Env, error) {
	if c.GetAsset() == "" {
		return nil, errors.New("env asset is required")
	}

	queryPool, err := NewPool(cluster, c.GetName(), PoolQuery, jumpserver, c, c.GetQueryPool(), logger)
	if err != nil {
		return nil, errors.WithMessage(err, "query pool")
	}
	deployPool, err := NewPool(cluster, c.GetName(), PoolDeploy, jumpserver, c, c.GetDeployPool(), logger)
	if err != nil {
		queryPool.Close()
		return nil, errors.WithMessage(err, "deploy pool")
	}

	return &Env{
		name:       c.GetName(),
		queryPool:  queryPool,
		deployPool: deployPool,
	}, nil
}

func (c *Cluster) Execute(ctx context.Context, req CommandRequest) (*CommandResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		return nil, errors.New("command is required")
	}
	if req.Env == "" {
		return nil, errors.New("env is required")
	}
	env, ok := c.envs[req.Env]
	if !ok {
		return nil, fmt.Errorf("unknown eks env: %s", req.Env)
	}
	req.Cluster = c.name
	return env.Execute(ctx, req)
}

func (e *Env) Execute(ctx context.Context, req CommandRequest) (*CommandResult, error) {
	req.Env = e.name
	switch req.Pool {
	case "", PoolQuery:
		req.Pool = PoolQuery
		return e.queryPool.Execute(ctx, req)
	case PoolDeploy:
		return e.deployPool.Execute(ctx, req)
	default:
		return nil, fmt.Errorf("unknown pool: %s", req.Pool)
	}
}

func (c *Cluster) Info() ClusterInfo {
	envs := make([]EnvInfo, 0, len(c.envs))
	for _, env := range c.envs {
		envs = append(envs, env.Info())
	}
	sort.Slice(envs, func(i, j int) bool {
		return envs[i].Name < envs[j].Name
	})
	return ClusterInfo{Name: c.name, Envs: envs}
}

func (e *Env) Info() EnvInfo {
	return EnvInfo{
		Name:   e.name,
		Query:  e.queryPool.Snapshot(),
		Deploy: e.deployPool.Snapshot(),
	}
}

func (c *Cluster) Close() {
	for _, env := range c.envs {
		env.Close()
	}
}

func (e *Env) Close() {
	e.queryPool.Close()
	e.deployPool.Close()
}

func durationOr(v time.Duration, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}
