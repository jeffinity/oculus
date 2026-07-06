package reconcile

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/mh/cmd/common"
)

var (
	flagConf       string
	flagStartOrgID int64
	flagEndOrgID   int64
	flagLimit      int
	flagDryRun     bool
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile-books",
		Short: "从 latest 回填缺失的 books 记录",
		RunE: func(cmd *cobra.Command, args []string) error {
			uc, cleanup, err := common.BuildComicUseCase(flagConf)
			if err != nil {
				return err
			}
			defer cleanup()

			total, created, err := uc.ReconcileBooksFromLatest(cmd.Context(), flagStartOrgID, flagEndOrgID, flagLimit, flagDryRun)
			if err != nil {
				return err
			}
			log.Infof("reconcile-books done: total_candidates=%d created=%d dry_run=%v", total, created, flagDryRun)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagConf, "conf", "./config.yaml", "config path, eg: --conf ./config.yaml")
	cmd.Flags().Int64Var(&flagStartOrgID, "start-org-id", 0, "inclusive org_id lower bound, 0 means no lower bound")
	cmd.Flags().Int64Var(&flagEndOrgID, "end-org-id", 0, "inclusive org_id upper bound, 0 means no upper bound")
	cmd.Flags().IntVar(&flagLimit, "limit", 200, "maximum missing books to process in one run")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "only print candidates without writing books")
	return cmd
}
