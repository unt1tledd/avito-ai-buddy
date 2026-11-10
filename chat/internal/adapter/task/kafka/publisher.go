package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"avito-ai-buddy/chat/internal/port"

	"github.com/segmentio/kafka-go"
)

type Publisher struct {
	writer *kafka.Writer
}

func NewPublisher(brokers []string, topic string) *Publisher {
	return &Publisher{writer: &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
	}}
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}

func (p *Publisher) PublishCreateAccessRequest(ctx context.Context, request port.CreateAccessRequest) error {
	proposalID := strconv.FormatInt(request.ProposalID, 10)
	body, err := json.Marshal(struct {
		UserID      string `json:"user_id"`
		ProposalID  string `json:"proposal_id"`
		ResourceID  string `json:"resource_id"`
		AccessLevel string `json:"access_level"`
	}{
		UserID:      request.UserID,
		ProposalID:  proposalID,
		ResourceID:  request.ResourceID,
		AccessLevel: request.AccessLevel,
	})
	if err != nil {
		return fmt.Errorf("encode access request: %w", err)
	}

	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(proposalID),
		Value: body,
	})
}
