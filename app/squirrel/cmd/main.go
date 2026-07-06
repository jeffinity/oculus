package main

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/squirrel/cmd/migrate"
	"github.com/jeffinity/oculus/app/squirrel/cmd/server"
)

var rootCmd = &cobra.Command{
	Use: "squirrel",
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(server.Command())
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
