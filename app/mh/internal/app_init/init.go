package app_init

import (
	"path"
	"strings"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jeffinity/singularity/logx"
	"github.com/jeffinity/singularity/nacosx"
	"github.com/jeffinity/singularity/set"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/jeffinity/oculus/app/mh/internal/conf"
)

func LoadConf(s ...config.Source) (*conf.Bootstrap, error) {
	c := config.New(
		config.WithSource(
			s...,
		),
		config.WithDecoder(func(kv *config.KeyValue, v map[string]interface{}) error {
			return yaml.Unmarshal(kv.Value, v)
		}),
	)
	if err := c.Load(); err != nil {
		return nil, errors.WithStack(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		return nil, errors.WithStack(err)
	}
	return &bc, nil
}

func LoadNacosConf(bc *conf.Bootstrap) (*conf.Bootstrap, error) {

	if bc.GetNacos().GetAddr() == "" || bc.GetNacos().GetPort() == 0 {
		return bc, nil // 未配置 nacos
	}

	dataIds := set.New[string]()
	if bc.GetNacos().GetDataId() != "" {
		dataIds.Insert(bc.GetNacos().GetDataId())
	}
	for _, did := range bc.GetNacos().GetDataIds() {
		if did != "" {
			dataIds.Insert(did)
		}
	}

	nc := NewNacosConf(bc)
	cc, err := nacosx.NewConfigClient(nc)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var sources []config.Source
	for _, dataId := range dataIds.All() {
		cfg := nc
		cfg.DataId = dataId
		src, err := nacosx.NewNacosConfigSource(cfg, cc)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		sources = append(sources, src)
	}

	nbc, err := LoadConf(sources...)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	nbc.Nacos = bc.Nacos
	return nbc, nil
}

func NewNacosConf(bc *conf.Bootstrap) nacosx.Conf {
	return nacosx.Conf{
		Addr:        bc.GetNacos().GetAddr(),
		Port:        bc.GetNacos().GetPort(),
		Username:    bc.GetNacos().GetUsername(),
		Password:    bc.GetNacos().GetPassword(),
		LogDir:      bc.GetNacos().GetLogDir(),
		ClusterName: bc.GetNacos().GetClusterName(),
		NamespaceId: bc.GetNacos().GetNamespaceId(),
		GroupId:     bc.GetNacos().GetGroupId(),
		DataId:      bc.GetNacos().GetDataId(),
	}
}

func NewLogger(c *conf.Bootstrap, serviceName string) (log.Logger, func(), error) {

	if serviceName == "" {
		return logx.New(logx.Options{
			Level:        parseLevel(c.GetLog().GetLevel()),
			BaseFilename: "",
		})
	}

	fn := c.GetLog().GetLogFilename()
	if fn == "" {
		fn = serviceName + ".log"
	}

	logDir := c.GetLog().GetLogDir()
	if logDir == "" {
		logDir = "."
	}

	return logx.New(logx.Options{
		Level:              parseLevel(c.GetLog().GetLevel()),
		BaseFilename:       path.Join(logDir, fn),
		MaxSizeBytes:       int64(c.GetLog().GetMaxSizeMb()) * 1024 * 1024,
		MaxBackups:         int(c.GetLog().GetMaxBackups()),
		Compress:           c.GetLog().GetCompress(),
		ForceDailyRollover: c.GetLog().GetRotateDaily(),
	})
}

func parseLevel(lvl string) log.Level {
	switch strings.ToUpper(lvl) {
	case "DEBUG":
		return log.LevelDebug
	case "WARNING", "WARN":
		return log.LevelWarn
	case "ERROR":
		return log.LevelError
	default:
		return log.LevelInfo
	}
}
