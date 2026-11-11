package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"avito-ai-buddy/chat/internal/port"

	ckafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const publisherDeliveryTimeout = 30000

// Publisher publishes chat commands to a Kafka topic and waits for the
// broker acknowledgement before returning.
type Publisher struct {
	producer *ckafka.Producer
	topic    string
}

// NewPublisher creates a producer with all-acks reliability; brokers must be
// non-empty.
func NewPublisher(brokers []string, topic string) (*Publisher, error) {
	if len(brokers) == 0 {
		return nil, errors.New("no kafka brokers configured")
	}
	producer, err := ckafka.NewProducer(&ckafka.ConfigMap{
		"bootstrap.servers":   strings.Join(brokers, ","),
		"acks":                "-1",
		"retries":             10,
		"delivery.timeout.ms": publisherDeliveryTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}
	return &Publisher{producer: producer, topic: topic}, nil
}

// Close flushes the pending messages and releases librdkafka resources.
func (p *Publisher) Close() {
	p.producer.Close()
}

type createAccessRequestPayload struct {
	UserID      string `json:"user_id"`
	ProposalID  string `json:"proposal_id"`
	ResourceID  string `json:"resource_id"`
	AccessLevel string `json:"access_level"`
}

// PublishCreateAccessRequest sends the confirmed access request keyed by the
// proposal ID and blocks until the broker confirms the delivery.
func (p *Publisher) PublishCreateAccessRequest(ctx context.Context, request port.CreateAccessRequest) error {
	proposalID := strconv.FormatInt(request.ProposalID, 10)
	body, err := json.Marshal(createAccessRequestPayload{
		UserID:      request.UserID,
		ProposalID:  proposalID,
		ResourceID:  request.ResourceID,
		AccessLevel: request.AccessLevel,
	})
	if err != nil {
		return fmt.Errorf("encode access request: %w", err)
	}

	return p.produce(ctx, []byte(proposalID), body)
}

func (p *Publisher) produce(ctx context.Context, key, value []byte) error {
	deliveryChan := make(chan ckafka.Event, 1)
	defer close(deliveryChan)

	if err := p.producer.Produce(&ckafka.Message{
		TopicPartition: ckafka.TopicPartition{
			Topic:     &p.topic,
			Partition: ckafka.PartitionAny,
		},
		Key:   key,
		Value: value,
	}, deliveryChan); err != nil {
		return fmt.Errorf("produce access request: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("produce access request: %w", ctx.Err())
	case event := <-deliveryChan:
		m, ok := event.(*ckafka.Message)
		if !ok {
			return fmt.Errorf("unexpected delivery event %T", event)
		}
		if m.TopicPartition.Error != nil {
			return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
		}
	}
	return nil
}
