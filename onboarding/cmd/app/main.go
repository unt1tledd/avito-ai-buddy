package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"avito-ai-buddy/onboarding/internal/app"
	"avito-ai-buddy/onboarding/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig.TimeKey = "time"
	logConfig.EncoderConfig.MessageKey = "msg"
	logConfig.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	logConfig.DisableStacktrace = true
	logConfig.InitialFields = map[string]any{"service": "onboarding"}
	logger, err := logConfig.Build()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.New()
	if err != nil {
		logger.Fatal("cannot load onboarding configuration", zap.Error(err))
	}

	if err := app.Run(ctx, logger, cfg); err != nil {
		logger.Fatal("onboarding service stopped", zap.Error(err))
	}
}
