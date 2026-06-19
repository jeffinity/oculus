package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/pkg/errors"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
	"github.com/jeffinity/oculus/app/eks-server/internal/eks"
)

const protocolVersion = "2024-11-05"
const defaultMaxConcurrentRequests = 32

type Server struct {
	name           string
	version        string
	maxOutputBytes int
	sem            chan struct{}
	manager        *eks.Manager
	logger         *log.Helper
}

func NewServer(c *conf.Bootstrap, manager *eks.Manager, logger *log.Helper) *Server {
	name := c.GetMcp().GetServerName()
	if name == "" {
		name = "oculus-eks-server"
	}
	version := c.GetMcp().GetServerVersion()
	if version == "" {
		version = "v0.1.0"
	}
	maxOutputBytes := int(c.GetMcp().GetMaxOutputBytes())
	if maxOutputBytes <= 0 {
		maxOutputBytes = 256 * 1024
	}
	maxConcurrentRequests := int(c.GetMcp().GetMaxConcurrentRequests())
	if maxConcurrentRequests <= 0 {
		maxConcurrentRequests = defaultMaxConcurrentRequests
	}
	return &Server{
		name:           name,
		version:        version,
		maxOutputBytes: maxOutputBytes,
		sem:            make(chan struct{}, maxConcurrentRequests),
		manager:        manager,
		logger:         logger,
	}
}

func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	writer := newSafeWriter(w)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var wg sync.WaitGroup
	defer wg.Wait()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = writer.write(response{JSONRPC: "2.0", Error: newError(-32700, err.Error())})
			continue
		}
		if req.ID == nil {
			continue
		}
		select {
		case s.sem <- struct{}{}:
		default:
			_ = writer.write(response{JSONRPC: "2.0", ID: req.ID, Error: newError(-32001, "server is busy")})
			continue
		}

		wg.Add(1)
		go func() {
			defer func() {
				<-s.sem
				wg.Done()
			}()
			s.handle(ctx, writer, req)
		}()
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, writer *safeWriter, req request) {
	result, rpcErr := s.dispatch(ctx, req)
	resp := response{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	if err := writer.write(resp); err != nil && s.logger != nil {
		s.logger.Errorf("write MCP response failed: %+v", err)
	}
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *responseError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools()}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	default:
		return nil, newError(-32601, "method not found: "+req.Method)
	}
}

func (s *Server) tools() []tool {
	return []tool{
		{
			Name:        "eks_list_clusters",
			Description: "List configured logical EKS clusters, environments, and pool status.",
			InputSchema: objectSchema(map[string]schemaProperty{}),
		},
		{
			Name:        "eks_query",
			Description: "Run a lightweight kubectl/helm command through the query pool.",
			InputSchema: objectSchema(map[string]schemaProperty{
				"cluster": stringProp("Logical cluster name, for example jumpserver-sg."),
				"env":     stringProp("Logical environment name, for example dev or test."),
				"command": stringProp("Single-line command, for example kubectl -n scloud-dnps-dev get pods -o wide."),
				"timeout_seconds": {
					Type:        "number",
					Description: "Optional command timeout in seconds.",
				},
			}, "cluster", "env", "command"),
		},
		{
			Name:        "eks_deploy",
			Description: "Run a deployment or upgrade command through the heavy-task deploy pool.",
			InputSchema: objectSchema(map[string]schemaProperty{
				"cluster": stringProp("Logical cluster name, for example jumpserver-sg."),
				"env":     stringProp("Logical environment name, for example dev or test."),
				"command": stringProp("Single-line deployment command, for example helm upgrade --install ..."),
				"timeout_seconds": {
					Type:        "number",
					Description: "Optional command timeout in seconds.",
				},
			}, "cluster", "env", "command"),
		},
	}
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (any, *responseError) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, newError(-32602, err.Error())
	}

	switch params.Name {
	case "eks_list_clusters":
		return toolResultJSON(s.manager.Clusters(), false), nil
	case "eks_query":
		return s.runCommandTool(ctx, params.Arguments, eks.PoolQuery)
	case "eks_deploy":
		return s.runCommandTool(ctx, params.Arguments, eks.PoolDeploy)
	default:
		return nil, newError(-32602, "unknown tool: "+params.Name)
	}
}

func (s *Server) runCommandTool(ctx context.Context, raw json.RawMessage, pool eks.PoolKind) (any, *responseError) {
	var args commandArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, newError(-32602, err.Error())
	}
	if args.Cluster == "" || args.Env == "" || args.Command == "" {
		return nil, newError(-32602, "cluster, env and command are required")
	}

	req := eks.CommandRequest{
		Cluster: args.Cluster,
		Env:     args.Env,
		Pool:    pool,
		Command: args.Command,
		Timeout: seconds(args.TimeoutSeconds),
	}
	result, err := s.manager.Execute(ctx, req)
	if result != nil {
		result.Output, result.Truncated = truncate(result.Output, s.maxOutputBytes)
	}
	if err != nil {
		if result != nil {
			return toolResultJSON(map[string]any{
				"error":  err.Error(),
				"result": result,
			}, true), nil
		}
		return toolResultText(err.Error(), true), nil
	}
	return toolResultJSON(result, result.ExitCode != 0), nil
}

func seconds(v float64) time.Duration {
	if v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

func truncate(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end], true
}

func toolResultText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}
}

func toolResultJSON(v any, isError bool) map[string]any {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolResultText(fmt.Sprintf("marshal result: %+v", err), true)
	}
	return toolResultText(string(data), isError)
}

func objectSchema(properties map[string]schemaProperty, required ...string) inputSchema {
	return inputSchema{
		Type:                 "object",
		Properties:           properties,
		Required:             required,
		AdditionalProperties: false,
	}
}

func stringProp(desc string) schemaProperty {
	return schemaProperty{Type: "string", Description: desc}
}

func wrapRPCError(err error) *responseError {
	if err == nil {
		return nil
	}
	return newError(-32000, err.Error())
}

var _ = wrapRPCError(errors.New("compile-time guard"))
