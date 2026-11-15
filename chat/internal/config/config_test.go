package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestChatRequiresUsableAuthHistoryAndDispatchSettings(t *testing.T) {
	t.Setenv("AUTH_TIMEOUT", "5s")
	t.Setenv("CHAT_HISTORY_PAGE_SIZE", "25")
	t.Setenv("CHAT_PROPOSAL_TTL", "12h")
	t.Setenv("CHAT_OUTBOX_BATCH_SIZE", "5")
	t.Setenv("CHAT_OUTBOX_IN_PROGRESS_TTL", "1m")
	t.Setenv("CHAT_OUTBOX_POLL_INTERVAL", "1s")
	t.Setenv("CHAT_OUTBOX_RETRY_DELAY", "2s")
	t.Setenv("CHAT_OUTBOX_PUBLISH_TIMEOUT", "3s")
	t.Setenv("KAFKA_BROKERS", " kafka-1:9092, , kafka-2:9092 ")

	cfg, err := New()
	if err != nil {
		t.Fatalf("load chat configuration: %v", err)
	}
	if cfg.Auth.Timeout != 5*time.Second || cfg.App.HistoryPageSize != 25 || cfg.Outbox.BatchSize != 5 ||
		cfg.Outbox.RetryDelay != 2*time.Second || cfg.Outbox.PublishTimeout != 3*time.Second ||
		cfg.Proposal.TTL != 12*time.Hour ||
		!slices.Equal(cfg.Kafka.BrokerAddrs(), []string{"kafka-1:9092", "kafka-2:9092"}) {
		t.Fatal("auth/history/outbox configuration did not honor environment overrides")
	}

	for _, tc := range []struct {
		name  string
		env   string
		value string
		want  string
	}{
		{name: "bad auth timeout", env: "AUTH_TIMEOUT", value: "invalid", want: "invalid duration"},
		{name: "auth timeout disabled", env: "AUTH_TIMEOUT", value: "0s", want: "AUTH_TIMEOUT"},
		{name: "empty history page", env: "CHAT_HISTORY_PAGE_SIZE", value: "0", want: "CHAT_HISTORY_PAGE_SIZE"},
		{name: "proposal TTL disabled", env: "CHAT_PROPOSAL_TTL", value: "0s", want: "CHAT_PROPOSAL_TTL"},
		{name: "no requests can be dispatched", env: "CHAT_OUTBOX_BATCH_SIZE", value: "0", want: "CHAT_OUTBOX_BATCH_SIZE"},
		{name: "retry disabled", env: "CHAT_OUTBOX_RETRY_DELAY", value: "0s", want: "outbox durations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			cfg, err := New()
			if cfg != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New() returned a config: %t, error: %v, want invalid configuration mentioning %q", cfg != nil, err, tc.want)
			}
		})
	}
}
