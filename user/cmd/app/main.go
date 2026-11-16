package main

import (
	"log"

	"avito-ai-buddy/user/internal/app"
	"avito-ai-buddy/user/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig.TimeKey = "time"
	logConfig.EncoderConfig.MessageKey = "msg"
	logConfig.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	logConfig.DisableStacktrace = true
	logConfig.InitialFields = map[string]any{"service": "user"}
	logger, err := logConfig.Build()
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.New()
	if err != nil {
		logger.Fatal("cannot load user configuration", zap.Error(err))
	}
	if err := app.Run(logger, cfg); err != nil {
		logger.Fatal("user service stopped", zap.Error(err))
	}
}
