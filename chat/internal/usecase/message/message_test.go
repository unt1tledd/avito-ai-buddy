package message

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/usecase/message/mocks"

	"github.com/golang-jwt/jwt/v4"
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

const testIdempotencyKey int64 = 1001

func TestInternAsksForAccessDuringOnboarding(t *testing.T) {
	const question = "How do I get access?"
	aiErr := errors.New("AI unavailable")
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name         string
		withProposal bool
		aiErr        error
		assistantErr error
		proposalErr  error
		wantErr      error
		wantCause    error
	}{
		{name: "answer with proposal", withProposal: true},
		{name: "answer without proposal"},
		{name: "AI fails", aiErr: aiErr, wantErr: chaterrors.ErrGetAIAnswer, wantCause: aiErr},
		{name: "reply cannot be saved", withProposal: true, assistantErr: dbErr, wantErr: chaterrors.ErrSaveAssistantMessage, wantCause: dbErr},
		{name: "proposal save fails", withProposal: true, proposalErr: dbErr, wantErr: chaterrors.ErrSaveProposal, wantCause: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			txCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			ctrl := gomock.NewController(t)
			conversations := mocks.NewMockconversationService(ctrl)
			messages := mocks.NewMockmessageRepository(ctrl)
			actions := mocks.NewMockactionRepository(ctrl)
			users := mocks.NewMockuserClient(ctrl)
			onboarding := mocks.NewMockonboardingClient(ctrl)
			ai := mocks.NewMockaiClient(ctrl)

			history := []entity.Message{{ID: 10, ConversationID: 21, Role: entity.MessageRoleAssistant, Text: "Welcome"}}
			user := entity.UserContext{UserID: "7", TeamID: "platform", Role: "intern"}
			step := &entity.OnboardingStep{ID: "setup", Title: "Set up a local environment", ResourceID: "repo"}
			plans := []entity.OnboardingPlan{
				{ID: "common", Title: "General track", CurrentStep: step},
				{ID: "backend", Title: "Backend"},
			}
			sources := []entity.Source{{DocumentID: "doc-1", Title: "Guide", URL: "https://example.com/guide"}}
			answer := entity.Answer{Text: "Request access", Sources: sources}
			if tc.withProposal {
				answer.Proposal = &entity.Action{ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read"}
			}

			claimUntil := time.Now().Add(replyLease)
			calls := []any{
				conversations.EXPECT().GetOrCreate(ctx, int64(7)).Return(&entity.Conversation{ID: 21, UserID: 7}, nil),
				messages.EXPECT().GetOrCreateQuestion(ctx, matchNewMessage(entity.Message{
					ConversationID: 21, Role: entity.MessageRoleUser, Text: question,
				}), testIdempotencyKey).Return(int64(11), question, nil),
				messages.EXPECT().ClaimReply(gomock.Any(), int64(11), replyLease).Return(claimUntil, nil),
				messages.EXPECT().ListByConversation(gomock.Any(), int64(21), int32(30), "11").Return(history, "", nil),
				users.EXPECT().GetUserContext(gomock.Any(), "7").Return(entity.UserContext{TeamID: "platform", Role: "intern"}, nil),
				onboarding.EXPECT().GetOnboardingPlans(gomock.Any(), int64(7), "session").Return(plans, nil),
				ai.EXPECT().GetAnswer(gomock.Any(), entity.AnswerRequest{
					User: user, Message: question, History: history, OnboardingPlans: plans,
				}).Return(answer, tc.aiErr),
			}
			want := entity.Message{
				ConversationID: 21, Role: entity.MessageRoleAssistant, Text: "Request access", Sources: sources,
				Proposal: answer.Proposal,
			}
			if tc.aiErr == nil {
				calls = append(calls, messages.EXPECT().Create(txCtx, matchNewMessage(want)).Return(int64(12), tc.assistantErr))
				want.ID = 12
				if tc.withProposal && tc.assistantErr == nil {
					proposal := entity.Action{MessageID: 12, ResourceID: "repo", ResourceLabel: "Repository", AccessLevel: "read"}
					calls = append(calls, actions.EXPECT().Create(txCtx, proposal).Return(int64(42), tc.proposalErr))
					proposal.ID = 42
					want.Proposal = &proposal
				}
			}
			if tc.wantErr == nil {
				calls = append(calls, messages.EXPECT().FinishReply(txCtx, int64(11), claimUntil, int64(12)).Return(nil))
			} else {
				calls = append(calls, messages.EXPECT().FinishReply(gomock.Any(), int64(11), claimUntil, int64(0)).Return(nil))
			}
			gomock.InOrder(calls...)

			auth := validatorFunc(func(gotCtx context.Context, token string) (int64, error) {
				if gotCtx != ctx || token != "session" {
					t.Fatalf("ValidateToken() received context %v and token %q", gotCtx, token)
				}
				return 7, nil
			})
			tx := transactorFunc(func(_ context.Context, fn func(context.Context) error) error {
				if tc.aiErr != nil {
					t.Fatal("unexpected WithTx() call after AI failure")
				}
				return fn(txCtx)
			})

			got, err := NewService(conversations, messages, actions, users, onboarding, ai, auth, tx, 4000, 30).
				SendMessage(ctx, "session", question, testIdempotencyKey)
			if tc.wantErr != nil {
				if !reflect.DeepEqual(got, entity.Message{}) || !errors.Is(err, tc.wantErr) || !errors.Is(err, tc.wantCause) {
					t.Fatalf("SendMessage() = %+v, %v, want empty message and error wrapping %v and %v", got, err, tc.wantErr, tc.wantCause)
				}
				return
			}
			if err != nil || got.CreatedAt.IsZero() {
				t.Fatalf("SendMessage() = %+v, %v, want a dated assistant message", got, err)
			}
			want.CreatedAt = got.CreatedAt
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("SendMessage() = %+v, want %+v", got, want)
			}
		})
	}
}

