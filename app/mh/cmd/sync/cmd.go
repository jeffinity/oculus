package sync

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/mh/cmd/common"
)

var flagConf string

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "同步最新漫画章节到存储与数据库",
		RunE: func(cmd *cobra.Command, args []string) error {
			uc, cleanup, err := common.BuildComicUseCase(flagConf)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := uc.Sync(cmd.Context()); err != nil {
				return err
			}
			log.Info("sync done")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagConf, "conf", "./config.yaml", "config path, eg: --conf ./config.yaml")
	return cmd
}
