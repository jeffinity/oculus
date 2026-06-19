package eks

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/pkg/errors"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
)

type Manager struct {
	clusters map[string]*Cluster
}

func NewManager(c *conf.Bootstrap, logger *log.Helper) (*Manager, error) {
	m := &Manager{clusters: make(map[string]*Cluster, len(c.GetEks()))}
	for _, clusterConf := range c.GetEks() {
		if clusterConf.GetName() == "" {
			return nil, errors.New("eks cluster name is required")
		}
		if _, ok := m.clusters[clusterConf.GetName()]; ok {
			return nil, fmt.Errorf("duplicate eks cluster: %s", clusterConf.GetName())
		}
		cluster, err := NewCluster(clusterConf, logger)
		if err != nil {
			return nil, errors.WithMessagef(err, "init cluster %s", clusterConf.GetName())
		}
		m.clusters[clusterConf.GetName()] = cluster
	}
	if len(m.clusters) == 0 {
		return nil, errors.New("at least one eks cluster is required")
	}
	return m, nil
}

func (m *Manager) Execute(ctx context.Context, req CommandRequest) (*CommandResult, error) {
	cluster, ok := m.clusters[req.Cluster]
	if !ok {
		return nil, fmt.Errorf("unknown eks cluster: %s", req.Cluster)
	}
	return cluster.Execute(ctx, req)
}

func (m *Manager) Clusters() []ClusterInfo {
	out := make([]ClusterInfo, 0, len(m.clusters))
	for _, cluster := range m.clusters {
		out = append(out, cluster.Info())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (m *Manager) Close() {
	for _, cluster := range m.clusters {
		cluster.Close()
	}
}
