package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"avito-ai-buddy/auth/controller"
	repository "avito-ai-buddy/auth/repository"
	auth "avito-ai-buddy/auth/usecase/auth"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL := os.Getenv("AUTH_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("AUTH_DATABASE_URL is required")
	}

	jwtSecret := os.Getenv("AUTH_JWT_SECRET")
	if jwtSecret == "" {
		return errors.New("AUTH_JWT_SECRET is required")
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errors.New("initialize PostgreSQL pool: check AUTH_DATABASE_URL")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	repo := repository.NewPostgresRepository(pool)
	service := auth.NewAuthService(repo, logger, []byte(jwtSecret))
	mux := http.NewServeMux()
	controller.New(service, logger).Register(mux)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("auth HTTP server listening", zap.String("address", server.Addr))

	return server.ListenAndServe()
}
