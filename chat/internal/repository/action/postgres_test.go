package action

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/repository/action/mocks"
	sqlc "avito-ai-buddy/chat/internal/repository/action/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"
)

func TestCreate(t *testing.T) {
	ctx := t.Context()
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	proposal := entity.Action{MessageID: 12, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read"}
	queries.EXPECT().Create(ctx, sqlc.CreateParams{
		MessageID: 12, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
	}).Return(int64(42), nil)

	got, err := (&PostgresRepository{queries: queries}).Create(ctx, proposal)
	if err != nil || got != 42 {
		t.Fatalf("Create() = %d, %v, want 42, nil", got, err)
	}
}

func TestGetByID(t *testing.T) {
	dbErr := errors.New("database unavailable")
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		row     sqlc.GetByIDRow
		dbErr   error
		want    *entity.Action
		wantErr error
	}{
		{
			name: "confirmed proposal",
			row: sqlc.GetByIDRow{
				ID: 42, MessageID: 12, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
				TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved",
				CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
			},
			want: &entity.Action{
				ID: 42, MessageID: 12, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read",
				CreatedAt:     createdAt,
				AccessRequest: &entity.AccessRequest{TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved"},
			},
		},
		{
			name: "unconfirmed proposal",
			row:  sqlc.GetByIDRow{ID: 42, MessageID: 12},
			want: &entity.Action{ID: 42, MessageID: 12},
		},
		{name: "missing proposal", dbErr: fmt.Errorf("query proposal: %w", pgx.ErrNoRows)},
		{name: "database error", dbErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			queries.EXPECT().GetByID(ctx, sqlc.GetByIDParams{ID: 42, UserID: 7}).Return(tc.row, tc.dbErr)

			got, err := (&PostgresRepository{queries: queries}).GetByID(ctx, 7, 42)
			if !reflect.DeepEqual(got, tc.want) || !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetByID() = %+v, %v, want %+v, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestSaveAccessRequest(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name    string
		rows    int64
		dbErr   error
		wantErr error
	}{
		{name: "saved", rows: 1},
		{name: "proposal missing", wantErr: chaterrors.ErrProposalNotFound},
		{name: "database error", dbErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			request := entity.AccessRequest{TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved"}
			queries.EXPECT().SaveAccessRequest(ctx, sqlc.SaveAccessRequestParams{
				ID: 42, TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved",
			}).Return(tc.rows, tc.dbErr)

			err := (&PostgresRepository{queries: queries}).SaveAccessRequest(ctx, 42, request)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SaveAccessRequest() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
