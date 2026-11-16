package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"avito-ai-buddy/task/internal/config"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type createHandler interface {
	Handle(context.Context, []byte) error
	ReportFailure(context.Context, []byte) error
}

type commandReader interface {
	FetchMessage(context.Context) (kafkago.Message, error)
	CommitMessages(context.Context, ...kafkago.Message) error
}

type dlqWriter interface {
	WriteMessages(context.Context, ...kafkago.Message) error
}

func RunConsumer(ctx context.Context, logger *slog.Logger, brokers []string, topic string, cfg config.KafkaConsumerConfig, writerConfig config.KafkaWriterConfig, handler createHandler) error {
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: brokers, Topic: topic, GroupID: cfg.GroupID, StartOffset: kafkago.FirstOffset,
	})
	defer func() { _ = reader.Close() }()
	dlq := &kafkago.Writer{
		Addr: kafkago.TCP(brokers...), Topic: cfg.DLQTopic, Balancer: &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll, BatchTimeout: writerConfig.BatchTimeout,
		WriteTimeout: writerConfig.WriteTimeout, MaxAttempts: writerConfig.MaxAttempts,
	}
	defer func() { _ = dlq.Close() }()

	logger.Info("task Kafka consumer starting", "topic", topic, "group", cfg.GroupID, "dlq", cfg.DLQTopic)
	return consume(ctx, logger, reader, dlq, handler, cfg)
}

func consume(ctx context.Context, logger *slog.Logger, reader commandReader, dlq dlqWriter, handler createHandler, cfg config.KafkaConsumerConfig) error {
	for ctx.Err() == nil {
		var message kafkago.Message
		if !retryKafka(ctx, logger, cfg, "fetch Kafka command", func() error {
			var err error
			message, err = reader.FetchMessage(ctx)
			return err
		}) {
			return nil
		}
		log := logger.With("topic", message.Topic, "partition", message.Partition, "offset", message.Offset)

		cause := handleWithRetry(ctx, log, handler, message.Value, cfg)
		if ctx.Err() != nil {
			return nil
		}
		if cause != nil {
			dead := deadLetter(message, cause)
			// Keep the original offset uncommitted until Kafka acknowledges the DLQ write.
			if !retryKafka(ctx, log, cfg, "publish task command to DLQ", func() error {
				publishCtx, cancel := context.WithTimeout(ctx, cfg.DLQPublishTimeout)
				defer cancel()
				return dlq.WriteMessages(publishCtx, dead)
			}) {
				return nil
			}
			log.Warn("task command sent to DLQ", "error", cause)
			if !retryKafka(ctx, log, cfg, "report task command result", func() error {
				publishCtx, cancel := context.WithTimeout(ctx, cfg.DLQPublishTimeout)
				defer cancel()
				return handler.ReportFailure(publishCtx, message.Value)
			}) {
				return nil
			}
		}
		if !retryKafka(ctx, log, cfg, "commit task Kafka offset", func() error {
			return reader.CommitMessages(ctx, message)
		}) {
			return nil
		}
	}
	return nil
}

func handleWithRetry(ctx context.Context, logger *slog.Logger, handler createHandler, payload []byte, cfg config.KafkaConsumerConfig) error {
	delay := cfg.RetryBaseBackoff
	for attempt := 1; ; attempt++ {
		err := handler.Handle(ctx, payload)
		if err == nil || errors.Is(err, taskerrors.ErrInvalidInput) || errors.Is(err, taskerrors.ErrProposalConflict) {
			return err
		}
		if attempt >= cfg.MaxAttempts {
			return fmt.Errorf("task command failed after %d attempts: %w", attempt, err)
		}
		logger.Warn("retry task command", "attempt", attempt, "delay", delay, "error", err)
		if !wait(ctx, delay) {
			return ctx.Err()
		}
		delay = nextBackoff(delay, cfg.RetryMaxBackoff)
	}
}

func deadLetter(message kafkago.Message, cause error) kafkago.Message {
	return kafkago.Message{
		Key:   message.Key,
		Value: message.Value,
		Headers: append(slices.Clone(message.Headers),
			kafkago.Header{Key: "x-original-topic", Value: []byte(message.Topic)},
			kafkago.Header{Key: "x-original-partition", Value: []byte(strconv.Itoa(message.Partition))},
			kafkago.Header{Key: "x-original-offset", Value: []byte(strconv.FormatInt(message.Offset, 10))},
			kafkago.Header{Key: "x-error", Value: []byte(cause.Error())},
		),
	}
}

func retryKafka(ctx context.Context, logger *slog.Logger, cfg config.KafkaConsumerConfig, operation string, run func() error) bool {
	delay := cfg.RetryBaseBackoff
	for ctx.Err() == nil {
		err := run()
		if err == nil {
			return true
		}
		logger.Warn(operation, "retry_in", delay, "error", err)
		if !wait(ctx, delay) {
			return false
		}
		delay = nextBackoff(delay, cfg.RetryMaxBackoff)
	}
	return false
}

func nextBackoff(delay, maximum time.Duration) time.Duration {
	if delay >= maximum-delay {
		return maximum
	}
	return delay * 2
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
