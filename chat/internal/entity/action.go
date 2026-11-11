package entity

type Action struct {
	ID            int64
	MessageID     int64
	ResourceID    string
	ResourceLabel string
	AccessLevel   string
	AccessRequest *AccessRequest
}

type AccessRequest struct {
	TaskID            string
	ExternalRequestID string
	Status            string
}
