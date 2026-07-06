package main

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jeffinity/singularity/buildinfo"
	"github.com/spf13/cobra"
)

func CmdVersion() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			ShowInfo()
		},
	}
	return cmd
}

func ShowInfo() {

	log.Infof("built with %s[OS: %s, arch: %s] from (version: %s, buildTime: %s, commitID: %s) by %s",
		buildinfo.GoVersion, buildinfo.BuildOS, buildinfo.GoArch, buildinfo.Version,
		buildinfo.BuildTime, buildinfo.CommitID, buildinfo.BuildUser)
}
