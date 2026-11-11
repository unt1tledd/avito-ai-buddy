package message

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	sqlc "avito-ai-buddy/chat/internal/repository/message/sqlc"
	"avito-ai-buddy/pkg/transactor"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresRepository stores conversation messages in PostgreSQL.
type PostgresRepository struct {
	queries sqlc.Querier
}

// NewPostgresRepository builds the repository over a pgx pool or transaction.
func NewPostgresRepository(db sqlc.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: sqlc.New(db)}
}

// getQueries prefers the transaction from the context when present, so all
// statements of a usecase run atomically.
func (r *PostgresRepository) getQueries(ctx context.Context) sqlc.Querier {
	if tx, err := transactor.ExtractTx(ctx); err == nil {
		return sqlc.New(tx)
	}
	return r.queries
}

// Create inserts a message and returns its generated ID.
func (r *PostgresRepository) Create(ctx context.Context, message entity.Message) (int64, error) {
	sources, err := json.Marshal(message.Sources)
	if err != nil {
		return 0, fmt.Errorf("encode message sources: %w", err)
	}
	role := sqlc.ChatMessageRole(message.Role)
	if role == "" {
		role = sqlc.ChatMessageRoleUnspecified
	}
	return r.getQueries(ctx).Create(ctx, sqlc.CreateParams{
		ConversationID: message.ConversationID,
		Role:           role,
		Body:           message.Text,
		Sources:        sources,
		CreatedAt:      pgtype.Timestamptz{Time: message.CreatedAt, Valid: true},
	})
}

// CreateIdempotent inserts a message with an idempotency key. It returns
// false when a message with the same key already exists in the conversation.
func (r *PostgresRepository) CreateIdempotent(ctx context.Context, message entity.Message, idempotencyKey int64) (bool, error) {
	sources, err := json.Marshal(message.Sources)
	if err != nil {
		return false, fmt.Errorf("encode message sources: %w", err)
	}
	role := sqlc.ChatMessageRole(message.Role)
	if role == "" {
		role = sqlc.ChatMessageRoleUnspecified
	}
	_, err = r.getQueries(ctx).CreateIdempotentMessage(ctx, sqlc.CreateIdempotentMessageParams{
		ConversationID: message.ConversationID,
		Role:           role,
		Body:           message.Text,
		Sources:        sources,
		CreatedAt:      pgtype.Timestamptz{Time: message.CreatedAt, Valid: true},
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING skipped the insert: the message already exists.
		return false, nil
	}
	return err == nil, err
}

// GetOrCreateQuestion inserts the intern's question with an idempotency key
// and returns its ID together with the stored text, so the caller can detect
// a reused key with different content.
func (r *PostgresRepository) GetOrCreateQuestion(ctx context.Context, question entity.Message, idempotencyKey int64) (int64, string, error) {
	row, err := r.getQueries(ctx).GetOrCreateQuestion(ctx, sqlc.GetOrCreateQuestionParams{
		ConversationID: question.ConversationID,
		Body:           question.Text,
		CreatedAt:      pgtype.Timestamptz{Time: question.CreatedAt, Valid: true},
		IdempotencyKey: idempotencyKey,
	})
	return row.ID, row.Body, err
}

// ClaimReply acquires a lease to generate the reply for the question. A zero
// time means the question is already claimed or answered.
func (r *PostgresRepository) ClaimReply(ctx context.Context, questionID int64, lease time.Duration) (time.Time, error) {
	claim, err := r.getQueries(ctx).ClaimReply(ctx, sqlc.ClaimReplyParams{
		QuestionID:    questionID,
		LeaseDuration: pgtype.Interval{Microseconds: lease.Microseconds(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	return claim.Time, err
}

// FinishReply releases the claim, storing the reply ID. A zero replyID only
// releases the claim without saving a reply. It fails when the claim expired.
func (r *PostgresRepository) FinishReply(ctx context.Context, questionID int64, claimUntil time.Time, replyID int64) error {
	rows, err := r.getQueries(ctx).FinishReply(ctx, sqlc.FinishReplyParams{
		QuestionID:         questionID,
		ClaimUntil:         pgtype.Timestamptz{Time: claimUntil, Valid: true},
		AssistantMessageID: pgtype.Int8{Int64: replyID, Valid: replyID != 0},
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return chaterrors.ErrReplyInProgress
	}
	return nil
}

// GetReply returns the saved reply for the question, or nil when the reply
// has not been generated yet.
func (r *PostgresRepository) GetReply(ctx context.Context, questionID int64) (*entity.Message, error) {
	row, err := r.getQueries(ctx).GetReply(ctx, questionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	message, err := decodeMessage(sqlc.ListByConversationRow(row))
	if err != nil {
		return nil, err
	}
	return &message, nil
}

type historyPageSizeKey struct{}

// WithHistoryPageSize stores the default history page size, applied when the
// caller does not request an explicit one.
func WithHistoryPageSize(ctx context.Context, pageSize int32) context.Context {
	return context.WithValue(ctx, historyPageSizeKey{}, pageSize)
}

// ListByConversation returns a history page ordered from newest to oldest
// with a cursor for the next page. An invalid cursor yields an empty page.
func (r *PostgresRepository) ListByConversation(ctx context.Context, conversationID int64, pageSize int32, pageToken string) ([]entity.Message, string, error) {
	if pageSize == 0 {
		pageSize, _ = ctx.Value(historyPageSizeKey{}).(int32)
	}
	if pageSize <= 0 {
		return nil, "", fmt.Errorf("page size must be positive")
	}
	var beforeID int64
	if pageToken != "" {
		id, err := strconv.ParseInt(pageToken, 10, 64)
		if err != nil || id <= 0 {
			return nil, "", nil
		}
		beforeID = id
	}
	rows, err := r.getQueries(ctx).ListByConversation(ctx, sqlc.ListByConversationParams{
		ConversationID: conversationID,
		BeforeID:       beforeID,
		PageLimit:      int64(pageSize) + 1,
	})
	if err != nil {
		return nil, "", err
	}

	var messages []entity.Message
	for _, row := range rows {
		message, err := decodeMessage(row)
		if err != nil {
			return nil, "", err
		}
		messages = append(messages, message)
	}

	var nextPageToken string
	if len(messages) > int(pageSize) {
		messages = messages[:pageSize]
		nextPageToken = strconv.FormatInt(messages[len(messages)-1].ID, 10)
	}
	slices.Reverse(messages)
	return messages, nextPageToken, nil
}

func decodeMessage(row sqlc.ListByConversationRow) (entity.Message, error) {
	message := entity.Message{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Role:           entity.MessageRole(row.Role),
		Text:           row.Body,
		CreatedAt:      row.CreatedAt.Time,
	}
	if err := json.Unmarshal(row.Sources, &message.Sources); err != nil {
		return entity.Message{}, fmt.Errorf("decode message sources: %w", err)
	}
	if row.ProposalID != 0 {
		message.Proposal = &entity.Action{
			ID:            row.ProposalID,
			MessageID:     row.ID,
			ResourceID:    row.ResourceID,
			ResourceLabel: row.ResourceLabel,
			AccessLevel:   row.AccessLevel,
		}
		if row.Status != "" {
			message.Proposal.AccessRequest = &entity.AccessRequest{
				TaskID:            row.TaskID,
				ExternalRequestID: row.ExternalRequestID,
				Status:            row.Status,
			}
		}
	}
	return message, nil
}
