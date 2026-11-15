package pubsub

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
	}
}

type FailedMessage struct {
	Subscription string
	MessageID    string
	Data         []byte
	Attributes   map[string]string
	Attempts     int
	LastError    string
}

type FailureRecorder interface {
	RecordFailure(ctx context.Context, message FailedMessage) error
}

func (p RetryPolicy) validate() error {
	if p.MaxAttempts < 1 {
		return errors.New("max attempts must be at least 1")
	}
	if p.InitialBackoff <= 0 {
		return errors.New("initial backoff must be greater than zero")
	}
	if p.MaxBackoff < p.InitialBackoff {
		return errors.New("max backoff must be greater than or equal to initial backoff")
	}
	return nil
}

func processWithRetry(
	ctx context.Context,
	policy RetryPolicy,
	recorder FailureRecorder,
	subscription string,
	message *Message,
	handler MessageHandler,
	logs ...*zap.Logger,
) error {
	log := zap.NewNop()
	if len(logs) > 0 && logs[0] != nil {
		log = logs[0]
	}
	if err := policy.validate(); err != nil {
		return err
	}
	if recorder == nil {
		return errors.New("failure recorder is required")
	}

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := handler(ctx, message); err == nil {
			log.Info("Pub/Sub message handler succeeded",
				zap.String("subscription", subscription),
				zap.String("message_id", message.ID),
				zap.String("event_id", safeEventID(message.Attributes)),
				zap.Int("attempt", attempt),
			)
			return nil
		} else {
			lastErr = err
			log.Warn("Pub/Sub message handler failed",
				zap.String("subscription", subscription),
				zap.String("message_id", message.ID),
				zap.String("event_id", safeEventID(message.Attributes)),
				zap.Int("attempt", attempt),
				zap.String("error_type", fmt.Sprintf("%T", err)),
			)
		}

		if attempt == policy.MaxAttempts {
			record := FailedMessage{
				Subscription: subscription,
				MessageID:    message.ID,
				Data:         append([]byte(nil), message.Data...),
				Attributes:   cloneAttributes(message.Attributes),
				Attempts:     attempt,
				LastError:    lastErr.Error(),
			}
			if err := recorder.RecordFailure(ctx, record); err != nil {
				return fmt.Errorf("record exhausted message %q after %d attempts: %w", message.ID, attempt, err)
			}
			log.Error("Pub/Sub message parked for investigation",
				zap.String("subscription", subscription),
				zap.String("message_id", message.ID),
				zap.String("event_id", safeEventID(message.Attributes)),
				zap.Int("attempts", attempt),
				zap.String("ack_action", "ack_after_durable_record"),
			)
			return nil
		}

		timer := time.NewTimer(retryBackoff(policy, attempt))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return fmt.Errorf("retry processing message %q: %w", message.ID, ctx.Err())
		case <-timer.C:
		}
	}
	return nil
}

func retryBackoff(policy RetryPolicy, failedAttempt int) time.Duration {
	delay := policy.InitialBackoff
	for i := 1; i < failedAttempt && delay < policy.MaxBackoff; i++ {
		if delay > policy.MaxBackoff/2 {
			return policy.MaxBackoff
		}
		delay *= 2
	}
	if delay > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return delay
}

func cloneAttributes(attributes map[string]string) map[string]string {
	if attributes == nil {
		return nil
	}
	clone := make(map[string]string, len(attributes))
	for key, value := range attributes {
		clone[key] = value
	}
	return clone
}
