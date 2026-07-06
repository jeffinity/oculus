package data

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jeffinity/singularity/pgx"

	"github.com/jeffinity/oculus/app/squirrel/internal/biz"
)

func NewHelloRepo(data *Data, logger log.Logger) biz.HelloRepo {
	return &helloRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "squirrel/helloRepo")),
	}
}

type helloRepo struct {
	data *Data
	log  *log.Helper
}

func (h *helloRepo) Save(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}

func (h *helloRepo) Update(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}

func (h *helloRepo) ListAll(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}

type Hello struct {
	pgx.BaseModel

	Name string `gorm:"column:name;type:varchar(64);comment:注册节点ID" json:"name"`
}

func (*Hello) TableName() string { return "hellos" }