func matchNewMessage(want entity.Message) gomock.Matcher {
	return gomock.Cond(func(got entity.Message) bool {
		if got.CreatedAt.IsZero() {
			return false
		}
		got.CreatedAt = time.Time{}
		return reflect.DeepEqual(got, want)
	})
}

func TestSendMessageWithExistingIdempotencyKey(t *testing.T) {
	const question = "How do I get access?"
	saved := entity.Message{ID: 12, Text: "Request access", Proposal: &entity.Action{ID: 42}}
	for _, tc := range []struct {
		name      string
		savedText string
		reply     *entity.Message
		wantErr   error
	}{
		{name: "replay saved answer", savedText: question, reply: &saved},
		{name: "reply in progress", savedText: question, wantErr: chaterrors.ErrReplyInProgress},
		{name: "key used for different text", savedText: "Different question", wantErr: chaterrors.ErrIdempotencyKeyReused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			ctrl := gomock.NewController(t)
			conversations := mocks.NewMockconversationService(ctrl)
			messages := mocks.NewMockmessageRepository(ctrl)
			conversations.EXPECT().GetOrCreate(ctx, int64(7)).Return(&entity.Conversation{ID: 21}, nil)
			messages.EXPECT().GetOrCreateQuestion(ctx, matchNewMessage(entity.Message{
				ConversationID: 21, Role: entity.MessageRoleUser, Text: question,
			}), testIdempotencyKey).Return(int64(11), tc.savedText, nil)
			if tc.savedText == question {
				messages.EXPECT().ClaimReply(gomock.Any(), int64(11), replyLease).Return(time.Time{}, nil)
				messages.EXPECT().GetReply(gomock.Any(), int64(11)).Return(tc.reply, nil)
			}
			auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
			reply, err := NewService(conversations, messages, nil, nil, nil, nil, auth, nil, 4000, 30).
				SendMessage(ctx, "session", question, testIdempotencyKey)
			want := entity.Message{}
			if tc.reply != nil {
				want = *tc.reply
			}
			if !errors.Is(err, tc.wantErr) || !reflect.DeepEqual(reply, want) {
				t.Fatalf("SendMessage() = %+v, %v, want %+v, %v", reply, err, want, tc.wantErr)
			}
		})
	}
}

