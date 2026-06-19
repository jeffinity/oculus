package main

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	mcpcmd "github.com/jeffinity/oculus/app/eks-server/cmd/mcp"
)

var rootCmd = &cobra.Command{
	Use: "eks-server",
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(mcpcmd.Command())
	rootCmd.AddCommand(CmdVersion())
}

func main() {
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if cmd.Name() != "version" && cmd.Name() != "mcp" {
			ShowInfo()
		}
	}

	err := rootCmd.Execute()
	if err != nil {
		log.Error(err)
		return
	}
}
