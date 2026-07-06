package biz

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
)

type HelloRepo interface {
	Save(context.Context) error
	Update(context.Context) error
	ListAll(context.Context) error
}

type HelloUseCase struct {
	repo HelloRepo
	log  *log.Helper
}

func NewHelloUseCase(hr HelloRepo, logger log.Logger) *HelloUseCase {
	return &HelloUseCase{
		repo: hr,
		log:  log.NewHelper(log.With(logger, "module", "squirrel/HelloUseCase")),
	}
}
