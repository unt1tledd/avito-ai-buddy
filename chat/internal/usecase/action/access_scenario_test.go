package action

import (
	"context"
	"errors"
	"testing"

	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/usecase/action/mocks"

	"go.uber.org/mock/gomock"
)

func TestInternCannotConfirmAnotherInternsProposal(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	actions := mocks.NewMockrepository(ctrl)
	outbox := mocks.NewMockoutboxRepository(ctrl)
	auth := validatorFunc(func(_ context.Context, token string) (int64, error) {
		if token != "intern-8-session" {
			t.Fatalf("validated token = %q, want intern-8-session", token)
		}
		return 8, nil
	})
	tx := transactorFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	actions.EXPECT().GetByID(ctx, int64(8), int64(42)).Return(nil, nil)

	service := NewService(actions, outbox, auth, tx)
	_, err := service.ConfirmProposal(ctx, "intern-8-session", "42")
	if !errors.Is(err, chaterrors.ErrProposalNotFound) {
		t.Fatalf("ConfirmProposal() error = %v, want %v", err, chaterrors.ErrProposalNotFound)
	}
}
