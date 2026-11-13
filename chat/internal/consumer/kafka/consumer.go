package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"avito-ai-buddy/chat/internal/config"
	"avito-ai-buddy/chat/internal/port"

	ckafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"go.uber.org/zap"
)

//go:generate mockgen -source=consumer.go -destination=mocks/consumer_mocks.go -package=mocks

type (
	Notifier = port.TaskEventsNotifier
)

var errDLQProducerDisabled = errors.New("kafka dlq producer disabled")

// RunConsumer runs the task events consumer until ctx is cancelled.
func RunConsumer(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.KafkaConfig,
	notifier Notifier,
) error {
	brokers := cfg.BrokerAddrs()
	if len(brokers) == 0 {
		return errors.New("empty kafka brokers")
	}

	bootstrapServersKey := "bootstrap.servers"
	consumer, err := ckafka.NewConsumer(&ckafka.ConfigMap{
		bootstrapServersKey:        joinBrokers(brokers),
		"group.id":                 cfg.ConsumerGroup,
		"auto.offset.reset":        "earliest",
		"enable.auto.commit":       false,
		"enable.auto.offset.store": false,
		"session.timeout.ms":       cfg.SessionTimeoutMs,
	})
	if err != nil {
		return err
	}

	defer func() {
		if err := consumer.Close(); err != nil {
			logger.Error("close kafka consumer", zap.Error(err))
		}
	}()

	var producer *ckafka.Producer
	defer func() {
		if producer != nil {
			producer.Close()
		}
	}()

	if err := consumer.SubscribeTopics([]string{cfg.TaskEventsTopic}, nil); err != nil {
		return err
	}

	logger.Info("kafka consumer started",
		zap.Strings("brokers", brokers),
		zap.String("topic", cfg.TaskEventsTopic),
		zap.String("dlq_topic", cfg.DLQTopic),
		zap.String("group", cfg.ConsumerGroup),
		zap.Int("batch_size", batchSize(cfg)),
		zap.Int("max_attempts", maxAttempts(cfg)),
	)

	for {
		select {
		case <-ctx.Done():
			logger.Info("kafka consumer stopped", zap.Error(ctx.Err()))
			return nil
		default:
		}

		batch, ok := pollBatch(ctx, logger, cfg, consumer)
		if !ok {
			return nil
		}

		if len(batch) == 0 {
			continue
		}

		if !processBatchWithoutCommit(ctx, logger, cfg, &producer, brokers, bootstrapServersKey, notifier, batch) {
			if ctx.Err() != nil {
				logger.Info("kafka consumer stopped", zap.Error(ctx.Err()))
				return nil
			}
			time.Sleep(cfg.RetryBaseBackoff)
			continue
		}

		if _, err := consumer.CommitOffsets(commitOffsets(batch)); err != nil {
			logger.Error("kafka batch commit",
				zap.Error(err),
				zap.Int("batch_size", len(batch)),
			)

			time.Sleep(cfg.RetryBaseBackoff)
			continue
		}

		logger.Debug("kafka batch committed", zap.Int("batch_size", len(batch)))
	}
}

type batchConsumer interface {
	Poll(timeoutMs int) ckafka.Event
	Assign(partitions []ckafka.TopicPartition) error
	Unassign() error
}

type dlqWriter interface {
	Produce(msg *ckafka.Message, deliveryChan chan ckafka.Event) error
}

