package config

import (
	"errors"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	Clients struct {
		UserGRPCAddr string        `env:"USER_GRPC_ADDR" envDefault:"user:9090"`
		UserTimeout  time.Duration `env:"ONBOARDING_USER_TIMEOUT" envDefault:"5s"`
	}
	Auth struct {
		BaseURL      string        `env:"AUTH_BASE_URL" envDefault:"http://auth:8080"`
		ValidatePath string        `env:"AUTH_VALIDATE_PATH" envDefault:"/v1/auth/validate"`
		Timeout      time.Duration `env:"AUTH_TIMEOUT" envDefault:"5s"`
	}
	GRPC struct {
		Port        string `env:"ONBOARDING_GRPC_PORT" envDefault:"50051"`
		GatewayPort string `env:"ONBOARDING_HTTP_PORT" envDefault:"8080"`
	}
	PG struct {
		URL string `env:"ONBOARDING_POSTGRES_URL,required"`
	}
	App struct {
		SeedDemo              bool          `env:"ONBOARDING_SEED_DEMO"`
		ServiceToken          string        `env:"ONBOARDING_SERVICE_TOKEN,required"`
		ShutdownTimeout       time.Duration `env:"ONBOARDING_SHUTDOWN_TIMEOUT" envDefault:"5s"`
		HTTPReadHeaderTimeout time.Duration `env:"ONBOARDING_HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	}
}

func New() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.PG.URL) == "" || strings.TrimSpace(cfg.App.ServiceToken) == "" {
		return nil, errors.New("onboarding database URL and service token are required")
	}
	if strings.TrimSpace(cfg.Auth.BaseURL) == "" || strings.TrimSpace(cfg.Auth.ValidatePath) == "" {
		return nil, errors.New("Auth URL and validation path are required")
	}
	if cfg.Auth.Timeout <= 0 {
		return nil, errors.New("AUTH_TIMEOUT must be positive")
	}
	if cfg.App.ShutdownTimeout <= 0 {
		return nil, errors.New("ONBOARDING_SHUTDOWN_TIMEOUT must be positive")
	}
	if strings.TrimSpace(cfg.Clients.UserGRPCAddr) == "" {
		return nil, errors.New("USER_GRPC_ADDR must not be empty")
	}
	if cfg.Clients.UserTimeout <= 0 {
		return nil, errors.New("ONBOARDING_USER_TIMEOUT must be positive")
	}
	if cfg.App.HTTPReadHeaderTimeout <= 0 {
		return nil, errors.New("ONBOARDING_HTTP_READ_HEADER_TIMEOUT must be positive")
	}
	return &cfg, nil
}
