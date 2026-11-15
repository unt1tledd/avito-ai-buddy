package kafka

import (
	"context"
	"fmt"
	"strconv"

	"avito-ai-buddy/task/internal/config"

	kafkago "github.com/segmentio/kafka-go"
)

type Publisher struct {
	writer *kafkago.Writer
}

func NewPublisher(brokers []string, topic string, cfg config.KafkaWriterConfig) *Publisher {
	return &Publisher{writer: &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll,
		BatchTimeout: cfg.BatchTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaxAttempts:  cfg.MaxAttempts,
	}}
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}

func (p *Publisher) CheckTopics(topics ...string) func(context.Context) error {
	return func(ctx context.Context) error {
		// Each probe needs fresh metadata, even if the publisher cached a healthy cluster.
		transport := &kafkago.Transport{}
		defer transport.CloseIdleConnections()
		client := &kafkago.Client{Addr: p.writer.Addr, Transport: transport}
		metadata, err := client.Metadata(ctx, &kafkago.MetadataRequest{})
		if err != nil {
			return err
		}
		available := make(map[string]bool, len(metadata.Topics))
		for _, topic := range metadata.Topics {
			ready := topic.Error == nil && len(topic.Partitions) > 0
			for _, partition := range topic.Partitions {
				ready = ready && partition.Error == nil && partition.Leader.Host != ""
			}
			available[topic.Name] = ready
		}
		for _, topic := range topics {
			if !available[topic] {
				return fmt.Errorf("Kafka topic %q is unavailable", topic)
			}
		}
		return nil
	}
}

func (p *Publisher) PublishTaskStatus(ctx context.Context, taskID int64, payload []byte) error {
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(strconv.FormatInt(taskID, 10)),
		Value: payload,
	})
}
