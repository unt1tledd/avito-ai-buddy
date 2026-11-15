package port

import "avito-ai-buddy/chat/internal/entity"

// AIAnswerRequest is the JSON payload sent to the AI service.
type AIAnswerRequest struct {
	Message         string             `json:"message"`
	Actor           AIActor            `json:"actor"`
	History         []AIHistoryTurn    `json:"history,omitempty"`
	OnboardingPlans []AIOnboardingPlan `json:"onboarding_plans"`
}

type AIActor struct {
	UserID        string `json:"user_id"`
	Role          string `json:"role"`
	Team          string `json:"team"`
	ActualEndDate string `json:"actual_end_date,omitempty"`
}

type AIHistoryTurn struct {
	Role      entity.MessageRole `json:"role"`
	Content   string             `json:"content"`
	SourceIDs []string           `json:"source_ids,omitempty"`
}

type AIOnboardingPlan struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	CurrentStep *AIOnboardingStep `json:"current_step"`
}

type AIOnboardingStep struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ResourceID string `json:"resource_id,omitempty"`
	ActionType string `json:"action_type,omitempty"`
}

// AIAnswerResponse is the JSON payload returned by the AI service.
type AIAnswerResponse struct {
	Answer   string      `json:"answer"`
	Sources  []AISource  `json:"sources"`
	Proposal *AIProposal `json:"proposal"`
}

type AISource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type AIProposal struct {
	ResourceID    string `json:"resource_id"`
	ResourceLabel string `json:"resource_label"`
	AccessLevel   string `json:"access_level"`
}
