package mcp

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/eks-server/internal/app_init"
	"github.com/jeffinity/oculus/app/eks-server/internal/eks"
	mcpserver "github.com/jeffinity/oculus/app/eks-server/internal/mcp"
)

var flagConf string

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run EKS MCP server over stdio",
		Run: func(cmd *cobra.Command, args []string) {
			runMCP()
		},
	}

	cmd.Flags().StringVar(&flagConf, "conf", "./config.yaml", "config path, eg: --conf config.yaml")
	return cmd
}

func runMCP() {
	log.SetLogger(log.NewStdLogger(os.Stderr))

	bc, err := app_init.LoadConf(file.NewSource(flagConf))
	if err != nil {
		log.Errorf("Load config failed: %+v", err)
		return
	}

	logger, cleanup, err := app_init.NewLogger(bc, "eks-server")
	if err != nil {
		log.Errorf("Init logger failed: %+v", err)
		return
	}
	defer cleanup()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	manager, err := eks.NewManager(bc, log.NewHelper(logger))
	if err != nil {
		log.NewHelper(logger).Errorf("Init EKS manager failed: %+v", err)
		return
	}
	defer manager.Close()

	srv := mcpserver.NewServer(bc, manager, log.NewHelper(logger))
	if err := srv.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		log.NewHelper(logger).Errorf("MCP server stopped: %+v", err)
	}
}
