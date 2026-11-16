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
	"go.uber.org/zap/zapcore"
)

func main() {
	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig.TimeKey = "time"
	logConfig.EncoderConfig.MessageKey = "msg"
	logConfig.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	logConfig.DisableStacktrace = true
	logConfig.InitialFields = map[string]any{"service": "auth"}
	logger, err := logConfig.Build()
	if err != nil {
		log.Fatal(err)
	}
	if err := run(logger); err != nil {
		logger.Fatal("auth service stopped", zap.Error(err))
	}
}

func run(logger *zap.Logger) error {
	databaseURL := os.Getenv("AUTH_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("AUTH_DATABASE_URL is required")
	}

	jwtSecret := os.Getenv("AUTH_JWT_SECRET")
	if jwtSecret == "" {
		return errors.New("AUTH_JWT_SECRET is required")
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
	controller.RegisterHealth(mux, repo.CheckReady)

	server := &http.Server{
		Addr: ":8080",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			response := &authResponse{ResponseWriter: w, status: http.StatusOK}
			mux.ServeHTTP(response, r)
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			log := logger.Info
			if response.status >= 500 {
				log = logger.Error
			}
			log("HTTP request completed", zap.String("route", route), zap.Int("status", response.status),
				zap.Int64("duration_ms", time.Since(started).Milliseconds()))
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("auth HTTP server listening", zap.String("address", server.Addr))

	return server.ListenAndServe()
}

type authResponse struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *authResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *authResponse) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
