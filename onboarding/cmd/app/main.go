package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"avito-ai-buddy/onboarding/internal/app"
	"avito-ai-buddy/onboarding/internal/config"

	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.New()
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(ctx, logger, cfg); err != nil {
		log.Fatal(err)
	}
}
