package message

//go:generate mockgen -source=message.go -destination=mocks/message_mocks.go -package=mocks

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/port"

	"github.com/golang-jwt/jwt/v4"
)

type conversationService interface {
	GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error)
}

type messageRepository interface {
	Create(ctx context.Context, message entity.Message) (int64, error)
	GetOrCreateQuestion(ctx context.Context, question entity.Message, idempotencyKey int64) (id int64, savedText string, err error)
	ClaimReply(ctx context.Context, questionID int64, lease time.Duration) (time.Time, error)
	GetReply(ctx context.Context, questionID int64) (*entity.Message, error)
	FinishReply(ctx context.Context, questionID int64, claimUntil time.Time, replyID int64) error
	ListByConversation(ctx context.Context, conversationID int64, pageSize int32, pageToken string) ([]entity.Message, string, error)
}

const (
	replyTimeout = 45 * time.Second
	replyLease   = time.Minute
)

type actionRepository interface {
	Create(ctx context.Context, action entity.Action) (int64, error)
}

type userClient interface {
	GetUserContext(ctx context.Context, userID string) (port.UserContext, error)
}

type onboardingClient interface {
	GetCurrentStep(ctx context.Context, userID string) (*port.OnboardingStep, error)
}

type aiClient interface {
	GetAnswer(ctx context.Context, request port.AnswerRequest) (port.Answer, error)
}

type chatService struct {
	tx            port.Transactor
	auth          port.TokenValidator
	conversations conversationService
	messagesRepo  messageRepository
	actionsRepo   actionRepository
	user          userClient
	onboarding    onboardingClient
	ai            aiClient
}

func NewService(
	conversations conversationService,
	messagesRepo messageRepository,
	actionsRepo actionRepository,
	user userClient,
	onboarding onboardingClient,
	ai aiClient,
	auth port.TokenValidator,
	tx port.Transactor,
) *chatService {
	return &chatService{
		tx:            tx,
		auth:          auth,
		conversations: conversations,
		messagesRepo:  messagesRepo,
		actionsRepo:   actionsRepo,
		user:          user,
		onboarding:    onboarding,
		ai:            ai,
	}
}

func (s *chatService) SendMessage(ctx context.Context, token, text string, idempotencyKey int64) (entity.Message, error) {
	userID, err := s.auth.ValidateToken(ctx, token)
	if err != nil {
		return entity.Message{}, err
	}

	conversation, err := s.conversations.GetOrCreate(ctx, userID)
	if err != nil {
		return entity.Message{}, fmt.Errorf("%w: %w", chaterrors.ErrGetConversation, err)
	}

	question := entity.Message{
		ConversationID: conversation.ID,
		Role:           entity.MessageRoleUser,
		Text:           text,
		CreatedAt:      time.Now().UTC(),
	}
	questionID, savedText, err := s.messagesRepo.GetOrCreateQuestion(ctx, question, idempotencyKey)
	if err != nil {
		return entity.Message{}, fmt.Errorf("%w: %w", chaterrors.ErrSaveUserMessage, err)
	}
	if savedText != question.Text {
		return entity.Message{}, chaterrors.ErrIdempotencyKeyReused
	}
	question.ID = questionID

	// The processing deadline leaves time for the database claim to expire after a crash.
	replyCtx, cancel := context.WithTimeout(ctx, replyTimeout)
	defer cancel()
	claimUntil, err := s.messagesRepo.ClaimReply(replyCtx, questionID, replyLease)
	if err != nil {
		return entity.Message{}, fmt.Errorf("claim reply: %w", err)
	}
	if claimUntil.IsZero() {
		reply, err := s.messagesRepo.GetReply(replyCtx, questionID)
		if err != nil {
			return entity.Message{}, fmt.Errorf("get saved reply: %w", err)
		}
		if reply == nil {
			return entity.Message{}, chaterrors.ErrReplyInProgress
		}
		return *reply, nil
	}

	reply, err := s.answerQuestion(replyCtx, userID, question, claimUntil)
	if err != nil {
		// Release on normal errors and cancellations. The lease covers a crash or failed release.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = s.messagesRepo.FinishReply(releaseCtx, questionID, claimUntil, 0)
	}
	return reply, err
}

