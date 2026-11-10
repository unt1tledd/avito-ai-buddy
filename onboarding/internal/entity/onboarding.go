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
	Title       string
	Description string
	Period      string
	ResourceID  string
	Status      StepStatus
}

type StepTemplate struct {
	Key         string
	Title       string
	Description string
	Period      string
	Position    int32
	ResourceID  string
}
