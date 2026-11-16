package entity

// TaskStatus is the status reported by Task. Failed means command processing
// failed before a task was created; it is not a decision rejecting access.
type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskFailed   TaskStatus = "failed"
	TaskApproved TaskStatus = "approved"
	TaskRejected TaskStatus = "rejected"
)
