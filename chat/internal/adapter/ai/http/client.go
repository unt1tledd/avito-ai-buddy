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
	"slices"
	"strings"
	"unicode/utf8"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
	"avito-ai-buddy/pkg/observability"
)

var historySourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type Options struct {
	BaseURL            string
	AnswerPath         string
	ServiceToken       string
	MaxMessageLength   int
	MaxHistorySources  int
	HistoryBudgetBytes int
}

// Client asks the AI service for an answer over HTTP.
type Client struct {
	client             *http.Client
	url                string
	serviceToken       string
	maxMessageLength   int
	maxHistorySources  int
	historyBudgetBytes int
}

func NewClient(client *http.Client, options Options) *Client {
	return &Client{
		client:             client,
		url:                strings.TrimRight(options.BaseURL, "/") + "/" + strings.TrimLeft(options.AnswerPath, "/"),
		serviceToken:       options.ServiceToken,
		maxMessageLength:   options.MaxMessageLength,
		maxHistorySources:  options.MaxHistorySources,
		historyBudgetBytes: options.HistoryBudgetBytes,
	}
}

func (c *Client) GetAnswer(ctx context.Context, request entity.AnswerRequest) (entity.Answer, error) {
	if strings.TrimSpace(c.serviceToken) == "" {
		return entity.Answer{}, errors.New("AI_SERVICE_TOKEN is required to call AI")
	}
	if message := strings.TrimSpace(request.Message); message == "" || utf8.RuneCountInString(message) > c.maxMessageLength {
		return entity.Answer{}, fmt.Errorf("AI question must contain 1 to %d characters", c.maxMessageLength)
	}
	body, err := json.Marshal(c.requestPayload(request))
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

func (c *Client) requestPayload(request entity.AnswerRequest) port.AIAnswerRequest {
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
		History:         c.recentHistory(request.History),
		OnboardingPlans: plans,
	}
}

func (c *Client) recentHistory(messages []entity.Message) []port.AIHistoryTurn {
	history := make([]port.AIHistoryTurn, 0, len(messages))
	used := 2 // JSON array brackets.
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != entity.MessageRoleUser && message.Role != entity.MessageRoleAssistant {
			continue
		}
		content := strings.TrimSpace(message.Text)
		if content == "" {
			continue
		}
		turn := port.AIHistoryTurn{Role: message.Role, Content: content}
		if message.Role == entity.MessageRoleAssistant {
			turn.SourceIDs = c.sourceIDsForHistory(message.Sources)
		}
		encoded, _ := json.Marshal(turn) // This payload contains only strings and a slice of strings.
		if len(history) != 0 {
			used++ // JSON array separator.
		}
		used += len(encoded)
		if used > c.historyBudgetBytes {
			break
		}
		history = append(history, turn)
	}
	slices.Reverse(history)
	return history
}

// Follow the AI history contract. Leave malformed legacy metadata unset so AI
// can apply its source-free history rules without rejecting the whole request.
func (c *Client) sourceIDsForHistory(sources []entity.Source) []string {
	if len(sources) == 0 || len(sources) > c.maxHistorySources {
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
