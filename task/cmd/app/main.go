package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"avito-ai-buddy/task/internal/app"
	"avito-ai-buddy/task/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.LevelKey {
				attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
			}
			return attr
		},
	})).With("service", "task")
	cfg, err := config.New()
	if err == nil {
		err = app.Run(ctx, logger, cfg)
	}
	if err != nil {
		logger.Error("task service stopped", "error", err)
		os.Exit(1)
	}
}
