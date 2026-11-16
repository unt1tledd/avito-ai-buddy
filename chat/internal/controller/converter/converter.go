package converter

import (
	"strconv"

	"avito-ai-buddy/chat/internal/entity"
	chatpb "avito-ai-buddy/pkg/generated/chat/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// MessageToProto converts a domain message into its protobuf representation.
func MessageToProto(message entity.Message) *chatpb.Message {
	result := &chatpb.Message{
		Id:   strconv.FormatInt(message.ID, 10),
		Text: message.Text,
	}
	switch message.Role {
	case entity.MessageRoleUser:
		result.Role = chatpb.MessageRole_MESSAGE_ROLE_USER
	case entity.MessageRoleAssistant:
		result.Role = chatpb.MessageRole_MESSAGE_ROLE_ASSISTANT
	}
	if !message.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(message.CreatedAt)
	}
	for _, source := range message.Sources {
		result.Sources = append(result.Sources, &chatpb.Source{
			DocumentId: source.DocumentID,
			Title:      source.Title,
			Url:        source.URL,
		})
	}
	if action := message.Proposal; action != nil {
		result.Proposal = &chatpb.AccessProposal{
			Id:            strconv.FormatInt(action.ID, 10),
			ResourceId:    action.ResourceID,
			ResourceLabel: action.ResourceLabel,
			AccessLevel:   action.AccessLevel,
		}
		if action.AccessRequest != nil {
			result.Proposal.AccessRequest = AccessRequestToProto(*action.AccessRequest)
		}
	}
	return result
}

// AccessRequestToProto converts an access request into its protobuf
// representation.
func AccessRequestToProto(request entity.AccessRequest) *chatpb.AccessRequest {
	return &chatpb.AccessRequest{
		TaskId:            request.TaskID,
		ExternalRequestId: request.ExternalRequestID,
		Status:            request.Status,
	}
}

// HistoryToProto converts a history page into the protobuf response with the
// cursor for the next page.
func HistoryToProto(messages []entity.Message, nextPageToken string) *chatpb.GetHistoryResponse {
	response := &chatpb.GetHistoryResponse{
		Messages:      make([]*chatpb.Message, 0, len(messages)),
		NextPageToken: nextPageToken,
	}
	for _, item := range messages {
		response.Messages = append(response.Messages, MessageToProto(item))
	}
	return response
}
