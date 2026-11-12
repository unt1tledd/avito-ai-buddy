package kafka

import (
	"context"
	"strconv"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

type Publisher struct {
	writer *kafkago.Writer
}

func NewPublisher(brokers []string, topic string) *Publisher {
	return &Publisher{writer: &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
		WriteTimeout: 5 * time.Second,
		MaxAttempts:  1,
	}}
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}

func (p *Publisher) PublishTaskStatus(ctx context.Context, taskID int64, payload []byte) error {
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(strconv.FormatInt(taskID, 10)),
		Value: payload,
	})
}
