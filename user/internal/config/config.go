package config

import (
	"net"
	"net/url"
	"time"

	"github.com/caarlos0/env/v10"
)

const (
	ShutdownTimeout   = 5 * time.Second
	HealthCheckPeriod = 30 * time.Second
	MaxConnIdleTime   = 5 * time.Minute
)

type Config struct {
	GRPC struct {
		Host        string `env:"GRPC_HOST" envDefault:"127.0.0.1"`
		Port        string `env:"GRPC_PORT" envDefault:"9090"`
		GatewayPort string `env:"GRPC_GATEWAY_PORT" envDefault:"8080"`
	}
	PG struct {
		Host     string `env:"POSTGRES_HOST" envDefault:"localhost"`
		Port     string `env:"POSTGRES_PORT" envDefault:"5432"`
		DB       string `env:"POSTGRES_DB" envDefault:"users"`
		User     string `env:"POSTGRES_USER" envDefault:"users"`
		Password string `env:"POSTGRES_PASSWORD" envDefault:"12345"`
	}
}

func (c *Config) ConstructPostgresURL() string {
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.PG.User, c.PG.Password),
		Host:     net.JoinHostPort(c.PG.Host, c.PG.Port),
		Path:     "/" + c.PG.DB,
		RawQuery: "sslmode=disable",
	}).String()
}

func New() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
