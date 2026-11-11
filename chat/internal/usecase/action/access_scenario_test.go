package action

import (
	"context"
	"errors"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
)

func TestInternCannotConfirmAnotherInternsProposal(t *testing.T) {
	ctx := t.Context()
	actions := &fakeActions{
		getByID: func(context.Context, int64, int64) (*entity.Action, error) { return nil, nil },
		saveAccess: func(context.Context, int64, entity.AccessRequest) error {
			t.Error("SaveAccessRequest() called for a foreign proposal")
			return nil
		},
	}
	outbox := &fakeOutbox{
		send: func(context.Context, entity.OutboxMessage) error {
			t.Error("SendOutboxMessage() called for a foreign proposal")
			return nil
		},
		getStatus: func(context.Context, string) (string, int32, error) {
			t.Error("GetMessageStatus() called for a foreign proposal")
			return "", 0, nil
		},
		reset: func(context.Context, string) error {
			t.Error("ResetMessage() called for a foreign proposal")
			return nil
		},
	}
	auth := validatorFunc(func(_ context.Context, token string) (int64, error) {
		if token != "intern-8-session" {
			t.Fatalf("validated token = %q, want intern-8-session", token)
		}
		return 8, nil
	})
	tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })

	service := NewService(actions, outbox, auth, tx)
	_, err := service.ConfirmProposal(ctx, "intern-8-session", "42")
	if !errors.Is(err, chaterrors.ErrProposalNotFound) {
		t.Fatalf("ConfirmProposal() error = %v, want %v", err, chaterrors.ErrProposalNotFound)
	}
}
