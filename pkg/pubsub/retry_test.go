package pubsub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

type failureRecorderStub struct {
	message FailedMessage
	calls   int
	err     error
}

func TestProcessMessageAcknowledgesOnlyAfterSuccessfulHandler(t *testing.T) {
	p := &gcpPubSub{log: zap.NewNop()}
	ackCalls, nackCalls := 0, 0
	p.processMessage(context.Background(), "orders-sub", &Message{ID: "message-ack"}, func(_ context.Context, message *Message) error {
		message.AckFunc()
		return nil
	}, func() { ackCalls++ }, func() { nackCalls++ })

	if ackCalls != 1 || nackCalls != 0 {
		t.Errorf("ack/nack calls = %d/%d, want 1/0", ackCalls, nackCalls)
	}
}

func TestProcessMessageNacksWhenHandlerRequestsAckButFails(t *testing.T) {
	p := &gcpPubSub{log: zap.NewNop()}
	ackCalls, nackCalls := 0, 0
	p.processMessage(context.Background(), "orders-sub", &Message{ID: "message-nack"}, func(_ context.Context, message *Message) error {
		message.AckFunc()
		return errors.New("processing failed")
	}, func() { ackCalls++ }, func() { nackCalls++ })

	if ackCalls != 0 || nackCalls != 1 {
		t.Errorf("ack/nack calls = %d/%d, want 0/1", ackCalls, nackCalls)
	}
}

func TestProcessMessageAcknowledgesAfterExhaustedFailureIsRecorded(t *testing.T) {
	p := &gcpPubSub{
		log:             zap.NewNop(),
		retryPolicy:     &RetryPolicy{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond},
		failureRecorder: &failureRecorderStub{},
	}
	ackCalls, nackCalls := 0, 0
	p.processMessage(context.Background(), "orders-sub", &Message{ID: "message-parked"}, func(context.Context, *Message) error {
		return errors.New("processing failed")
	}, func() { ackCalls++ }, func() { nackCalls++ })

	if ackCalls != 1 || nackCalls != 0 {
		t.Errorf("ack/nack calls = %d/%d, want 1/0 after durable record", ackCalls, nackCalls)
	}
}

func TestProcessMessageNacksWhenExhaustedFailureCannotBeRecorded(t *testing.T) {
	p := &gcpPubSub{
		log:             zap.NewNop(),
		retryPolicy:     &RetryPolicy{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond},
		failureRecorder: &failureRecorderStub{err: errors.New("storage unavailable")},
	}
	ackCalls, nackCalls := 0, 0
	p.processMessage(context.Background(), "orders-sub", &Message{ID: "message-not-parked"}, func(context.Context, *Message) error {
		return errors.New("processing failed")
	}, func() { ackCalls++ }, func() { nackCalls++ })

	if ackCalls != 0 || nackCalls != 1 {
		t.Errorf("ack/nack calls = %d/%d, want 0/1 when durable record fails", ackCalls, nackCalls)
	}
}

func TestSafeEventIDOnlyReturnsNonSensitiveIdentifierShape(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "uuid", id: "c04f51f1-a0c7-4ee7-917e-eac0a972994e", want: "c04f51f1-a0c7-4ee7-917e-eac0a972994e"},
		{name: "rejects spaces", id: "customer rut 123", want: ""},
		{name: "rejects long value", id: strings.Repeat("a", 129), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeEventID(map[string]string{"event_id": tt.id})
			if got != tt.want {
				t.Errorf("safeEventID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func (r *failureRecorderStub) RecordFailure(_ context.Context, message FailedMessage) error {
	r.message = message
	r.calls++
	return r.err
}

func TestProcessWithRetryRetriesUntilSuccess(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 5, InitialBackoff: time.Nanosecond, MaxBackoff: time.Microsecond}
	recorder := &failureRecorderStub{}
	calls := 0

	err := processWithRetry(context.Background(), policy, recorder, "orders-sub", &Message{ID: "message-1"}, func(context.Context, *Message) error {
		calls++
		if calls < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("processWithRetry() error = %v", err)
	}
	if calls != 3 {
		t.Errorf("handler called %d times, want 3", calls)
	}
	if recorder.calls != 0 {
		t.Errorf("failure recorder called %d times, want 0", recorder.calls)
	}
}

func TestProcessWithRetryRecordsExhaustedMessage(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Nanosecond, MaxBackoff: time.Microsecond}
	recorder := &failureRecorderStub{}
	message := &Message{
		ID:         "message-2",
		Data:       []byte(`{"order_id":"42"}`),
		Attributes: map[string]string{"type": "order.created"},
	}
	calls := 0

	err := processWithRetry(context.Background(), policy, recorder, "orders-sub", message, func(context.Context, *Message) error {
		calls++
		return errors.New("database unavailable")
	})
	if err != nil {
		t.Fatalf("processWithRetry() error = %v", err)
	}
	if calls != policy.MaxAttempts {
		t.Errorf("handler called %d times, want %d", calls, policy.MaxAttempts)
	}
	if recorder.calls != 1 {
		t.Fatalf("failure recorder called %d times, want 1", recorder.calls)
	}
	if recorder.message.Subscription != "orders-sub" || recorder.message.MessageID != message.ID ||
		recorder.message.Attempts != policy.MaxAttempts || recorder.message.LastError != "database unavailable" {
		t.Errorf("recorded failure = %+v, missing expected metadata", recorder.message)
	}
	if string(recorder.message.Data) != string(message.Data) || recorder.message.Attributes["type"] != "order.created" {
		t.Errorf("recorded message = %+v, want payload and attributes preserved", recorder.message)
	}
}

func TestProcessWithRetryReturnsRecorderFailure(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond}
	recorder := &failureRecorderStub{err: errors.New("database unavailable")}

	err := processWithRetry(context.Background(), policy, recorder, "orders-sub", &Message{ID: "message-3"}, func(context.Context, *Message) error {
		return errors.New("handler failed")
	})
	if err == nil {
		t.Fatal("processWithRetry() error = nil, want persistence error")
	}
}

func TestRetryBackoffGrowsAndCaps(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 8, InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i, expected := range want {
		if got := retryBackoff(policy, i+1); got != expected {
			t.Errorf("retryBackoff(attempt %d) = %s, want %s", i+1, got, expected)
		}
	}
}

func TestProcessWithRetryStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy := RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Hour, MaxBackoff: time.Hour}
	recorder := &failureRecorderStub{}
	calls := 0

	err := processWithRetry(ctx, policy, recorder, "orders-sub", &Message{ID: "message-4"}, func(context.Context, *Message) error {
		calls++
		return errors.New("temporary failure")
	})
	if err == nil {
		t.Fatal("processWithRetry() error = nil, want cancellation error")
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1 after cancellation", calls)
	}
}