func pollBatch(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.KafkaConfig,
	consumer batchConsumer,
) ([]*ckafka.Message, bool) {
	batch := make([]*ckafka.Message, 0, batchSize(cfg))

	for len(batch) < batchSize(cfg) {
		select {
		case <-ctx.Done():
			logger.Info("kafka consumer stopped", zap.Error(ctx.Err()))
			return batch, false
		default:
		}

		pollTimeoutMs := cfg.PollTimeoutMs
		if len(batch) > 0 {
			pollTimeoutMs = 0
		}

		ev := consumer.Poll(pollTimeoutMs)
		if ev == nil {
			return batch, true
		}

		switch e := ev.(type) {
		case *ckafka.Message:
			logger.Debug("message fetched",
				zap.String("topic", topicName(e)),
				zap.Int32("partition", e.TopicPartition.Partition),
				zap.Int64("offset", int64(e.TopicPartition.Offset)),
				zap.ByteString("key", e.Key),
			)

			batch = append(batch, e)

		case ckafka.Error:
			logger.Warn("kafka poll error",
				zap.String("code", e.Code().String()),
				zap.Error(e),
			)

			time.Sleep(cfg.RetryBaseBackoff)

		case ckafka.AssignedPartitions:
			logger.Info("kafka partitions assigned",
				zap.Any("partitions", e.Partitions),
			)

			if err := consumer.Assign(e.Partitions); err != nil {
				logger.Error("kafka assign partitions", zap.Error(err))
			}

		case ckafka.RevokedPartitions:
			logger.Info("kafka partitions revoked",
				zap.Any("partitions", e.Partitions),
			)

			if err := consumer.Unassign(); err != nil {
				logger.Error("kafka unassign partitions", zap.Error(err))
			}

		default:
		}
	}

	return batch, true
}

func processBatchWithoutCommit(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.KafkaConfig,
	producer **ckafka.Producer,
	brokers []string,
	bootstrapServersKey string,
	notifier Notifier,
	batch []*ckafka.Message,
) bool {
	for _, message := range batch {
		if !processMessageWithoutCommit(ctx, logger, cfg, producer, brokers, bootstrapServersKey, notifier, message) {
			return false
		}
	}

	return true
}

func processMessageWithoutCommit(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.KafkaConfig,
	producer **ckafka.Producer,
	brokers []string,
	bootstrapServersKey string,
	notifier Notifier,
	message *ckafka.Message,
) bool {
	var event port.TaskEvent
	if unmarshalErr := json.Unmarshal(message.Value, &event); unmarshalErr != nil {
		// Malformed payloads never become valid, so they are sent to the DLQ
		// right away instead of wasting retry attempts.
		return discardToDLQ(ctx, logger, cfg, producer, brokers, bootstrapServersKey, message, unmarshalErr)
	}

	finalErr := notifyWithRetry(ctx, logger, cfg, notifier, event)
	if ctx.Err() != nil {
		return false
	}

	if finalErr != nil {
		// The handler failed after exhausting all retry attempts: park the
		// message in the DLQ so the batch can move on.
		return discardToDLQ(ctx, logger, cfg, producer, brokers, bootstrapServersKey, message, finalErr)
	}

	return true
}

func discardToDLQ(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.KafkaConfig,
	producer **ckafka.Producer,
	brokers []string,
	bootstrapServersKey string,
	message *ckafka.Message,
	cause error,
) bool {
	producerClient, err := getDLQProducer(producer, brokers, bootstrapServersKey)
	if err != nil {
		if errors.Is(err, errDLQProducerDisabled) {
			return true
		}

		logger.Error("create kafka dlq producer", zap.Error(err))
		return false
	}

	if producerClient == nil {
		return true
	}

	if produceErr := sendToDLQ(ctx, cfg, producerClient, message, cause); produceErr != nil {
		logger.Error("send kafka message to dlq",
			zap.Error(produceErr),
			zap.String("topic", topicName(message)),
			zap.Int32("partition", message.TopicPartition.Partition),
			zap.Int64("offset", int64(message.TopicPartition.Offset)),
		)

		return false
	}

	logger.Info("message sent to dlq",
		zap.String("topic", topicName(message)),
		zap.Int32("partition", message.TopicPartition.Partition),
		zap.Int64("offset", int64(message.TopicPartition.Offset)),
		zap.Error(cause),
	)

	return true
}

