package action

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/port"
)

const testProposalTTL = 24 * time.Hour

type validatorFunc func(context.Context, string) (int64, error)

func (f validatorFunc) ValidateToken(ctx context.Context, token string) (int64, error) {
	return f(ctx, token)
}

type transactorFunc func(context.Context, func(context.Context) error) error

func (f transactorFunc) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return f(ctx, fn)
}

type fakeActions struct {
	getByID    func(context.Context, int64, int64) (*entity.Action, error)
	saveAccess func(context.Context, int64, entity.AccessRequest) error
}

func (a *fakeActions) GetByID(ctx context.Context, userID, proposalID int64) (*entity.Action, error) {
	return a.getByID(ctx, userID, proposalID)
}

func (a *fakeActions) SaveAccessRequest(ctx context.Context, proposalID int64, request entity.AccessRequest) error {
	return a.saveAccess(ctx, proposalID, request)
}

type fakeOutbox struct {
	send      func(context.Context, entity.OutboxMessage) error
	getStatus func(context.Context, string) (string, int32, error)
	reset     func(context.Context, string) error
}

func (o *fakeOutbox) SendOutboxMessage(ctx context.Context, message entity.OutboxMessage) error {
	return o.send(ctx, message)
}

func (o *fakeOutbox) GetMessageStatus(ctx context.Context, idempotencyKey string) (string, int32, error) {
	return o.getStatus(ctx, idempotencyKey)
}

func (o *fakeOutbox) ResetMessage(ctx context.Context, idempotencyKey string) error {
	return o.reset(ctx, idempotencyKey)
}

func TestConfirmProposalEnqueuesAccessRequest(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name      string
		saveErr   error
		outboxErr error
	}{
		{name: "queued"},
		{name: "save failed", saveErr: dbErr},
		{name: "enqueue failed", outboxErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			txCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			proposal := entity.Action{ID: 42, ResourceID: "repo", AccessLevel: "read", CreatedAt: time.Now()}
			pending := entity.AccessRequest{Status: "pending_dispatch"}
			var savedRequest *entity.AccessRequest
			var queued *entity.OutboxMessage
			actions := &fakeActions{
				getByID: func(context.Context, int64, int64) (*entity.Action, error) { return &proposal, nil },
				saveAccess: func(_ context.Context, id int64, request entity.AccessRequest) error {
					if id != 42 {
						t.Errorf("SaveAccessRequest() proposal = %d, want 42", id)
					}
					if tc.saveErr == nil {
						savedRequest = &request
					}
					return tc.saveErr
				},
			}
			wantPayload := port.CreateAccessRequest{
				UserID: "7", ProposalID: 42, ResourceID: "repo", AccessLevel: "read",
			}
			outbox := &fakeOutbox{
				send: func(_ context.Context, message entity.OutboxMessage) error {
					var payload port.CreateAccessRequest
					if message.IdempotencyKey != "42" ||
						message.Kind != entity.OutboxKindCreateAccessRequest ||
						json.Unmarshal(message.Data, &payload) != nil || payload.DispatchID == "" {
						t.Errorf("unexpected outbox message: %+v", message)
					}
					payload.DispatchID = ""
					if payload != wantPayload {
						t.Errorf("unexpected command: %+v", payload)
					}
					if tc.outboxErr == nil {
						queued = &message
					}
					return tc.outboxErr
				},
				getStatus: func(context.Context, string) (string, int32, error) {
					t.Error("GetMessageStatus() called for a fresh proposal")
					return "", 0, nil
				},
				reset: func(context.Context, string) error {
					t.Error("ResetMessage() called for a fresh proposal")
					return nil
				},
			}

			auth := validatorFunc(func(gotCtx context.Context, token string) (int64, error) {
				if gotCtx != ctx || token != "session" {
					t.Fatalf("ValidateToken() received context %v and token %q", gotCtx, token)
				}
				return 7, nil
			})
			tx := transactorFunc(func(gotCtx context.Context, fn func(context.Context) error) error {
				if gotCtx != ctx {
					t.Fatalf("WithTx() context = %v, want %v", gotCtx, ctx)
				}
				err := fn(txCtx)
				if err != nil {
					// A real transaction rolls the writes back on failure.
					savedRequest, queued = nil, nil
				}
				return err
			})

			got, err := NewService(actions, outbox, auth, tx, testProposalTTL).ConfirmProposal(ctx, "session", " 42 ")
			want, wantErr := pending, tc.saveErr
			if wantErr == nil {
				wantErr = tc.outboxErr
			}
			if wantErr != nil {
				want = entity.AccessRequest{}
				if savedRequest != nil || queued != nil {
					t.Error("state was persisted despite the failed transaction")
				}
			} else if savedRequest == nil || queued == nil {
				t.Error("request was not saved and queued")
			}
			if got != want || !errors.Is(err, wantErr) {
				t.Fatalf("ConfirmProposal() = %+v, %v, want %+v, %v", got, err, want, wantErr)
			}
		})
	}
}