func (s *chatService) answerQuestion(ctx context.Context, userID int64, question entity.Message, claimUntil time.Time) (entity.Message, error) {
	history, _, err := s.messagesRepo.ListByConversation(ctx, question.ConversationID, 0, strconv.FormatInt(question.ID, 10))
	if err != nil {
		return entity.Message{}, fmt.Errorf("%w: %w", chaterrors.ErrListMessages, err)
	}

	userKey := strconv.FormatInt(userID, 10)
	user, err := s.user.GetUserContext(ctx, userKey)
	if err != nil {
		return entity.Message{}, fmt.Errorf("%w: %w", chaterrors.ErrGetUserContext, err)
	}
	user.UserID = userKey

	step, err := s.onboarding.GetCurrentStep(ctx, userKey)
	if err != nil {
		return entity.Message{}, fmt.Errorf("%w: %w", chaterrors.ErrGetOnboardingStep, err)
	}

	answer, err := s.ai.GetAnswer(ctx, port.AnswerRequest{
		User:        user,
		Question:    question.Text,
		History:     history,
		CurrentStep: step,
	})
	if err != nil {
		return entity.Message{}, fmt.Errorf("%w: %w", chaterrors.ErrGetAIAnswer, err)
	}

	assistantMessage := entity.Message{
		ConversationID: question.ConversationID,
		Role:           entity.MessageRoleAssistant,
		Text:           answer.Text,
		Sources:        answer.Sources,
		CreatedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	if proposal := answer.Proposal; proposal != nil {
		assistantMessage.Proposal = &entity.Action{
			ResourceID:    proposal.ResourceID,
			ResourceLabel: proposal.ResourceLabel,
			AccessLevel:   proposal.AccessLevel,
		}
	}

	if err := s.tx.WithTx(ctx, func(txCtx context.Context) error {
		assistantMessageID, err := s.messagesRepo.Create(txCtx, assistantMessage)
		if err != nil {
			return fmt.Errorf("%w: %w", chaterrors.ErrSaveAssistantMessage, err)
		}
		assistantMessage.ID = assistantMessageID

		if action := assistantMessage.Proposal; action != nil {
			action.MessageID = assistantMessageID
			actionID, err := s.actionsRepo.Create(txCtx, *action)
			if err != nil {
				return fmt.Errorf("%w: %w", chaterrors.ErrSaveProposal, err)
			}
			action.ID = actionID
		}
		if err := s.messagesRepo.FinishReply(txCtx, question.ID, claimUntil, assistantMessageID); err != nil {
			return fmt.Errorf("finish reply: %w", err)
		}
		return nil
	}); err != nil {
		return entity.Message{}, err
	}

	return assistantMessage, nil
}

func (s *chatService) GetHistory(ctx context.Context, token string, pageSize int32, pageToken string) (entity.HistoryPage, error) {
	userID, err := s.auth.ValidateToken(ctx, token)
	if err != nil {
		return entity.HistoryPage{}, err
	}
	conversation, err := s.conversations.GetOrCreate(ctx, userID)
	if err != nil {
		return entity.HistoryPage{}, fmt.Errorf("%w: %w", chaterrors.ErrGetConversation, err)
	}

	messages, nextPageToken, err := s.messagesRepo.ListByConversation(ctx, conversation.ID, pageSize, pageToken)
	if err != nil {
		return entity.HistoryPage{}, fmt.Errorf("%w: %w", chaterrors.ErrListMessages, err)
	}
	return entity.HistoryPage{Messages: messages, NextPageToken: nextPageToken}, nil
}

func (s *chatService) GetSession(ctx context.Context, token string) (string, time.Time, error) {
	userID, err := s.auth.ValidateToken(ctx, token)
	if err != nil {
		return "", time.Time{}, err
	}

	// Auth has already verified this token. Read its expiry to bound the WebSocket session.
	claims := new(jwt.RegisteredClaims)
	if _, _, err := new(jwt.Parser).ParseUnverified(token, claims); err != nil ||
		claims.ExpiresAt == nil || !time.Now().Before(claims.ExpiresAt.Time) {
		return "", time.Time{}, chaterrors.ErrInvalidToken
	}
	return strconv.FormatInt(userID, 10), claims.ExpiresAt.Time, nil
}
