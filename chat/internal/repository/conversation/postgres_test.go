package conversation

import (
	"errors"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/repository/conversation/mocks"
	sqlc "avito-ai-buddy/chat/internal/repository/conversation/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGetOrCreate(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	repo := &PostgresRepository{queries: queries}
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	queries.EXPECT().GetOrCreate(ctx, int64(7)).Return(sqlc.GetOrCreateRow{
		ID: 21, UserID: 7, CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
	}, nil)

	got, err := repo.GetOrCreate(ctx, 7)
	want := entity.Conversation{ID: 21, UserID: 7, CreatedAt: createdAt}
	if err != nil || got == nil || *got != want {
		t.Fatalf("GetOrCreate() = %+v, %v, want %+v, nil", got, err, want)
	}

	dbErr := errors.New("database unavailable")
	queries.EXPECT().GetOrCreate(ctx, int64(8)).Return(sqlc.GetOrCreateRow{}, dbErr)
	got, err = repo.GetOrCreate(ctx, 8)
	if got != nil || !errors.Is(err, dbErr) {
		t.Fatalf("GetOrCreate() = %+v, %v, want nil, %v", got, err, dbErr)
	}
}

func TestGetOrCreateSelectedConversation(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name, id string
		queryErr error
		code     codes.Code
	}{
		{name: "owned conversation", id: "21"},
		{name: "non-numeric id", id: "abc", code: codes.InvalidArgument},
		{name: "zero id", id: "0", code: codes.InvalidArgument},
		{name: "missing or not owned", id: "21", queryErr: pgx.ErrNoRows, code: codes.NotFound},
		{name: "database error", id: "21", queryErr: dbErr, code: codes.Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-conversation-id", tc.id))
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			repo := &PostgresRepository{queries: queries}
			if tc.code != codes.InvalidArgument {
				queries.EXPECT().GetByID(ctx, sqlc.GetByIDParams{ID: 21, UserID: 7}).
					Return(sqlc.GetByIDRow{ID: 21, UserID: 7}, tc.queryErr)
			}
			got, err := repo.GetOrCreate(ctx, 7)
			if status.Code(err) != tc.code {
				t.Fatalf("GetOrCreate() error = %v, want %v", err, tc.code)
			}
			if tc.code == codes.OK {
				if got == nil || got.ID != 21 || got.UserID != 7 {
					t.Fatalf("GetOrCreate() = %+v, want conversation 21 for user 7", got)
				}
			} else if got != nil || (tc.queryErr == dbErr && !errors.Is(err, dbErr)) {
				t.Fatalf("GetOrCreate() = %+v, %v", got, err)
			}
		})
	}
}

func TestConversationRepositoryWritesAndListsForOwner(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	repo := &PostgresRepository{queries: queries}
	createdAt := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	dbTime := pgtype.Timestamptz{Time: createdAt, Valid: true}
	want := entity.ConversationSummary{ID: 21, Title: "Git", CreatedAt: createdAt}
	const ownerID int64 = 7

	queries.EXPECT().Create(ctx, ownerID).Return(sqlc.CreateRow{ID: 21, Title: "Git", CreatedAt: dbTime}, nil)
	created, err := repo.Create(ctx, ownerID)
	if err != nil || created != want {
		t.Fatalf("Create() = %+v, %v, want %+v", created, err, want)
	}

	queries.EXPECT().List(ctx, ownerID).Return([]sqlc.ListRow{{ID: 21, Title: "Git", CreatedAt: dbTime}}, nil)
	items, err := repo.List(ctx, ownerID)
	if err != nil || len(items) != 1 || items[0] != want {
		t.Fatalf("List() = %+v, %v, want [%+v]", items, err, want)
	}

	dbErr := errors.New("database unavailable")
	queries.EXPECT().List(ctx, ownerID).Return(nil, dbErr)
	if items, err := repo.List(ctx, ownerID); items != nil || !errors.Is(err, dbErr) {
		t.Fatalf("List() = %+v, %v, want nil, %v", items, err, dbErr)
	}

	for _, affected := range []int64{1, 0} {
		queries.EXPECT().Delete(ctx, sqlc.DeleteParams{ID: 21, UserID: ownerID}).Return(affected, nil)
		got, err := repo.Delete(ctx, ownerID, 21)
		if err != nil || got != (affected > 0) {
			t.Errorf("Delete() = %v, %v for %d affected rows", got, err, affected)
		}
	}

	queries.EXPECT().DeleteAll(ctx, ownerID).Return(dbErr)
	if err := repo.DeleteAll(ctx, ownerID); !errors.Is(err, dbErr) {
		t.Errorf("DeleteAll() error = %v, want %v", err, dbErr)
	}
}
