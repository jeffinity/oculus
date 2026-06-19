package eks

import "time"

type PoolKind string

const (
	PoolQuery  PoolKind = "query"
	PoolDeploy PoolKind = "deploy"
)

type CommandRequest struct {
	Cluster string
	Env     string
	Pool    PoolKind
	Command string
	Timeout time.Duration
}

type CommandResult struct {
	Cluster    string `json:"cluster"`
	Env        string `json:"env"`
	Pool       string `json:"pool"`
	Command    string `json:"command"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
}

type ClusterInfo struct {
	Name string    `json:"name"`
	Envs []EnvInfo `json:"envs"`
}

type EnvInfo struct {
	Name   string       `json:"name"`
	Query  PoolSnapshot `json:"query_pool"`
	Deploy PoolSnapshot `json:"deploy_pool"`
}

type PoolSnapshot struct {
	MinSize int `json:"min_size"`
	MaxSize int `json:"max_size"`
	Idle    int `json:"idle"`
	Active  int `json:"active"`
	Total   int `json:"total"`
}
