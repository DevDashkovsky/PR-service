package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log = zap.NewNop()

func Init(development bool) error {
	var err error
	var config zap.Config

	if development {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
	}

	Log, err = config.Build()
	if err != nil {
		return err
	}

	return nil
}

func Sync() {
	if Log != nil {
		if err := Log.Sync(); err != nil {
			return
		}
	}
}
