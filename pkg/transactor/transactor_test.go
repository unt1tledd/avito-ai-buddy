package transactor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

type transactionRecorder struct {
	pgx.Tx
	calls     []string
	beginErr  error
	commitErr error
}

func (r *transactionRecorder) Begin(context.Context) (pgx.Tx, error) {
	r.calls = append(r.calls, "begin")
	return r, r.beginErr
}

func (r *transactionRecorder) Commit(context.Context) error {
	r.calls = append(r.calls, "commit")
	return r.commitErr
}

func (r *transactionRecorder) Rollback(context.Context) error {
	r.calls = append(r.calls, "rollback")
	return nil
}

func TestWithTxLifecycle(t *testing.T) {
	failure := errors.New("transaction failure")
	for _, tc := range []struct {
		name                        string
		beginErr, runErr, commitErr error
		wantErr                     error
		wantCalls                   []string
	}{
		{name: "commit", wantCalls: []string{"begin", "run", "commit"}},
		{name: "rollback", runErr: failure, wantErr: failure, wantCalls: []string{"begin", "run", "rollback"}},
		{name: "begin error", beginErr: failure, wantErr: failure, wantCalls: []string{"begin"}},
		{name: "commit error", commitErr: failure, wantErr: failure, wantCalls: []string{"begin", "run", "commit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &transactionRecorder{beginErr: tc.beginErr, commitErr: tc.commitErr}
			transactor := &transactorImpl{db: recorder}
			err := transactor.WithTx(t.Context(), func(ctx context.Context) error {
				recorder.calls = append(recorder.calls, "run")
				if tx, err := ExtractTx(ctx); err != nil || tx != recorder {
					t.Errorf("ExtractTx() = %v, %v, want active transaction", tx, err)
				}
				return tc.runErr
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("WithTx() error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(recorder.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", recorder.calls, tc.wantCalls)
			}
		})
	}
}

func TestWithTxReusesOuterTransaction(t *testing.T) {
	recorder := &transactionRecorder{}
	transactor := &transactorImpl{db: recorder}
	failure := errors.New("nested operation failed")

	err := transactor.WithTx(t.Context(), func(ctx context.Context) error {
		return transactor.WithTx(ctx, func(nested context.Context) error {
			tx, err := ExtractTx(nested)
			if err != nil || tx != recorder {
				t.Errorf("ExtractTx() = %v, %v, want outer transaction", tx, err)
			}
			return failure
		})
	})

	if !errors.Is(err, failure) {
		t.Errorf("WithTx() error = %v, want %v", err, failure)
	}
	if want := []string{"begin", "rollback"}; !reflect.DeepEqual(recorder.calls, want) {
		t.Errorf("calls = %v, want %v", recorder.calls, want)
	}
}

func TestNewTransactorUsesPoolAndContextHasNoTransactionByDefault(t *testing.T) {
	if transactor := NewTransactor(nil); transactor == nil {
		t.Fatal("NewTransactor() returned nil")
	}
	if tx, err := ExtractTx(t.Context()); tx != nil || !errors.Is(err, ErrTxNotFound) {
		t.Errorf("ExtractTx() = %v, %v, want nil, ErrTxNotFound", tx, err)
	}
}
