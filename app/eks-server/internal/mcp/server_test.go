package mcp

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
	"github.com/jeffinity/oculus/app/eks-server/internal/eks"
)

func TestServerServeInitializeAndToolsList(t *testing.T) {
	manager, err := eks.NewManager(&conf.Bootstrap{
		Eks: []*conf.EKSCluster{
			{
				Name: "jumpserver-sg",
				Jumpserver: &conf.JumpServer{
					Host:         "jumpserver-sg.gainetics.io",
					User:         "jeff",
					IdentityFile: "~/.ssh/jump_rsa",
				},
				Envs: []*conf.EKSEnv{
					{
						Name:  "dev",
						Asset: "gainetics-eks-dev-cluster-admin",
						QueryPool: &conf.Pool{
							MaxSize: 1,
						},
						DeployPool: &conf.Pool{MaxSize: 1},
					},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()

	server := NewServer(&conf.Bootstrap{}, manager, nil)
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n",
	)
	var output bytes.Buffer

	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, `"protocolVersion"`) {
		t.Fatalf("output does not contain initialize response: %s", got)
	}
	if !strings.Contains(got, `"eks_query"`) {
		t.Fatalf("output does not contain tools response: %s", got)
	}
	if !strings.Contains(got, `"env"`) {
		t.Fatalf("output does not contain env schema: %s", got)
	}
}

func TestTruncatePreservesUTF8(t *testing.T) {
	got, truncated := truncate("状态正常", len("状")+1)
	if !truncated {
		t.Fatalf("truncate() truncated = false")
	}
	if got != "状" {
		t.Fatalf("truncate() = %q, want %q", got, "状")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncate() returned invalid UTF-8")
	}
}
