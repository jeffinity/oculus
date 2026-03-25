package migrate

import (
	"context"
	"os"
	"sort"

	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jeffinity/singularity/migratex"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/jeffinity/oculus/app/squirrel/internal/app_init"
)

var (
	flagConf  string
	flagTable string
)

func CmdMigrate() *cobra.Command {
	root := &cobra.Command{
		Use:   "migrate",
		Short: "数据库迁移工具（支持列出、单表、全量迁移）",
	}
	root.PersistentFlags().StringVar(&flagConf, "conf", "./config.yaml", "配置文件路径，例如：--conf ./config.yaml")
	root.AddCommand(newAllCmd(), newLsCmd(), newTableCmd())
	return root
}

func newAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "all",
		Short: "迁移所有的数据表",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, cleanup, logger, err := buildMigrator()
			if err != nil {
				return err
			}
			defer cleanup()

			hl := log.NewHelper(logger)
			if m == nil || m.core == nil {
				hl.Warn("不支持 migrate")
				return nil
			}

			hl.Infof("开始迁移所有表 ...")
			if err := m.MigrateAll(); err != nil {
				hl.Errorf("迁移失败: %+v", err)
				return errors.WithStack(err)
			}
			hl.Infof("迁移完成")
			return nil
		},
	}
}

func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "列出所有可迁移的数据表",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, cleanup, logger, err := buildMigrator()
			if err != nil {
				return err
			}
			defer cleanup()

			hl := log.NewHelper(logger)
			if m == nil || m.core == nil {
				hl.Warn("不支持 migrate")
				return nil
			}

			tbs := m.ListTables()
			sort.Strings(tbs)
			if len(tbs) == 0 {
				hl.Warn("没有可迁移的数据表")
				return nil
			}
			printTableList(tbs)
			return nil
		},
	}
}

func newTableCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "table",
		Short: "迁移指定的数据表",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagTable == "" {
				_ = cmd.Usage()
				return errors.New("必须使用 -t/--table 指定表名")
			}

			m, cleanup, logger, err := buildMigrator()
			if err != nil {
				return err
			}
			defer cleanup()

			hl := log.NewHelper(logger)
			if m == nil || m.core == nil {
				hl.Warn("不支持 migrate")
				return nil
			}

			hl.Infof("开始迁移表: %s ...", flagTable)
			if err := m.MigrateOne(flagTable); err != nil {
				hl.Errorf("迁移表失败: %+v", err)
				return errors.WithStack(err)
			}
			hl.Infof("迁移表完成: %s", flagTable)
			return nil
		},
	}
	c.Flags().StringVarP(&flagTable, "table", "t", "", "要迁移的表名（必填）")
	return c
}

func buildMigrator() (*CmdMigrator, func(), log.Logger, error) {

	bc, err := app_init.LoadConf(file.NewSource(flagConf))
	if err != nil {
		return nil, func() {}, nil, errors.Wrap(err, "加载配置失败")
	}
	bc, err = app_init.LoadNacosConf(bc)
	if err != nil {
		return nil, func() {}, nil, errors.Wrap(err, "加载 Nacos 配置失败")
	}

	logger, logCleanup, err := app_init.NewLogger(bc, "")
	if err != nil {
		return nil, func() {}, nil, errors.Wrap(err, "初始化日志失败")
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	m, migCleanup, err := initMigrator(rootCtx, bc, logger)
	if err != nil {
		logCleanup()
		cancel()
		return nil, func() {}, nil, err
	}

	cleanup := func() {
		migCleanup()
		logCleanup()
		cancel()
	}
	return m, cleanup, logger, nil
}

type CmdMigrator struct {
	ctx  context.Context
	core *migratex.Migrator
}

func NewMigrator(ctx context.Context, m *migratex.Migrator) *CmdMigrator {
	return &CmdMigrator{ctx: ctx, core: m}
}

func (m *CmdMigrator) MigrateAll() error { return errors.WithStack(m.core.MigrateAll(m.ctx)) }

func (m *CmdMigrator) ListTables() []string { return m.core.ListTables() }

func (m *CmdMigrator) MigrateOne(tbName string) error {
	return errors.WithStack(m.core.MigrateOne(m.ctx, tbName))
}

func printTableList(tables []string) {
	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.AppendHeader(table.Row{"序号", "表名"})
	for i, tb := range tables {
		tw.AppendRow(table.Row{i + 1, tb})
	}
	tw.Render()
}
