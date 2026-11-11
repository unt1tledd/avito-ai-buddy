package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

type OutboxConfig struct {
	Workers        int           `env:"TASK_OUTBOX_WORKERS" envDefault:"1"`
	BatchSize      int32         `env:"TASK_OUTBOX_BATCH_SIZE" envDefault:"10"`
	MaxAttempts    int32         `env:"TASK_OUTBOX_MAX_ATTEMPTS" envDefault:"5"`
	PollInterval   time.Duration `env:"TASK_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	RetryDelay     time.Duration `env:"TASK_OUTBOX_RETRY_DELAY" envDefault:"3s"`
	PublishTimeout time.Duration `env:"TASK_OUTBOX_PUBLISH_TIMEOUT" envDefault:"5s"`
}

type KafkaConsumerConfig struct {
	GroupID          string        `env:"TASK_KAFKA_CONSUMER_GROUP" envDefault:"task"`
	DLQTopic         string        `env:"TASK_KAFKA_COMMANDS_DLQ_TOPIC" envDefault:"task.access_requests.create-dlq"`
	MaxAttempts      int           `env:"TASK_KAFKA_MAX_ATTEMPTS" envDefault:"3"`
	RetryBaseBackoff time.Duration `env:"TASK_KAFKA_RETRY_BASE_BACKOFF" envDefault:"500ms"`
	RetryMaxBackoff  time.Duration `env:"TASK_KAFKA_RETRY_MAX_BACKOFF" envDefault:"30s"`
}

type Config struct {
	KafkaConsumer     KafkaConsumerConfig
	Outbox            OutboxConfig
	GRPCAddr          string `env:"TASK_GRPC_ADDR" envDefault:"127.0.0.1:9094"`
	HTTPAddr          string `env:"TASK_HTTP_ADDR" envDefault:"127.0.0.1:8084"`
	DatabaseURL       string `env:"TASK_DATABASE_URL,required"`
	TrackerToken      string `env:"TASK_TRACKER_TOKEN,required"`
	UserBaseURL       string `env:"TASK_USER_BASE_URL" envDefault:"http://127.0.0.1:8080"`
	KafkaBrokers      string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	TaskCommandsTopic string `env:"KAFKA_TASK_COMMANDS_TOPIC" envDefault:"task.access_requests.create"`
	TaskEventsTopic   string `env:"KAFKA_TASK_EVENTS_TOPIC" envDefault:"task.status.changed"`
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
	consumer := cfg.KafkaConsumer
	if strings.TrimSpace(consumer.DLQTopic) == "" || consumer.DLQTopic == cfg.TaskCommandsTopic ||
		consumer.DLQTopic == cfg.TaskEventsTopic || consumer.MaxAttempts <= 0 ||
		consumer.RetryBaseBackoff <= 0 || consumer.RetryMaxBackoff < consumer.RetryBaseBackoff {
		return Config{}, errors.New("task Kafka consumer needs a distinct DLQ topic, positive attempts and valid backoff")
	}
	if cfg.Outbox.Workers <= 0 || cfg.Outbox.BatchSize <= 0 || cfg.Outbox.MaxAttempts <= 0 ||
		cfg.Outbox.PollInterval <= 0 || cfg.Outbox.RetryDelay <= 0 || cfg.Outbox.PublishTimeout <= 0 {
		return Config{}, errors.New("task outbox settings must be positive")
	}
	userURL, err := url.Parse(cfg.UserBaseURL)
	if err != nil || userURL.Host == "" || (userURL.Scheme != "http" && userURL.Scheme != "https") {
		return Config{}, errors.New("TASK_USER_BASE_URL must be an absolute HTTP URL")
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
