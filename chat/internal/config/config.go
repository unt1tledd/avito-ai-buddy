package config

import (
	"errors"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	Clients struct {
		UserGRPCAddr       string `env:"USER_GRPC_ADDR" envDefault:"user:50051"`
		OnboardingGRPCAddr string `env:"ONBOARDING_GRPC_ADDR" envDefault:"onboarding:50051"`
		AIBaseURL          string `env:"AI_BASE_URL" envDefault:"http://ai:8080"`
	}

	Auth struct {
		BaseURL      string        `env:"AUTH_BASE_URL" envDefault:"http://auth:8080"`
		ValidatePath string        `env:"AUTH_VALIDATE_PATH" envDefault:"/v1/auth/validate"`
		Timeout      time.Duration `env:"AUTH_TIMEOUT" envDefault:"5s"`
	}

	GRPC struct {
		Port        string `env:"CHAT_GRPC_PORT" envDefault:"50051"`
		GatewayPort string `env:"CHAT_HTTP_PORT" envDefault:"8080"`
	}

	PG struct {
		URL string `env:"CHAT_POSTGRES_URL" envDefault:"postgres://localhost:5432/chat"`
	}

	Kafka KafkaConfig

	Inbox struct {
		Retention       time.Duration `env:"CHAT_INBOX_RETENTION" envDefault:"168h"`
		CleanupInterval time.Duration `env:"CHAT_INBOX_CLEANUP_INTERVAL" envDefault:"1h"`
	}

	Outbox struct {
		BatchSize      int32         `env:"CHAT_OUTBOX_BATCH_SIZE" envDefault:"10"`
		InProgressTTL  time.Duration `env:"CHAT_OUTBOX_IN_PROGRESS_TTL" envDefault:"1m"`
		PollInterval   time.Duration `env:"CHAT_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
		RetryDelay     time.Duration `env:"CHAT_OUTBOX_RETRY_DELAY" envDefault:"5s"`
		PublishTimeout time.Duration `env:"CHAT_OUTBOX_PUBLISH_TIMEOUT" envDefault:"5s"`
		MaxAttempts    int32         `env:"CHAT_OUTBOX_MAX_ATTEMPTS" envDefault:"5"`
	}

	App struct {
		HistoryPageSize int32 `env:"CHAT_HISTORY_PAGE_SIZE" envDefault:"30"`
	}
}

type KafkaConfig struct {
	Brokers           string        `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	TaskCommandsTopic string        `env:"KAFKA_TASK_COMMANDS_TOPIC" envDefault:"task.access_requests.create"`
	TaskEventsTopic   string        `env:"KAFKA_TASK_EVENTS_TOPIC" envDefault:"task.status.changed"`
	DLQTopic          string        `env:"KAFKA_TASK_EVENTS_DLQ_TOPIC" envDefault:"task.status.changed-dlq"`
	ConsumerGroup     string        `env:"KAFKA_CONSUMER_GROUP" envDefault:"chat"`
	SessionTimeoutMs  int           `env:"KAFKA_SESSION_TIMEOUT_MS" envDefault:"10000"`
	PollTimeoutMs     int           `env:"KAFKA_POLL_TIMEOUT_MS" envDefault:"100"`
	BatchSize         int           `env:"KAFKA_CONSUMER_BATCH_SIZE" envDefault:"10"`
	RetryBaseBackoff  time.Duration `env:"KAFKA_RETRY_BASE_BACKOFF" envDefault:"500ms"`
	RetryMaxBackoff   time.Duration `env:"KAFKA_RETRY_MAX_BACKOFF" envDefault:"30s"`
	MaxAttempts       int           `env:"KAFKA_NOTIFY_MAX_ATTEMPTS" envDefault:"3"`
}

func New() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}
	if cfg.Auth.Timeout <= 0 {
		return nil, errors.New("AUTH_TIMEOUT must be positive")
	}
	if cfg.App.HistoryPageSize <= 0 {
		return nil, errors.New("CHAT_HISTORY_PAGE_SIZE must be positive")
	}
	if cfg.Outbox.BatchSize <= 0 {
		return nil, errors.New("CHAT_OUTBOX_BATCH_SIZE must be positive")
	}
	if cfg.Outbox.InProgressTTL <= 0 || cfg.Outbox.PollInterval <= 0 || cfg.Outbox.RetryDelay <= 0 || cfg.Outbox.PublishTimeout <= 0 {
		return nil, errors.New("outbox durations must be positive")
	}
	if cfg.Kafka.RetryBaseBackoff <= 0 || cfg.Kafka.RetryMaxBackoff <= 0 || cfg.Kafka.MaxAttempts <= 0 {
		return nil, errors.New("kafka retry settings must be positive")
	}
	if cfg.Inbox.Retention <= 0 || cfg.Inbox.CleanupInterval <= 0 {
		return nil, errors.New("inbox retention settings must be positive")
	}
	if cfg.Outbox.MaxAttempts <= 0 {
		return nil, errors.New("CHAT_OUTBOX_MAX_ATTEMPTS must be positive")
	}
	return &cfg, nil
}

func (c KafkaConfig) BrokerAddrs() []string {
	var brokers []string
	for _, part := range strings.Split(c.Brokers, ",") {
		if address := strings.TrimSpace(part); address != "" {
			brokers = append(brokers, address)
		}
	}
	return brokers
}