// notifyWithRetry retries failed notifications with exponential backoff.
// The delay doubles after every attempt until RetryMaxBackoff is reached.
// After MaxAttempts failed attempts the last error is returned so the caller
// can route the message to the DLQ.
func notifyWithRetry(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config.KafkaConfig,
	notifier Notifier,
	event port.TaskEvent,
) error {
	backoff := cfg.RetryBaseBackoff

	for attempt := 1; ; attempt++ {
		err := notifier.PublishTaskStatus(ctx, event)
		if err == nil {
			return nil
		}

		logger.Error("publish task status",
			zap.Error(err),
			zap.Int("attempt", attempt),
			zap.String("user_id", event.UserID),
			zap.String("task_id", event.TaskID),
			zap.String("status", event.Status),
		)

		if attempt >= maxAttempts(cfg) {
			return fmt.Errorf("publish task status after %d attempts: %w", attempt, err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		if backoff < cfg.RetryMaxBackoff {
			backoff = min(backoff*2, cfg.RetryMaxBackoff)
		}
	}
}

func getDLQProducer(producer **ckafka.Producer, brokers []string, bootstrapServersKey string) (dlqWriter, error) {
	if producer == nil {
		return nil, errDLQProducerDisabled
	}

	if *producer != nil {
		return *producer, nil
	}

	created, err := ckafka.NewProducer(&ckafka.ConfigMap{
		bootstrapServersKey: joinBrokers(brokers),
	})
	if err != nil {
		return nil, err
	}

	*producer = created
	return created, nil
}

func sendToDLQ(
	ctx context.Context,
	cfg *config.KafkaConfig,
	producer dlqWriter,
	message *ckafka.Message,
	cause error,
) error {
	if cfg.DLQTopic == "" {
		return errors.New("empty kafka dlq topic")
	}

	deliveryCh := make(chan ckafka.Event, 1)

	err := producer.Produce(&ckafka.Message{
		TopicPartition: ckafka.TopicPartition{
			Topic:     &cfg.DLQTopic,
			Partition: ckafka.PartitionAny,
		},
		Key:   message.Key,
		Value: message.Value,
		Headers: append(message.Headers,
			ckafka.Header{Key: "x-original-topic", Value: []byte(topicName(message))},
			ckafka.Header{Key: "x-original-partition", Value: []byte(strconv.Itoa(int(message.TopicPartition.Partition)))},
			ckafka.Header{Key: "x-original-offset", Value: []byte(strconv.FormatInt(int64(message.TopicPartition.Offset), 10))},
			ckafka.Header{Key: "x-error", Value: []byte(cause.Error())},
		),
	}, deliveryCh)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case ev := <-deliveryCh:
		delivery, ok := ev.(*ckafka.Message)
		if !ok {
			return fmt.Errorf("unexpected kafka dlq delivery event: %T", ev)
		}

		return delivery.TopicPartition.Error
	}
}

func commitOffsets(batch []*ckafka.Message) []ckafka.TopicPartition {
	offsets := make(map[string]ckafka.TopicPartition)

	for _, message := range batch {
		topic := topicName(message)
		key := fmt.Sprintf("%s:%d", topic, message.TopicPartition.Partition)
		offset := message.TopicPartition
		offset.Offset++
		offset.Error = nil

		current, ok := offsets[key]
		if !ok || offset.Offset > current.Offset {
			offsets[key] = offset
		}
	}

	result := make([]ckafka.TopicPartition, 0, len(offsets))
	for _, offset := range offsets {
		result = append(result, offset)
	}

	return result
}

func batchSize(cfg *config.KafkaConfig) int {
	if cfg.BatchSize <= 0 {
		return 1
	}

	return cfg.BatchSize
}

func maxAttempts(cfg *config.KafkaConfig) int {
	if cfg.MaxAttempts <= 0 {
		return 1
	}

	return cfg.MaxAttempts
}

func topicName(message *ckafka.Message) string {
	if message.TopicPartition.Topic == nil {
		return ""
	}

	return *message.TopicPartition.Topic
}

func joinBrokers(brokers []string) string {
	return strings.Join(brokers, ",")
}
