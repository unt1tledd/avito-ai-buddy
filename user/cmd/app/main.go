package main

import (
	"log"

	"avito-ai-buddy/user/internal/app"
	"avito-ai-buddy/user/internal/config"

	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
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
