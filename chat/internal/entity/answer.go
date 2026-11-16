package entity

// UserContext is the intern profile snippet the AI service needs to answer.
type UserContext struct {
	UserID        string
	TeamID        string
	Role          string
	ActualEndDate string
}

// OnboardingStep is the onboarding step the intern is currently working on
// within a plan.
type OnboardingStep struct {
	ID         string
	Title      string
	ResourceID string
	ActionType string
}

// OnboardingPlan is one onboarding track assigned to the intern. CurrentStep
// is nil once every step of the track is finished.
type OnboardingPlan struct {
	ID          string
	Title       string
	CurrentStep *OnboardingStep
}

// AnswerRequest carries everything the AI service needs to produce an answer:
// the user profile, the question, the conversation history and the onboarding
// plans with their current steps.
type AnswerRequest struct {
	User            UserContext
	Message         string
	History         []Message
	OnboardingPlans []OnboardingPlan
}

// Answer is the AI reply: the text, the cited sources and an optional access
// proposal the intern may confirm.
type Answer struct {
	Text     string
	Sources  []Source
	Proposal *Action
}
