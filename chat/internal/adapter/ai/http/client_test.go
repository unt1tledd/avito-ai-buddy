package http

import (
	"strings"
	"testing"

	"avito-ai-buddy/chat/internal/entity"
)

func TestRecentHistoryKeepsLongAnswersWithinBudget(t *testing.T) {
	client := NewClient(nil, Options{MaxMessageLength: 4000, MaxHistorySources: 6, HistoryBudgetBytes: 7000})
	longAnswer := strings.Repeat("x", 5000)
	history := client.recentHistory([]entity.Message{
		{Role: entity.MessageRoleUser, Text: strings.Repeat("y", 3000)},
		{Role: entity.MessageRoleAssistant, Text: longAnswer, Sources: []entity.Source{{DocumentID: "vpn-guide"}}},
		{Role: entity.MessageRoleUser, Text: "А дальше?"},
	})
	if len(history) != 2 || history[0].Content != longAnswer || history[1].Content != "А дальше?" {
		t.Fatalf("expected long assistant answer and latest user message, got %d messages", len(history))
	}
	if len(history[0].SourceIDs) != 1 || history[0].SourceIDs[0] != "vpn-guide" {
		t.Fatalf("source IDs = %v, want [vpn-guide]", history[0].SourceIDs)
	}
}
