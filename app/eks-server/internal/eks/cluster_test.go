package eks

import (
	"context"
	"strings"
	"testing"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
)

func TestNewClusterValidatesEnvConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *conf.EKSCluster
		want string
	}{
		{
			name: "missing envs",
			cfg:  baseCluster(nil),
			want: "at least one eks env is required",
		},
		{
			name: "duplicate env",
			cfg:  baseCluster([]*conf.EKSEnv{baseEnv("dev"), baseEnv("dev")}),
			want: "duplicate eks env: dev",
		},
		{
			name: "min greater than max",
			cfg:  baseCluster([]*conf.EKSEnv{baseEnvWithBadPool("dev")}),
			want: "query pool: query pool min_size cannot exceed max_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, err := NewCluster(tt.cfg, nil)
			if cluster != nil {
				cluster.Close()
			}
			if err == nil {
				t.Fatalf("NewCluster() error = nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewCluster() error = %q, want contains %q", err.Error(), tt.want)
			}
		})
	}
}

func TestClusterExecuteRoutesByEnv(t *testing.T) {
	cluster, err := NewCluster(baseCluster([]*conf.EKSEnv{baseEnv("dev"), baseEnv("test")}), nil)
	if err != nil {
		t.Fatalf("NewCluster() error = %v", err)
	}
	defer cluster.Close()

	for _, env := range cluster.envs {
		env.queryPool.sessionFactory = func(context.Context) (*Session, error) {
			return fakeSession(0, "ok"), nil
		}
	}

	result, err := cluster.Execute(context.Background(), CommandRequest{
		Env:     "test",
		Pool:    PoolQuery,
		Command: "kubectl get pods",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Cluster != "jumpserver-sg" || result.Env != "test" {
		t.Fatalf("Execute() routed to cluster=%q env=%q", result.Cluster, result.Env)
	}

	_, err = cluster.Execute(context.Background(), CommandRequest{Env: "missing", Command: "kubectl get pods"})
	if err == nil || !strings.Contains(err.Error(), "unknown eks env: missing") {
		t.Fatalf("Execute() unknown env error = %v", err)
	}
}

func baseCluster(envs []*conf.EKSEnv) *conf.EKSCluster {
	return &conf.EKSCluster{
		Name: "jumpserver-sg",
		Jumpserver: &conf.JumpServer{
			Host:         "jumpserver-sg.gainetics.io",
			User:         "jeff",
			IdentityFile: "~/.ssh/jump_rsa",
		},
		Envs: envs,
	}
}

func baseEnv(name string) *conf.EKSEnv {
	return &conf.EKSEnv{
		Name:       name,
		Asset:      "gainetics-eks-dev-cluster-admin",
		QueryPool:  &conf.Pool{MaxSize: 1},
		DeployPool: &conf.Pool{MaxSize: 1},
	}
}

func baseEnvWithBadPool(name string) *conf.EKSEnv {
	env := baseEnv(name)
	env.QueryPool.MinSize = 2
	env.QueryPool.MaxSize = 1
	return env
}