func TestConfirmProposalDoesNotEnqueueAgain(t *testing.T) {
	approved := entity.AccessRequest{TaskID: "task-1", Status: "approved"}
	pending := entity.AccessRequest{Status: "pending_dispatch"}
	expired := time.Now().Add(-testProposalTTL - time.Minute)
	for _, tc := range []struct {
		name     string
		proposal *entity.Action
		status   string
		want     entity.AccessRequest
		wantErr  error
	}{
		{
			name:     "already dispatched",
			proposal: &entity.Action{ID: 42, CreatedAt: expired, AccessRequest: &pending},
			status:   entity.OutboxStatusSuccess,
			want:     pending,
		},
		{
			name:     "dispatch still in progress",
			proposal: &entity.Action{ID: 42, CreatedAt: expired, AccessRequest: &pending},
			status:   entity.OutboxStatusInProgress,
			want:     pending,
		},
		{
			name:     "expired approved proposal still returns its status",
			proposal: &entity.Action{ID: 42, CreatedAt: expired, AccessRequest: &approved},
			want:     approved,
		},
		{
			name:     "expired proposal cannot be confirmed",
			proposal: &entity.Action{ID: 42, CreatedAt: expired},
			wantErr:  chaterrors.ErrProposalExpired,
		},
		{
			name:     "expired proposal cannot restart failed dispatch",
			proposal: &entity.Action{ID: 42, CreatedAt: expired, AccessRequest: &pending},
			status:   entity.OutboxStatusFailed,
			wantErr:  chaterrors.ErrProposalExpired,
		},
		{name: "proposal not found", wantErr: chaterrors.ErrProposalNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			actions := &fakeActions{
				getByID: func(context.Context, int64, int64) (*entity.Action, error) { return tc.proposal, nil },
				saveAccess: func(context.Context, int64, entity.AccessRequest) error {
					t.Error("SaveAccessRequest() called on an already confirmed proposal")
					return nil
				},
			}
			outbox := &fakeOutbox{
				send: func(context.Context, entity.OutboxMessage) error {
					t.Error("SendOutboxMessage() called on an already confirmed proposal")
					return nil
				},
				getStatus: func(_ context.Context, key string) (string, int32, error) {
					if key != "42" {
						t.Errorf("GetMessageStatus() key = %q, want 42", key)
					}
					return tc.status, 0, nil
				},
				reset: func(context.Context, string) error {
					t.Error("ResetMessage() called on a live dispatch")
					return nil
				},
			}
			auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
			tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error {
				return fn(ctx)
			})

			got, err := NewService(actions, outbox, auth, tx, testProposalTTL).ConfirmProposal(ctx, "session", "42")
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ConfirmProposal() = %+v, %v, want %+v, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestConfirmProposalRequeuesFailedDispatch(t *testing.T) {
	ctx := t.Context()
	failed := entity.AccessRequest{TaskID: "task-1", Status: "failed_dispatch"}
	proposal := &entity.Action{ID: 42, CreatedAt: time.Now(), AccessRequest: &failed}
	var saved entity.AccessRequest
	var resetKey string
	var sent bool
	actions := &fakeActions{
		getByID: func(context.Context, int64, int64) (*entity.Action, error) { return proposal, nil },
		saveAccess: func(_ context.Context, id int64, request entity.AccessRequest) error {
			if id != 42 {
				t.Errorf("SaveAccessRequest() proposal = %d, want 42", id)
			}
			saved = request
			return nil
		},
	}
	outbox := &fakeOutbox{
		send: func(context.Context, entity.OutboxMessage) error {
			sent = true
			return nil
		},
		getStatus: func(context.Context, string) (string, int32, error) {
			return entity.OutboxStatusFailed, 5, nil
		},
		reset: func(_ context.Context, key string) error {
			resetKey = key
			return nil
		},
	}
	auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
	tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })

	got, err := NewService(actions, outbox, auth, tx, testProposalTTL).ConfirmProposal(ctx, "session", "42")
	want := entity.AccessRequest{TaskID: "task-1", Status: "pending_dispatch"}
	if err != nil || got != want {
		t.Fatalf("ConfirmProposal() = %+v, %v, want %+v, nil", got, err, want)
	}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved request = %+v, want %+v", saved, want)
	}
	if resetKey != "42" {
		t.Fatalf("reset key = %q, want 42", resetKey)
	}
	if !sent {
		t.Error("re-confirmation must replace the command's dispatch ID")
	}
}

func TestRetryAfterTaskFailureReplacesDispatchAttempt(t *testing.T) {
	request := entity.AccessRequest{Status: "failed"}
	proposal := &entity.Action{ID: 42, ResourceID: "wiki", AccessLevel: "read", CreatedAt: time.Now(), AccessRequest: &request}
	var command port.CreateAccessRequest
	var resets int
	actions := &fakeActions{
		getByID:    func(context.Context, int64, int64) (*entity.Action, error) { return proposal, nil },
		saveAccess: func(_ context.Context, _ int64, value entity.AccessRequest) error { request = value; return nil },
	}
	outbox := &fakeOutbox{
		getStatus: func(context.Context, string) (string, int32, error) { return entity.OutboxStatusSuccess, 0, nil },
		reset:     func(context.Context, string) error { resets++; return nil },
		send: func(_ context.Context, message entity.OutboxMessage) error {
			return json.Unmarshal(message.Data, &command)
		},
	}
	service := NewService(actions, outbox, validatorFunc(func(context.Context, string) (int64, error) { return 7, nil }), transactorFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }), testProposalTTL)
	if _, err := service.ConfirmProposal(t.Context(), "session", "42"); err != nil {
		t.Fatal(err)
	}
	first := command.DispatchID
	if first == "" || request.Status != "pending_dispatch" || resets != 1 {
		t.Fatalf("command=%+v, request=%+v, resets=%d", command, request, resets)
	}
	request.Status = "failed"
	if _, err := service.ConfirmProposal(t.Context(), "session", "42"); err != nil {
		t.Fatal(err)
	}
	if command.DispatchID == first || command.ProposalID != 42 || resets != 2 {
		t.Fatalf("retry=%+v, resets=%d", command, resets)
	}
}
