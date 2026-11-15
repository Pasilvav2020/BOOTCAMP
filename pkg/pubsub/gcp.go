package pubsub

import (
	"context"
	"fmt"
	"sync"

	gcppubsub "cloud.google.com/go/pubsub/v2"
	"go.uber.org/zap"
	"google.golang.org/api/option"
)

type gcpPubSub struct {
	client          *gcppubsub.Client
	mu              sync.Mutex
	pubs            map[string]*gcppubsub.Publisher
	retryPolicy     *RetryPolicy
	failureRecorder FailureRecorder
	log             *zap.Logger
}

func NewGCPPubSub(ctx context.Context, projectID string, opts ...option.ClientOption) (PubSub, error) {
	return newGCPPubSub(ctx, projectID, nil, nil, zap.L(), opts...)
}

func NewGCPPubSubWithRetry(ctx context.Context, projectID string, policy RetryPolicy, recorder FailureRecorder, opts ...option.ClientOption) (PubSub, error) {
	return NewGCPPubSubWithRetryAndLogger(ctx, projectID, policy, recorder, zap.L(), opts...)
}

func NewGCPPubSubWithRetryAndLogger(ctx context.Context, projectID string, policy RetryPolicy, recorder FailureRecorder, log *zap.Logger, opts ...option.ClientOption) (PubSub, error) {
	if err := policy.validate(); err != nil {
		return nil, fmt.Errorf("pubsub retry policy: %w", err)
	}
	if recorder == nil {
		return nil, fmt.Errorf("pubsub retry policy: failure recorder is required")
	}
	if log == nil {
		return nil, fmt.Errorf("pubsub retry policy: logger is required")
	}
	return newGCPPubSub(ctx, projectID, &policy, recorder, log, opts...)
}

func newGCPPubSub(ctx context.Context, projectID string, policy *RetryPolicy, recorder FailureRecorder, log *zap.Logger, opts ...option.ClientOption) (PubSub, error) {
	client, err := gcppubsub.NewClient(ctx, projectID, opts...)

	if err != nil {
		return nil, fmt.Errorf("pubsub gcp: %w", err)
	}

	return &gcpPubSub{
		client:          client,
		pubs:            make(map[string]*gcppubsub.Publisher),
		retryPolicy:     policy,
		failureRecorder: recorder,
		log:             log,
	}, nil
}

func (p *gcpPubSub) publisher(topic string) *gcppubsub.Publisher {
	p.mu.Lock()

	defer p.mu.Unlock()

	pub, ok := p.pubs[topic]
	if !ok {
		pub = p.client.Publisher(topic)
		p.pubs[topic] = pub
	}

	return pub
}

func (p *gcpPubSub) Publish(ctx context.Context, topic string, data []byte, attrs map[string]string) error {
	pub := p.publisher(topic)

	result := pub.Publish(ctx, &gcppubsub.Message{
		Data:       data,
		Attributes: attrs,
	})

	messageID, err := result.Get(ctx)
	if err != nil {
		p.log.Error("Pub/Sub publish failed",
			zap.String("topic", topic),
			zap.String("error_type", fmt.Sprintf("%T", err)),
		)
		return err
	}
	p.log.Info("Pub/Sub message published",
		zap.String("topic", topic),
		zap.String("message_id", messageID),
	)
	return err
}

func (p *gcpPubSub) Subscribe(ctx context.Context, subscription string, handler MessageHandler) error {
	sub := p.client.Subscriber(subscription)

	return sub.Receive(ctx, func(c context.Context, msg *gcppubsub.Message) {
		message := &Message{
			ID:         msg.ID,
			Data:       msg.Data,
			Attributes: msg.Attributes,
		}
		p.processMessage(c, subscription, message, handler, msg.Ack, msg.Nack)
	})
}

func (p *gcpPubSub) processMessage(ctx context.Context, subscription string, message *Message, handler MessageHandler, ack, nack func()) {
	handlerMessage := *message
	handlerMessage.AckFunc = func() {}
	handlerMessage.NackFunc = func() {}

	var err error
	if p.retryPolicy != nil {
		err = processWithRetry(ctx, *p.retryPolicy, p.failureRecorder, subscription, &handlerMessage, handler, p.log)
	} else {
		err = handler(ctx, &handlerMessage)
	}
	if err != nil {
		p.log.Error("Pub/Sub message processing deferred",
			zap.String("subscription", subscription),
			zap.String("message_id", message.ID),
			zap.String("event_id", safeEventID(message.Attributes)),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("ack_action", "nack"),
		)
		nack()
		return
	}
	p.log.Info("Pub/Sub message processing completed",
		zap.String("subscription", subscription),
		zap.String("message_id", message.ID),
		zap.String("event_id", safeEventID(message.Attributes)),
		zap.String("ack_action", "ack"),
	)
	ack()
}

func (p *gcpPubSub) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pub := range p.pubs {
		pub.Stop()
	}
	p.client.Close()
	return nil
}

func (p *gcpPubSub) Native() interface{} {
	return p.client
}

func safeEventID(attributes map[string]string) string {
	eventID := attributes["event_id"]
	if len(eventID) == 0 || len(eventID) > 128 {
		return ""
	}
	for _, char := range eventID {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_') {
			return ""
		}
	}
	return eventID
}
