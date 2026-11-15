package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

type OutboxConfig struct {
	Workers          int           `env:"TASK_OUTBOX_WORKERS" envDefault:"1"`
	BatchSize        int32         `env:"TASK_OUTBOX_BATCH_SIZE" envDefault:"10"`
	MaxAttempts      int32         `env:"TASK_OUTBOX_MAX_ATTEMPTS" envDefault:"5"`
	PollInterval     time.Duration `env:"TASK_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	RetryDelay       time.Duration `env:"TASK_OUTBOX_RETRY_DELAY" envDefault:"3s"`
	FailedRetryDelay time.Duration `env:"TASK_OUTBOX_FAILED_RETRY_DELAY" envDefault:"1m"`
	PublishTimeout   time.Duration `env:"TASK_OUTBOX_PUBLISH_TIMEOUT" envDefault:"5s"`
}

type HTTPServerConfig struct {
	MaxBodyBytes      int64         `env:"TASK_HTTP_MAX_BODY_BYTES" envDefault:"8192"`
	ReadHeaderTimeout time.Duration `env:"TASK_HTTP_READ_HEADER_TIMEOUT" envDefault:"3s"`
	ReadTimeout       time.Duration `env:"TASK_HTTP_READ_TIMEOUT" envDefault:"5s"`
	WriteTimeout      time.Duration `env:"TASK_HTTP_WRITE_TIMEOUT" envDefault:"10s"`
	IdleTimeout       time.Duration `env:"TASK_HTTP_IDLE_TIMEOUT" envDefault:"30s"`
}

type KafkaWriterConfig struct {
	BatchTimeout time.Duration `env:"TASK_KAFKA_WRITER_BATCH_TIMEOUT" envDefault:"10ms"`
	WriteTimeout time.Duration `env:"TASK_KAFKA_WRITER_WRITE_TIMEOUT" envDefault:"5s"`
	MaxAttempts  int           `env:"TASK_KAFKA_WRITER_MAX_ATTEMPTS" envDefault:"1"`
}

type KafkaConsumerConfig struct {
	DLQPublishTimeout time.Duration `env:"TASK_KAFKA_DLQ_PUBLISH_TIMEOUT" envDefault:"5s"`
	GroupID           string        `env:"TASK_KAFKA_CONSUMER_GROUP" envDefault:"task"`
	DLQTopic          string        `env:"TASK_KAFKA_COMMANDS_DLQ_TOPIC" envDefault:"task.access_requests.create-dlq"`
	MaxAttempts       int           `env:"TASK_KAFKA_MAX_ATTEMPTS" envDefault:"3"`
	RetryBaseBackoff  time.Duration `env:"TASK_KAFKA_RETRY_BASE_BACKOFF" envDefault:"500ms"`
	RetryMaxBackoff   time.Duration `env:"TASK_KAFKA_RETRY_MAX_BACKOFF" envDefault:"30s"`
}

type Config struct {
	HTTPServer           HTTPServerConfig
	KafkaWriter          KafkaWriterConfig
	ShutdownTimeout      time.Duration `env:"TASK_SHUTDOWN_TIMEOUT" envDefault:"5s"`
	AuthTimeout          time.Duration `env:"TASK_AUTH_TIMEOUT" envDefault:"3s"`
	AuthMaxResponseBytes int64         `env:"TASK_AUTH_MAX_RESPONSE_BYTES" envDefault:"4096"`
	UserTimeout          time.Duration `env:"TASK_USER_TIMEOUT" envDefault:"3s"`
	UserMaxResponseBytes int64         `env:"TASK_USER_MAX_RESPONSE_BYTES" envDefault:"65536"`
	DefaultPageSize      int32         `env:"TASK_DEFAULT_PAGE_SIZE" envDefault:"50"`
	KafkaConsumer        KafkaConsumerConfig
	Outbox               OutboxConfig
	GRPCAddr             string `env:"TASK_GRPC_ADDR" envDefault:"127.0.0.1:9094"`
	HTTPAddr             string `env:"TASK_HTTP_ADDR" envDefault:"127.0.0.1:8084"`
	DatabaseURL          string `env:"TASK_DATABASE_URL,required"`
	TrackerToken         string `env:"TASK_TRACKER_TOKEN,required"`
	AuthBaseURL          string `env:"TASK_AUTH_BASE_URL" envDefault:"http://auth:8080"`
	UserBaseURL          string `env:"TASK_USER_BASE_URL" envDefault:"http://127.0.0.1:8080"`
	KafkaBrokers         string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	TaskCommandsTopic    string `env:"KAFKA_TASK_COMMANDS_TOPIC" envDefault:"task.access_requests.create"`
	TaskEventsTopic      string `env:"KAFKA_TASK_EVENTS_TOPIC" envDefault:"task.status.changed"`
}

func New() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.TrackerToken) == "" || len(cfg.Brokers()) == 0 ||
		cfg.TaskCommandsTopic == "" || cfg.TaskEventsTopic == "" || strings.TrimSpace(cfg.KafkaConsumer.GroupID) == "" {
		return Config{}, errors.New("tracker token, Kafka brokers, topics and group are required")
	}
	httpServer := cfg.HTTPServer
	if httpServer.MaxBodyBytes <= 0 || httpServer.ReadHeaderTimeout <= 0 ||
		httpServer.ReadTimeout <= 0 || httpServer.WriteTimeout <= 0 || httpServer.IdleTimeout <= 0 {
		return Config{}, errors.New("TASK_HTTP body limit and timeouts must be positive")
	}
	if cfg.ShutdownTimeout <= 0 || cfg.AuthTimeout <= 0 || cfg.UserTimeout <= 0 ||
		cfg.AuthMaxResponseBytes <= 0 || cfg.UserMaxResponseBytes <= 0 {
		return Config{}, errors.New("task shutdown timeout and HTTP client timeouts and response limits must be positive")
	}
	if cfg.DefaultPageSize < 1 || cfg.DefaultPageSize > 100 {
		return Config{}, errors.New("TASK_DEFAULT_PAGE_SIZE must be between 1 and 100")
	}
	writer := cfg.KafkaWriter
	if writer.BatchTimeout <= 0 || writer.WriteTimeout <= 0 || writer.MaxAttempts <= 0 {
		return Config{}, errors.New("task Kafka writer timeouts and attempts must be positive")
	}
	consumer := cfg.KafkaConsumer
	if strings.TrimSpace(consumer.DLQTopic) == "" || consumer.DLQTopic == cfg.TaskCommandsTopic ||
		consumer.DLQTopic == cfg.TaskEventsTopic || consumer.MaxAttempts <= 0 ||
		consumer.DLQPublishTimeout <= 0 || consumer.RetryBaseBackoff <= 0 || consumer.RetryMaxBackoff < consumer.RetryBaseBackoff {
		return Config{}, errors.New("task Kafka consumer needs a distinct DLQ topic, positive attempts and timeout, and valid backoff")
	}
	if cfg.Outbox.Workers <= 0 || cfg.Outbox.BatchSize <= 0 || cfg.Outbox.MaxAttempts <= 0 ||
		cfg.Outbox.PollInterval <= 0 || cfg.Outbox.RetryDelay <= 0 ||
		cfg.Outbox.FailedRetryDelay < cfg.Outbox.RetryDelay || cfg.Outbox.PublishTimeout <= 0 {
		return Config{}, errors.New("task outbox settings must be positive and failed retry delay must be at least the retry delay")
	}
	userURL, err := url.Parse(cfg.UserBaseURL)
	if err != nil || userURL.Host == "" || (userURL.Scheme != "http" && userURL.Scheme != "https") {
		return Config{}, errors.New("TASK_USER_BASE_URL must be an absolute HTTP URL")
	}
	authURL, err := url.Parse(cfg.AuthBaseURL)
	if err != nil || authURL.Host == "" || (authURL.Scheme != "http" && authURL.Scheme != "https") {
		return Config{}, errors.New("TASK_AUTH_BASE_URL must be an absolute HTTP URL")
	}
	return cfg, nil
}

func (c Config) Brokers() []string {
	var brokers []string
	for _, address := range strings.Split(c.KafkaBrokers, ",") {
		if address = strings.TrimSpace(address); address != "" {
			brokers = append(brokers, address)
		}
	}
	return brokers
}
