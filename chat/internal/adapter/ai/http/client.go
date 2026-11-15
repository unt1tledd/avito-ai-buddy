package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
	"avito-ai-buddy/pkg/observability"
)

const (
	maxHistoryMessages = 12
	maxMessageLength   = 4000
	maxHistorySources  = 6
)

var historySourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type Options struct {
	BaseURL      string
	AnswerPath   string
	ServiceToken string
}

// Client asks the AI service for an answer over HTTP.
type Client struct {
	client       *http.Client
	url          string
	serviceToken string
}

func NewClient(client *http.Client, options Options) *Client {
	return &Client{
		client:       client,
		url:          strings.TrimRight(options.BaseURL, "/") + "/" + strings.TrimLeft(options.AnswerPath, "/"),
		serviceToken: options.ServiceToken,
	}
}

func (c *Client) GetAnswer(ctx context.Context, request entity.AnswerRequest) (entity.Answer, error) {
	if strings.TrimSpace(c.serviceToken) == "" {
		return entity.Answer{}, errors.New("AI_SERVICE_TOKEN is required to call AI")
	}
	if message := strings.TrimSpace(request.Message); message == "" || utf8.RuneCountInString(message) > maxMessageLength {
		return entity.Answer{}, fmt.Errorf("AI question must contain 1 to %d characters", maxMessageLength)
	}
	body, err := json.Marshal(requestPayload(request))
	if err != nil {
		return entity.Answer{}, fmt.Errorf("encode AI request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return entity.Answer{}, fmt.Errorf("build AI request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if id := observability.RequestID(ctx); id != "" {
		req.Header.Set(observability.RequestIDHeader, id)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return entity.Answer{}, fmt.Errorf("call AI service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return entity.Answer{}, fmt.Errorf("AI answer endpoint returned HTTP %d", resp.StatusCode)
	}

	var payload port.AIAnswerResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return entity.Answer{}, fmt.Errorf("decode AI response: %w", err)
	}
	if strings.TrimSpace(payload.Answer) == "" {
		return entity.Answer{}, errors.New("AI response contains no answer")
	}
	return responseAnswer(payload), nil
}

func requestPayload(request entity.AnswerRequest) port.AIAnswerRequest {
	plans := make([]port.AIOnboardingPlan, 0, len(request.OnboardingPlans))
	for _, item := range request.OnboardingPlans {
		track := port.AIOnboardingPlan{ID: item.ID, Title: item.Title}
		if current := item.CurrentStep; current != nil {
			track.CurrentStep = &port.AIOnboardingStep{
				ID: current.ID, Title: current.Title,
				ResourceID: current.ResourceID, ActionType: current.ActionType,
			}
		}
		plans = append(plans, track)
	}
	return port.AIAnswerRequest{
		Message: request.Message,
		Actor: port.AIActor{
			UserID: request.User.UserID, Role: request.User.Role, Team: request.User.TeamID,
			ActualEndDate: request.User.ActualEndDate,
		},
		History:         recentHistory(request.History),
		OnboardingPlans: plans,
	}
}

func recentHistory(messages []entity.Message) []port.AIHistoryTurn {
	if len(messages) > maxHistoryMessages {
		messages = messages[len(messages)-maxHistoryMessages:]
	}
	history := make([]port.AIHistoryTurn, 0, len(messages))
	for _, message := range messages {
		if message.Role != entity.MessageRoleUser && message.Role != entity.MessageRoleAssistant {
			continue
		}
		content := strings.TrimSpace(message.Text)
		if content == "" || utf8.RuneCountInString(content) > maxMessageLength {
			continue
		}
		turn := port.AIHistoryTurn{Role: message.Role, Content: content}
		if message.Role == entity.MessageRoleAssistant {
			turn.SourceIDs = sourceIDsForHistory(message.Sources)
		}
		history = append(history, turn)
	}
	return history
}

// Follow the AI history contract. Leave malformed legacy metadata unset so AI
// can apply its source-free history rules without rejecting the whole request.
func sourceIDsForHistory(sources []entity.Source) []string {
	if len(sources) == 0 || len(sources) > maxHistorySources {
		return nil
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		if !historySourceID.MatchString(source.DocumentID) {
			return nil
		}
		ids = append(ids, source.DocumentID)
	}
	return ids
}

func responseAnswer(payload port.AIAnswerResponse) entity.Answer {
	answer := entity.Answer{Text: payload.Answer}
	for _, source := range payload.Sources {
		answer.Sources = append(answer.Sources, entity.Source{DocumentID: source.ID, Title: source.Title, URL: source.URL})
	}
	if proposal := payload.Proposal; proposal != nil {
		answer.Proposal = &entity.Action{
			ResourceID: proposal.ResourceID, ResourceLabel: proposal.ResourceLabel, AccessLevel: proposal.AccessLevel,
		}
	}
	return answer
}
