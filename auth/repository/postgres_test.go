package auth_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"avito-ai-buddy/auth/entity"
	repository "avito-ai-buddy/auth/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type queryRowStub struct {
	repository.DB
	queryRow func(args []any) pgx.Row
}

func (s queryRowStub) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	return s.queryRow(args)
}

type rowStub func(dest ...any) error

func (scan rowStub) Scan(dest ...any) error {
	return scan(dest...)
}

func TestPostgresRepository(t *testing.T) {
	t.Parallel()

	user := entity.User{
		ID:           42,
		Email:        "test@example.com",
		PasswordHash: "stored-hash",
		CreatedAt:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	}
	queryErr := errors.New("database unavailable")
	noRows := fmt.Errorf("query returned no user: %w", pgx.ErrNoRows)
	operations := []struct {
		name       string
		args       []any
		wantNoRows error
		call       func(t *testing.T, db repository.DB) (entity.User, error)
	}{
		{
			name:       "find user by email",
			args:       []any{user.Email},
			wantNoRows: entity.ErrUserNotFound,
			call: func(t *testing.T, db repository.DB) (entity.User, error) {
				return repository.NewPostgresRepository(db).GetUserByEmail(t.Context(), user.Email)
			},
		},
		{
			name:       "create user",
			args:       []any{user.Email, user.PasswordHash},
			wantNoRows: entity.ErrUserAlreadyExists,
			call: func(t *testing.T, db repository.DB) (entity.User, error) {
				return repository.NewPostgresRepository(db).CreateUser(t.Context(), user.Email, user.PasswordHash)
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name    string
				scanErr error
				want    entity.User
				wantErr error
			}{
				{name: "success", want: user},
				{name: "no rows", scanErr: noRows, wantErr: operation.wantNoRows},
				{name: "database failure", scanErr: queryErr, wantErr: queryErr},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					db := queryRowStub{queryRow: func(args []any) pgx.Row {
						require.Equal(t, operation.args, args)
						return rowStub(func(dest ...any) error {
							if tt.scanErr != nil {
								return tt.scanErr
							}
							*dest[0].(*int64) = user.ID
							*dest[1].(*string) = user.Email
							*dest[2].(*string) = user.PasswordHash
							*dest[3].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: user.CreatedAt, Valid: true}
							return nil
						})
					}}

					got, err := operation.call(t, db)

					require.ErrorIs(t, err, tt.wantErr)
					require.Equal(t, tt.want, got)
				})
			}
		})
	}
}
