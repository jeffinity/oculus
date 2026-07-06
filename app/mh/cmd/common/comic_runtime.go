package common

import (
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"

	"github.com/jeffinity/oculus/app/mh/internal/app_init"
	"github.com/jeffinity/oculus/app/mh/internal/biz"
	"github.com/jeffinity/oculus/app/mh/internal/data"
)

func BuildComicUseCase(confPath string) (*biz.ComicUseCase, func(), error) {
	bc, err := app_init.LoadConf(file.NewSource(confPath))
	if err != nil {
		return nil, nil, err
	}
	bc, err = app_init.LoadNacosConf(bc)
	if err != nil {
		return nil, nil, err
	}

	logger, cleanupLog, err := app_init.NewLogger(bc, "")
	if err != nil {
		return nil, nil, err
	}

	pg, err := data.NewPostgres(bc, logger)
	if err != nil {
		cleanupLog()
		return nil, nil, err
	}
	mc, err := data.NewMinio(bc)
	if err != nil {
		cleanupLog()
		return nil, nil, err
	}
	repo := data.NewComicRepoWithDeps(pg, mc, bc, logger)
	uc := biz.NewComicUseCase(repo, bc, logger)

	cleanup := func() {
		sqlDB, e := pg.DB()
		if e == nil {
			_ = sqlDB.Close()
		}
		cleanupLog()
	}
	log.NewHelper(logger).Info("mh runtime ready")
	return uc, cleanup, nil
}
