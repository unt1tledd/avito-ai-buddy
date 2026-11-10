package action

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/port"
	"avito-ai-buddy/chat/internal/usecase/action/mocks"

	"go.uber.org/mock/gomock"
)

type validatorFunc func(context.Context, string) (int64, error)

func (f validatorFunc) ValidateToken(ctx context.Context, token string) (int64, error) {
	return f(ctx, token)
}

type transactorFunc func(context.Context, func(context.Context) error) error

func (f transactorFunc) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return f(ctx, fn)
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

			ctrl := gomock.NewController(t)
			actions := mocks.NewMockrepository(ctrl)
			outbox := mocks.NewMockoutboxRepository(ctrl)
			proposal := entity.Action{ID: 42, ResourceID: "repo", AccessLevel: "read"}
			pending := entity.AccessRequest{Status: "pending_dispatch"}
			load := actions.EXPECT().GetByID(txCtx, int64(7), int64(42)).Return(&proposal, nil)
			save := actions.EXPECT().SaveAccessRequest(txCtx, int64(42), pending).Return(tc.saveErr).After(load)
			if tc.saveErr == nil {
				wantPayload := port.CreateAccessRequest{
					UserID: "7", ProposalID: 42, ResourceID: "repo", AccessLevel: "read",
				}
				outbox.EXPECT().SendOutboxMessage(txCtx, gomock.Cond(func(message entity.OutboxMessage) bool {
					var payload port.CreateAccessRequest
					return message.IdempotencyKey == "42" &&
						message.Kind == entity.OutboxKindCreateAccessRequest &&
						json.Unmarshal(message.Data, &payload) == nil && payload == wantPayload
				})).Return(tc.outboxErr).After(save)
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
				return fn(txCtx)
			})

			got, err := NewService(actions, outbox, auth, tx).ConfirmProposal(ctx, "session", " 42 ")
			want, wantErr := pending, tc.saveErr
			if wantErr == nil {
				wantErr = tc.outboxErr
			}
			if wantErr != nil {
				want = entity.AccessRequest{}
			}
			if got != want || !errors.Is(err, wantErr) {
				t.Fatalf("ConfirmProposal() = %+v, %v, want %+v, %v", got, err, want, wantErr)
			}
		})
	}
}

func TestConfirmProposalDoesNotEnqueueAgain(t *testing.T) {
	for _, tc := range []struct {
		name     string
		proposal *entity.Action
		want     entity.AccessRequest
		wantErr  error
	}{
		{
			name:     "already confirmed",
			proposal: &entity.Action{ID: 42, AccessRequest: &entity.AccessRequest{TaskID: "task-1", Status: "approved"}},
			want:     entity.AccessRequest{TaskID: "task-1", Status: "approved"},
		},
		{name: "proposal not found", wantErr: chaterrors.ErrProposalNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			ctrl := gomock.NewController(t)
			actions := mocks.NewMockrepository(ctrl)
			outbox := mocks.NewMockoutboxRepository(ctrl)
			actions.EXPECT().GetByID(ctx, int64(7), int64(42)).Return(tc.proposal, nil)
			auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
			tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error {
				return fn(ctx)
			})

			got, err := NewService(actions, outbox, auth, tx).ConfirmProposal(ctx, "session", "42")
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ConfirmProposal() = %+v, %v, want %+v, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
