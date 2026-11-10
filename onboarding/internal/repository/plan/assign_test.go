package plan_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/repository/plan"
	"avito-ai-buddy/pkg/transactor"
)

func TestAssignPlan(t *testing.T) {
	const userID int64 = 42
	const track = "qa"
	assigned := entity.Plan{ID: 7, UserID: userID, Track: track}
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name                                    string
		beginErr, lockErr, assignErr, commitErr error
		wantErr                                 error
		wantCalls                               []string
	}{
		{name: "assign and commit", wantCalls: []string{"begin", "lock", "assign", "commit"}},
		{name: "unsupported track", lockErr: pgx.ErrNoRows, wantErr: onboardingerrors.ErrUnsupportedTrack, wantCalls: []string{"begin", "lock", "rollback"}},
		{name: "lock query fails", lockErr: dbErr, wantErr: dbErr, wantCalls: []string{"begin", "lock", "rollback"}},
		{name: "track conflict", assignErr: pgx.ErrNoRows, wantErr: onboardingerrors.ErrPlanTrackConflict, wantCalls: []string{"begin", "lock", "assign", "rollback"}},
		{name: "assignment fails", assignErr: dbErr, wantErr: dbErr, wantCalls: []string{"begin", "lock", "assign", "rollback"}},
		{name: "commit fails", commitErr: dbErr, wantErr: dbErr, wantCalls: []string{"begin", "lock", "assign", "commit"}},
		{name: "begin fails", beginErr: dbErr, wantErr: dbErr, wantCalls: []string{"begin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			tx := &assignmentTx{
				calls:     &calls,
				commitErr: tc.commitErr,
				query: func(sql string, args []any) pgx.Row {
					switch {
					case strings.HasPrefix(sql, "-- name: LockTrackTemplate "):
						calls = append(calls, "lock")
						if !reflect.DeepEqual(args, []any{track}) {
							t.Fatalf("lock args = %v, want [%s]", args, track)
						}
						return assignmentRow(func(dest ...any) error {
							if tc.lockErr != nil {
								return tc.lockErr
							}
							*dest[0].(*int64) = 1
							return nil
						})
					case strings.HasPrefix(sql, "-- name: AssignPlan "):
						calls = append(calls, "assign")
						if !reflect.DeepEqual(args, []any{userID, track}) {
							t.Fatalf("assign args = %v, want [%d %s]", args, userID, track)
						}
						return assignmentRow(func(dest ...any) error {
							if tc.assignErr != nil {
								return tc.assignErr
							}
							*dest[0].(*int64) = assigned.ID
							*dest[1].(*int64) = assigned.UserID
							*dest[2].(*string) = assigned.Track
							return nil
						})
					default:
						t.Fatalf("unexpected query: %s", sql)
						return nil
					}
				},
			}
			begin := assignmentBegin(func(context.Context) (pgx.Tx, error) {
				calls = append(calls, "begin")
				if tc.beginErr != nil {
					return nil, tc.beginErr
				}
				return tx, nil
			})
			repo := plan.NewPlanRepository(nil, transactor.NewTransactor(begin))
			got, err := repo.AssignPlan(context.Background(), userID, track)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AssignPlan error = %v, want %v", err, tc.wantErr)
			}
			want := assigned
			if tc.wantErr != nil {
				want = entity.Plan{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("AssignPlan = %+v, want %+v", got, want)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("transaction calls = %v, want %v", calls, tc.wantCalls)
			}
		})
	}
}

type assignmentBegin func(context.Context) (pgx.Tx, error)

func (begin assignmentBegin) Begin(ctx context.Context) (pgx.Tx, error) {
	return begin(ctx)
}

type assignmentTx struct {
	pgx.Tx
	calls     *[]string
	commitErr error
	query     func(string, []any) pgx.Row
}

func (tx *assignmentTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return tx.query(sql, args)
}

func (tx *assignmentTx) Commit(context.Context) error {
	*tx.calls = append(*tx.calls, "commit")
	return tx.commitErr
}

func (tx *assignmentTx) Rollback(context.Context) error {
	*tx.calls = append(*tx.calls, "rollback")
	return nil
}

type assignmentRow func(...any) error

func (row assignmentRow) Scan(dest ...any) error {
	return row(dest...)
}
