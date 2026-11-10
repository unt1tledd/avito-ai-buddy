package outbox

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/repository/outbox/mocks"
	sqlc "avito-ai-buddy/chat/internal/repository/outbox/sqlc"

	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"
)

func TestSendOutboxMessage(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	message := entity.OutboxMessage{
		IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`),
	}
	queries.EXPECT().SendOutboxMessage(ctx, sqlc.SendOutboxMessageParams{
		IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`),
	}).Return(nil)

	if err := (&PostgresRepository{queries: queries}).SendOutboxMessage(ctx, message); err != nil {
		t.Fatalf("SendOutboxMessage() = %v, want nil", err)
	}
}

func TestGetMessages(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	ttl := 1500 * time.Millisecond
	queries.EXPECT().GetOutboxMessages(ctx, sqlc.GetOutboxMessagesParams{
		BatchSize: 3, InProgressTtl: pgtype.Interval{Microseconds: 1500000, Valid: true},
	}).Return([]sqlc.GetOutboxMessagesRow{
		{IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`)},
		{IdempotencyKey: "43", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":43}`)},
	}, nil)

	got, err := (&PostgresRepository{queries: queries}).GetMessages(ctx, 3, ttl)
	want := []entity.OutboxMessage{
		{IdempotencyKey: "42", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":42}`)},
		{IdempotencyKey: "43", Kind: entity.OutboxKindCreateAccessRequest, Data: []byte(`{"proposal_id":43}`)},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetMessages() = %+v, %v, want %+v, nil", got, err, want)
	}
}

func TestMarkAsProcessed(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	keys := []string{"42", "43"}
	queries.EXPECT().MarkOutboxMessagesAsProcessed(ctx, keys).Return(nil)

	if err := (&PostgresRepository{queries: queries}).MarkAsProcessed(ctx, keys); err != nil {
		t.Fatalf("MarkAsProcessed() = %v, want nil", err)
	}
}

func TestMarkAsRetryable(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	keys := []string{"42", "43"}
	dbErr := errors.New("database unavailable")
	queries.EXPECT().MarkOutboxMessagesAsRetryable(ctx, keys).Return(dbErr)

	if err := (&PostgresRepository{queries: queries}).MarkAsRetryable(ctx, keys); !errors.Is(err, dbErr) {
		t.Fatalf("MarkAsRetryable() = %v, want %v", err, dbErr)
	}
}

func TestOutboxReadFailureIsNotAnEmptyQueue(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	dbErr := errors.New("database unavailable")
	queries.EXPECT().GetOutboxMessages(ctx, sqlc.GetOutboxMessagesParams{
		BatchSize: 10, InProgressTtl: pgtype.Interval{Microseconds: time.Minute.Microseconds(), Valid: true},
	}).Return(nil, dbErr)

	got, err := (&PostgresRepository{queries: queries}).GetMessages(ctx, 10, time.Minute)
	if got != nil || !errors.Is(err, dbErr) {
		t.Fatalf("GetMessages() = %+v, %v, want nil, %v", got, err, dbErr)
	}
}
