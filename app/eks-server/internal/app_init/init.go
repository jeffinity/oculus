package app_init

import (
	"path"
	"strings"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jeffinity/singularity/logx"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
)

func LoadConf(s ...config.Source) (*conf.Bootstrap, error) {
	c := config.New(
		config.WithSource(s...),
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

func NewLogger(c *conf.Bootstrap, serviceName string) (log.Logger, func(), error) {
	if serviceName == "" {
		return logx.New(logx.Options{BaseFilename: ""})
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
		ConsoleToStderr:    true,
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
