package action

import (
	"errors"
	"testing"

	"avito-ai-buddy/chat/internal/controller/action/mocks"
	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"

	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestConfirmProposal(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer session"))
	actions := mocks.NewMockactionService(gomock.NewController(t))
	actions.EXPECT().ConfirmProposal(ctx, "session", "42").Return(entity.AccessRequest{
		TaskID: "task-1", ExternalRequestID: "request-1", Status: "approved",
	}, nil)

	got, err := NewServer(actions, zap.NewNop()).ConfirmProposal(ctx, &chatpb.ConfirmProposalRequest{ProposalId: "42"})
	want := &chatpb.ConfirmProposalResponse{AccessRequest: &chatpb.AccessRequest{
		TaskId: "task-1", ExternalRequestId: "request-1", Status: "approved",
	}}
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("ConfirmProposal() = %v, %v, want %v, nil", got, err, want)
	}
}

func TestConfirmProposalErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		proposalID string
		token      string
		serviceErr error
		want       codes.Code
	}{
		{name: "blank ID", proposalID: " ", token: "session", want: codes.InvalidArgument},
		{name: "missing token", proposalID: "42", want: codes.Unauthenticated},
		{name: "invalid IDs", proposalID: "bad", token: "session", serviceErr: chaterrors.ErrMissingConfirmationIDs, want: codes.InvalidArgument},
		{name: "missing proposal", proposalID: "42", token: "session", serviceErr: chaterrors.ErrProposalNotFound, want: codes.NotFound},
		{name: "expired proposal", proposalID: "42", token: "session", serviceErr: chaterrors.ErrProposalExpired, want: codes.FailedPrecondition},
		{name: "invalid token", proposalID: "42", token: "session", serviceErr: chaterrors.ErrInvalidToken, want: codes.Unauthenticated},
		{name: "auth unavailable", proposalID: "42", token: "session", serviceErr: chaterrors.ErrAuthUnavailable, want: codes.Unavailable},
		{name: "unexpected error", proposalID: "42", token: "session", serviceErr: errors.New("database unavailable"), want: codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.token != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+tc.token))
			}
			actions := mocks.NewMockactionService(gomock.NewController(t))
			if tc.serviceErr != nil {
				actions.EXPECT().ConfirmProposal(ctx, tc.token, tc.proposalID).Return(entity.AccessRequest{}, tc.serviceErr)
			}

			got, err := NewServer(actions, zap.NewNop()).ConfirmProposal(ctx, &chatpb.ConfirmProposalRequest{ProposalId: tc.proposalID})
			if got != nil || status.Code(err) != tc.want {
				t.Fatalf("ConfirmProposal() = %v, %v, want nil, %s", got, err, tc.want)
			}
		})
	}
}
