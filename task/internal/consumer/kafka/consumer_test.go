package kafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"avito-ai-buddy/task/internal/config"
	taskerrors "avito-ai-buddy/task/internal/errors"
)

type testHandler func(context.Context, []byte) error

func (h testHandler) Handle(ctx context.Context, payload []byte) error {
	return h(ctx, payload)
}

func (h testHandler) ReportFailure(context.Context, []byte) error { return nil }

type reportingHandler struct {
	testHandler
	report func(context.Context) error
}

func (h reportingHandler) ReportFailure(ctx context.Context, _ []byte) error { return h.report(ctx) }

type testDLQ func(context.Context, ...kafkago.Message) error

func (d testDLQ) WriteMessages(ctx context.Context, messages ...kafkago.Message) error {
	return d(ctx, messages...)
}

type testReader struct {
	message kafkago.Message
	fetch   func() error
	commit  func(kafkago.Message)
}

func (r testReader) FetchMessage(context.Context) (kafkago.Message, error) {
	if r.fetch != nil {
		if err := r.fetch(); err != nil {
			return kafkago.Message{}, err
		}
	}
	return r.message, nil
}

func (r testReader) CommitMessages(_ context.Context, messages ...kafkago.Message) error {
	r.commit(messages[0])
	return nil
}

func TestConsumeCommitAfterProcessingOrDLQAck(t *testing.T) {
	unavailable := errors.New("service unavailable")
	for _, tc := range []struct {
		name            string
		handlingErrors  []error
		dlqFailsOnce    bool
		cancelOnDLQFail bool
		want            []string
	}{
		{"invalid goes straight to DLQ", []error{taskerrors.ErrInvalidInput}, true, false, []string{"handle", "dlq", "dlq", "commit"}},
		{"temporary errors exhaust attempts", []error{unavailable, unavailable, unavailable}, false, false, []string{"handle", "handle", "handle", "dlq", "commit"}},
		{"temporary error recovers", []error{unavailable, nil}, false, false, []string{"handle", "handle", "commit"}},
		{"DLQ failure during shutdown leaves offset uncommitted", []error{taskerrors.ErrProposalConflict}, true, true, []string{"handle", "dlq"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var order []string
			var handles, writes int
			original := kafkago.Message{
				Topic: "task.access_requests.create", Partition: 2, Offset: 47,
				Key: []byte("17"), Value: []byte("original payload"),
				Headers: []kafkago.Header{{Key: "trace", Value: []byte("abc")}},
			}
			reader := testReader{message: original, commit: func(message kafkago.Message) {
				order = append(order, "commit")
				if message.Offset != original.Offset {
					t.Errorf("committed offset %d", message.Offset)
				}
				cancel()
			}}
			handler := testHandler(func(context.Context, []byte) error {
				order = append(order, "handle")
				if handles >= len(tc.handlingErrors) {
					t.Fatal("unexpected processing attempt")
				}
				err := tc.handlingErrors[handles]
				handles++
				return err
			})
			dlq := testDLQ(func(_ context.Context, messages ...kafkago.Message) error {
				order = append(order, "dlq")
				message := messages[0]
				if string(message.Key) != "17" || string(message.Value) != "original payload" {
					t.Errorf("DLQ changed the original key or payload: %+v", message)
				}
				headers := map[string]string{}
				for _, header := range message.Headers {
					headers[header.Key] = string(header.Value)
				}
				if headers["trace"] != "abc" || headers["x-original-topic"] != original.Topic ||
					headers["x-original-partition"] != "2" || headers["x-original-offset"] != "47" ||
					headers["x-error"] == "" {
					t.Errorf("DLQ headers: %v", headers)
				}
				writes++
				if writes == 1 && tc.dlqFailsOnce {
					if tc.cancelOnDLQFail {
						cancel()
					}
					return unavailable
				}
				return nil
			})
			cfg := config.KafkaConsumerConfig{DLQPublishTimeout: time.Second, MaxAttempts: 3, RetryBaseBackoff: time.Millisecond, RetryMaxBackoff: 2 * time.Millisecond}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			if err := consume(ctx, logger, reader, dlq, handler, cfg); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(order, tc.want) {
				t.Fatalf("operations = %v, want %v", order, tc.want)
			}
		})
	}
}

func TestConsumeRecoversAfterKafkaFetchEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var fetches, handled, committed int
	reader := testReader{
		message: kafkago.Message{Offset: 47},
		fetch: func() error {
			fetches++
			if fetches <= 3 {
				return io.EOF
			}
			return nil
		},
		commit: func(message kafkago.Message) {
			if message.Offset != 47 || handled != 1 {
				t.Errorf("committed offset %d after %d handler calls", message.Offset, handled)
			}
			committed++
			cancel()
		},
	}
	handler := testHandler(func(context.Context, []byte) error {
		handled++
		return nil
	})
	cfg := config.KafkaConsumerConfig{
		MaxAttempts: 1, RetryBaseBackoff: time.Millisecond, RetryMaxBackoff: 2 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := consume(ctx, logger, reader, nil, handler, cfg); err != nil {
		t.Fatal(err)
	}
	if fetches != 4 || handled != 1 || committed != 1 {
		t.Fatalf("fetches=%d, handled=%d, committed=%d; want 4, 1, 1", fetches, handled, committed)
	}
}

func TestFailureResultMustBeAcknowledgedBeforeCommit(t *testing.T) {
	for _, cancelDuringReport := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelDuringReport), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var order []string
			reports := 0
			reader := testReader{commit: func(kafkago.Message) { order = append(order, "commit"); cancel() }}
			handler := reportingHandler{
				testHandler: func(context.Context, []byte) error {
					order = append(order, "handle")
					return taskerrors.ErrInvalidInput
				},
				report: func(context.Context) error {
					order = append(order, "report")
					reports++
					if cancelDuringReport {
						cancel()
					}
					if reports == 1 {
						return errors.New("result topic unavailable")
					}
					return nil
				},
			}
			dlq := testDLQ(func(context.Context, ...kafkago.Message) error { order = append(order, "dlq"); return nil })
			cfg := config.KafkaConsumerConfig{MaxAttempts: 1, DLQPublishTimeout: time.Second, RetryBaseBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond}
			if err := consume(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), reader, dlq, handler, cfg); err != nil {
				t.Fatal(err)
			}
			want := []string{"handle", "dlq", "report", "report", "commit"}
			if cancelDuringReport {
				want = want[:3]
			}
			if !slices.Equal(order, want) {
				t.Fatalf("order=%v, want %v", order, want)
			}
		})
	}
}