func TestSendMessageRetriesFailedQuestion(t *testing.T) {
	const question = "How do I get access?"
	ctx := t.Context()
	txCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctrl := gomock.NewController(t)
	conversations := mocks.NewMockconversationService(ctrl)
	messages := mocks.NewMockmessageRepository(ctrl)
	users := mocks.NewMockuserClient(ctrl)
	onboarding := mocks.NewMockonboardingClient(ctrl)
	ai := mocks.NewMockaiClient(ctrl)
	firstClaim := time.Now().Add(replyLease)
	secondClaim := firstClaim.Add(time.Second)
	answerReq := entity.AnswerRequest{Message: question, User: entity.UserContext{UserID: "7"}}
	wantReply := entity.Message{ConversationID: 21, Role: entity.MessageRoleAssistant, Text: "Request access"}

	conversations.EXPECT().GetOrCreate(ctx, int64(7)).Return(&entity.Conversation{ID: 21}, nil).Times(2)
	messages.EXPECT().GetOrCreateQuestion(ctx, matchNewMessage(entity.Message{
		ConversationID: 21, Role: entity.MessageRoleUser, Text: question,
	}), testIdempotencyKey).Return(int64(11), question, nil).Times(2)
	messages.EXPECT().ListByConversation(gomock.Any(), int64(21), int32(30), "11").Return(nil, "", nil).Times(2)
	users.EXPECT().GetUserContext(gomock.Any(), "7").Return(entity.UserContext{}, nil).Times(2)
	onboarding.EXPECT().GetOnboardingPlans(gomock.Any(), int64(7), "session").Return(nil, nil).Times(2)
	gomock.InOrder(
		messages.EXPECT().ClaimReply(gomock.Any(), int64(11), replyLease).Return(firstClaim, nil),
		ai.EXPECT().GetAnswer(gomock.Any(), answerReq).Return(entity.Answer{}, context.DeadlineExceeded),
		messages.EXPECT().FinishReply(gomock.Any(), int64(11), firstClaim, int64(0)).Return(nil),
		messages.EXPECT().ClaimReply(gomock.Any(), int64(11), replyLease).Return(secondClaim, nil),
		ai.EXPECT().GetAnswer(gomock.Any(), answerReq).Return(entity.Answer{Text: wantReply.Text}, nil),
		messages.EXPECT().Create(txCtx, matchNewMessage(wantReply)).Return(int64(12), nil),
		messages.EXPECT().FinishReply(txCtx, int64(11), secondClaim, int64(12)).Return(nil),
	)

	auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
	tx := transactorFunc(func(_ context.Context, f func(context.Context) error) error { return f(txCtx) })
	service := NewService(conversations, messages, nil, users, onboarding, ai, auth, tx, 4000, 30)
	if _, err := service.SendMessage(ctx, "session", question, testIdempotencyKey); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first SendMessage() error = %v, want deadline exceeded", err)
	}
	reply, err := service.SendMessage(ctx, "session", question, testIdempotencyKey)
	if err != nil || reply.ID != 12 || reply.Text != wantReply.Text {
		t.Fatalf("retry SendMessage() = %+v, %v, want saved answer", reply, err)
	}
}

func TestGetHistory(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	conversations := mocks.NewMockconversationService(ctrl)
	messages := mocks.NewMockmessageRepository(ctrl)
	auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
	service := NewService(conversations, messages, nil, nil, nil, nil, auth, nil, 4000, 30)
	history := []entity.Message{{ID: 11, ConversationID: 21, Role: entity.MessageRoleUser, Text: "Question"}}
	gomock.InOrder(
		conversations.EXPECT().GetOrCreate(ctx, int64(7)).Return(&entity.Conversation{ID: 21}, nil),
		messages.EXPECT().ListByConversation(ctx, int64(21), int32(2), "13").Return(history, "11", nil),
	)

	got, err := service.GetHistory(ctx, "session", 2, "13")
	want := entity.HistoryPage{Messages: history, NextPageToken: "11"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetHistory() = %+v, %v, want %+v, nil", got, err, want)
	}
}

