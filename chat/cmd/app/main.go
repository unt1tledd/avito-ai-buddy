package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"avito-ai-buddy/chat/internal/app"
	"avito-ai-buddy/chat/internal/config"

	"go.uber.org/zap"
)

// main boots the chat service and exits with a non-zero code on failure.
func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run keeps the logger alive until the deferred Sync executes, so error logs
// are not lost when the service exits with an error.
func run() error {
	logger, err := zap.NewProduction()
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.New()
	if err != nil {
		logger.Error("load configuration", zap.Error(err))
		return err
	}

	if err := app.Run(ctx, logger, cfg); err != nil {
		logger.Error("chat service exited", zap.Error(err))
		return err
	}
	return nil
}
