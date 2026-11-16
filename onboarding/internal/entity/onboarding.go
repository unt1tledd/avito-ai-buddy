package entity

type Plan struct {
	ID     int64
	UserID int64
	Track  string
	Steps  []Step
}

type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepCompleted StepStatus = "completed"
)

type Step struct {
	ID          string
	Track       string
	Title       string
	Description string
	Period      string
	ResourceID  string
	Status      StepStatus
}

// ActionType marks the action Chat can offer for a step with a resource.
const ActionTypeAccessRequest = "access_request"

// TrackContext is the plan context of a single track: the first incomplete
// step by template order, or nil when every step is completed.
type TrackContext struct {
	Track       string
	DisplayName string
	CurrentStep *Step
}

type StepTemplate struct {
	Key         string
	Title       string
	Description string
	Period      string
	Position    int32
	ResourceID  string
}