func TestSignedOutInternCannotReadHistoryOrAskAQuestion(t *testing.T) {
	ctrl := gomock.NewController(t)
	conversations := mocks.NewMockconversationService(ctrl)
	messages := mocks.NewMockmessageRepository(ctrl)
	auth := validatorFunc(func(context.Context, string) (int64, error) { return 0, chaterrors.ErrInvalidToken })
	service := NewService(conversations, messages, nil, nil, nil, nil, auth, nil, 4000, 30)

	got, err := service.GetHistory(t.Context(), "invalid", 2, "")
	if !reflect.DeepEqual(got, entity.HistoryPage{}) || !errors.Is(err, chaterrors.ErrInvalidToken) {
		t.Fatalf("GetHistory() = %+v, %v, want empty page and invalid token error", got, err)
	}
	reply, err := service.SendMessage(t.Context(), "invalid", "How do I get access?", testIdempotencyKey)
	if !reflect.DeepEqual(reply, entity.Message{}) || !errors.Is(err, chaterrors.ErrInvalidToken) {
		t.Fatalf("SendMessage() = %+v, %v, want empty reply and invalid token error", reply, err)
	}
}

func TestQuestionSurvivesUnavailableOnboardingContext(t *testing.T) {
	unavailable := errors.New("dependency unavailable")
	for _, tc := range []struct {
		name       string
		historyErr error
		userErr    error
		stepErr    error
		wantErr    error
	}{
		{name: "history is unavailable", historyErr: unavailable, wantErr: chaterrors.ErrListMessages},
		{name: "team data is unavailable", userErr: unavailable, wantErr: chaterrors.ErrGetUserContext},
		{name: "onboarding plans are unavailable", stepErr: unavailable, wantErr: chaterrors.ErrGetOnboardingPlans},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const question = "What should I do next?"
			ctx := t.Context()
			ctrl := gomock.NewController(t)
			conversations := mocks.NewMockconversationService(ctrl)
			messages := mocks.NewMockmessageRepository(ctrl)
			users := mocks.NewMockuserClient(ctrl)
			onboarding := mocks.NewMockonboardingClient(ctrl)
			ai := mocks.NewMockaiClient(ctrl)
			actions := mocks.NewMockactionRepository(ctrl)
			auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
			tx := transactorFunc(func(context.Context, func(context.Context) error) error {
				t.Fatal("reply transaction started without onboarding context")
				return nil
			})
			claimUntil := time.Now().Add(replyLease)
			calls := []any{
				conversations.EXPECT().GetOrCreate(ctx, int64(7)).Return(&entity.Conversation{ID: 21}, nil),
				messages.EXPECT().GetOrCreateQuestion(ctx, matchNewMessage(entity.Message{
					ConversationID: 21, Role: entity.MessageRoleUser, Text: question,
				}), testIdempotencyKey).Return(int64(11), question, nil),
				messages.EXPECT().ClaimReply(gomock.Any(), int64(11), replyLease).Return(claimUntil, nil),
				messages.EXPECT().ListByConversation(gomock.Any(), int64(21), int32(30), "11").Return(nil, "", tc.historyErr),
			}
			if tc.historyErr == nil {
				calls = append(calls, users.EXPECT().GetUserContext(gomock.Any(), "7").Return(entity.UserContext{}, tc.userErr))
			}
			if tc.historyErr == nil && tc.userErr == nil {
				calls = append(calls, onboarding.EXPECT().GetOnboardingPlans(gomock.Any(), int64(7), "session").Return(nil, tc.stepErr))
			}
			calls = append(calls, messages.EXPECT().FinishReply(gomock.Any(), int64(11), claimUntil, int64(0)).Return(nil))
			gomock.InOrder(calls...)

			got, err := NewService(conversations, messages, actions, users, onboarding, ai, auth, tx, 4000, 30).
				SendMessage(ctx, "session", question, testIdempotencyKey)
			if !reflect.DeepEqual(got, entity.Message{}) || !errors.Is(err, tc.wantErr) || !errors.Is(err, unavailable) {
				t.Fatalf("SendMessage() = %+v, %v, want no reply and %v wrapping %v", got, err, tc.wantErr, unavailable)
			}
		})
	}
}

