package proof

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/mh/cmd/common"
)

var flagConf string

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proof",
		Short: "补全漫画元数据与封面（兼容旧 proof 任务名）",
		RunE: func(cmd *cobra.Command, args []string) error {
			uc, cleanup, err := common.BuildComicUseCase(flagConf)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := uc.Proof(cmd.Context()); err != nil {
				return err
			}
			log.Info("proof done")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagConf, "conf", "./config.yaml", "config path, eg: --conf ./config.yaml")
	return cmd
}
