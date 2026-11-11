package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"avito-ai-buddy/task/internal/app"
	"avito-ai-buddy/task/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.New()
	if err == nil {
		err = app.Run(ctx, slog.Default(), cfg)
	}
	if err != nil {
		slog.Error("task service stopped", "error", err)
		os.Exit(1)
	}
}
