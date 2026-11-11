package conversation

import (
	"errors"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/repository/conversation/mocks"
	sqlc "avito-ai-buddy/chat/internal/repository/conversation/sqlc"

	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"
)

func TestGetOrCreate(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	repo := &PostgresRepository{queries: queries}
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	queries.EXPECT().GetOrCreate(ctx, int64(7)).Return(sqlc.ChatConversation{
		ID: 21, UserID: 7, CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
	}, nil)

	got, err := repo.GetOrCreate(ctx, 7)
	want := entity.Conversation{ID: 21, UserID: 7, CreatedAt: createdAt}
	if err != nil || got == nil || *got != want {
		t.Fatalf("GetOrCreate() = %+v, %v, want %+v, nil", got, err, want)
	}

	dbErr := errors.New("database unavailable")
	queries.EXPECT().GetOrCreate(ctx, int64(8)).Return(sqlc.ChatConversation{}, dbErr)
	got, err = repo.GetOrCreate(ctx, 8)
	if got != nil || !errors.Is(err, dbErr) {
		t.Fatalf("GetOrCreate() = %+v, %v, want nil, %v", got, err, dbErr)
	}
}