func TestInternSessionIsBoundedByVerifiedTokenExpiry(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	past := future.Add(-2 * time.Hour)
	for _, tc := range []struct {
		name    string
		expiry  *jwt.NumericDate
		authErr error
		wantErr error
	}{
		{name: "active session", expiry: jwt.NewNumericDate(future)},
		{name: "expired session", expiry: jwt.NewNumericDate(past), wantErr: chaterrors.ErrInvalidToken},
		{name: "session without expiry", wantErr: chaterrors.ErrInvalidToken},
		{name: "auth cannot verify token", expiry: jwt.NewNumericDate(future), authErr: chaterrors.ErrAuthUnavailable, wantErr: chaterrors.ErrAuthUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{ExpiresAt: tc.expiry}).
				SignedString([]byte("synthetic-test-signing-key"))
			if err != nil {
				t.Fatal(err)
			}
			auth := validatorFunc(func(gotCtx context.Context, gotToken string) (int64, error) {
				if gotCtx != ctx || gotToken != token {
					t.Fatal("session did not validate the supplied token")
				}
				return 7, tc.authErr
			})
			service := NewService(nil, nil, nil, nil, nil, nil, auth, nil, 4000, 30)
			userID, expiresAt, err := service.GetSession(ctx, token)
			if tc.wantErr != nil {
				if userID != "" || !expiresAt.IsZero() || !errors.Is(err, tc.wantErr) {
					t.Fatalf("GetSession() = %q, %v, %v, want no session and %v", userID, expiresAt, err, tc.wantErr)
				}
				return
			}
			if err != nil || userID != "7" || !expiresAt.Equal(future) {
				t.Fatalf("GetSession() = %q, %v, %v, want intern 7 and expiry %v", userID, expiresAt, err, future)
			}
		})
	}
}

func TestReopeningChatReportsUnavailableHistory(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	conversations := mocks.NewMockconversationService(ctrl)
	messages := mocks.NewMockmessageRepository(ctrl)
	unavailable := errors.New("database unavailable")
	conversations.EXPECT().GetOrCreate(ctx, int64(7)).Return(&entity.Conversation{ID: 21}, nil)
	messages.EXPECT().ListByConversation(ctx, int64(21), int32(10), "").Return(nil, "", unavailable)
	auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
	service := NewService(conversations, messages, nil, nil, nil, nil, auth, nil, 4000, 30)

	got, err := service.GetHistory(ctx, "session", 10, "")
	if !reflect.DeepEqual(got, entity.HistoryPage{}) || !errors.Is(err, chaterrors.ErrListMessages) || !errors.Is(err, unavailable) {
		t.Fatalf("GetHistory() = %+v, %v, want empty page and history unavailable", got, err)
	}
}

func TestSendMessageRejectsQuestionAboveConfiguredLimitBeforeSaving(t *testing.T) {
	auth := validatorFunc(func(context.Context, string) (int64, error) { return 7, nil })
	service := NewService(nil, nil, nil, nil, nil, nil, auth, nil, 3, 30)

	_, err := service.SendMessage(t.Context(), "token", "четыре", 1)
	if !errors.Is(err, chaterrors.ErrInvalidMessageLength) {
		t.Fatalf("SendMessage() error = %v, want ErrInvalidMessageLength", err)
	}
}
