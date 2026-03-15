package main

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/mh/cmd/migrate"
	"github.com/jeffinity/oculus/app/mh/cmd/proof"
	"github.com/jeffinity/oculus/app/mh/cmd/proot"
	"github.com/jeffinity/oculus/app/mh/cmd/reconcile"
	"github.com/jeffinity/oculus/app/mh/cmd/server"
	"github.com/jeffinity/oculus/app/mh/cmd/sync"
)

var rootCmd = &cobra.Command{
	Use: "mh",
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(server.Command())
	rootCmd.AddCommand(sync.Command())
	rootCmd.AddCommand(proof.Command())
	rootCmd.AddCommand(proot.Command())
	rootCmd.AddCommand(reconcile.Command())
	rootCmd.AddCommand(CmdVersion())
	rootCmd.AddCommand(migrate.CmdMigrate())
}

func main() {

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if cmd.Name() != "version" {
			ShowInfo()
		}
	}

	err := rootCmd.Execute()
	if err != nil {
		log.Error(err)
		return
	}
}
