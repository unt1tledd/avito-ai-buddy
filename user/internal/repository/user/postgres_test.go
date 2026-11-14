package user_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"avito-ai-buddy/user/internal/entity"
	"avito-ai-buddy/user/internal/repository/user"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type dbStub struct {
	user.DB
	tx       pgx.Tx
	queryRow func(string, ...any) pgx.Row
}

func (d dbStub) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

func (d dbStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	return d.queryRow(query, args...)
}

type rowStub func(...any) error

func (r rowStub) Scan(dest ...any) error { return r(dest...) }

type txStub struct {
	pgx.Tx
	exec       func(string) (pgconn.CommandTag, error)
	queryRow   func(string) pgx.Row
	committed  bool
	rolledBack bool
}

func (tx *txStub) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	return tx.exec(query)
}

func (tx *txStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	return tx.queryRow(query)
}

func (tx *txStub) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *txStub) Rollback(context.Context) error {
	if tx.committed || tx.rolledBack {
		return pgx.ErrTxClosed
	}
	tx.rolledBack = true
	return nil
}

func TestCreateTransaction(t *testing.T) {
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	profile := entity.Profile{UserID: 42, FirstName: "Ivan", LastName: "Ivanov"}
	internship := entity.Internship{TeamID: 7, Track: "Backend", StartDate: start, PlannedEndDate: start.AddDate(0, 1, 0)}
	for _, tc := range []struct {
		name          string
		membershipErr error
		insertErr     error
		wantErr       error
	}{
		{name: "commit successful creation"},
		{
			name:          "roll back when team is missing",
			membershipErr: &pgconn.PgError{Code: "23503", ConstraintName: "team_members_team_fk"},
			wantErr:       entity.ErrTeamNotFound,
		},
		{
			name:      "roll back when mentor is outside the team",
			insertErr: &pgconn.PgError{Code: "23503", ConstraintName: "internships_mentor_membership_fk"},
			wantErr:   entity.ErrMentorNotInTeam,
		},
		{name: "roll back duplicate internship", insertErr: pgx.ErrNoRows, wantErr: entity.ErrInternshipExists},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &txStub{
				exec: func(query string) (pgconn.CommandTag, error) {
					switch {
					case strings.HasPrefix(query, "-- name: InsertProfile "):
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					case strings.HasPrefix(query, "-- name: EnsureTeamMembership "):
						return pgconn.NewCommandTag("INSERT 0 1"), tc.membershipErr
					default:
						t.Fatalf("unexpected Exec query: %s", query)
						return pgconn.CommandTag{}, nil
					}
				},
				queryRow: func(query string) pgx.Row {
					if tc.membershipErr != nil || !strings.HasPrefix(query, "-- name: CreateInternship ") {
						t.Fatalf("unexpected QueryRow query: %s", query)
					}
					return rowStub(func(dest ...any) error {
						if tc.insertErr != nil {
							return tc.insertErr
						}
						*dest[0].(*int64) = 101
						return nil
					})
				},
			}
			id, err := user.NewPostgresRepository(dbStub{tx: tx}).Create(t.Context(), profile, internship)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Create error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if id != 101 || !tx.committed || tx.rolledBack {
					t.Fatalf("id=%d, committed=%t, rolledBack=%t, want successful commit", id, tx.committed, tx.rolledBack)
				}
			} else if id != 0 || tx.committed || !tx.rolledBack {
				t.Fatalf("id=%d, committed=%t, rolledBack=%t, want rollback", id, tx.committed, tx.rolledBack)
			}
		})
	}
}

func TestGetMetadataQueryErrors(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name    string
		scanErr error
		wantErr error
	}{
		{name: "no rows", scanErr: pgx.ErrNoRows, wantErr: entity.ErrMetadataNotFound},
		{name: "database error", scanErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := dbStub{queryRow: func(query string, args ...any) pgx.Row {
				if !strings.HasPrefix(query, "-- name: GetMetadata ") || len(args) != 1 || args[0] != int64(42) {
					t.Fatalf("unexpected metadata query: %s, args %v", query, args)
				}
				return rowStub(func(...any) error { return tc.scanErr })
			}}
			got, err := user.NewPostgresRepository(db).GetMetadata(t.Context(), 42)
			if got != (entity.Metadata{}) || !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetMetadata = (%+v, %v), want empty metadata and %v", got, err, tc.wantErr)
			}
		})
	}
}
