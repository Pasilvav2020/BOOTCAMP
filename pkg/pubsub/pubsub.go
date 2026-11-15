package pubsub

import "context"

type PubSub interface {
	Publish(ctx context.Context, topic string, data []byte, attrs map[string]string) error
	Subscribe(ctx context.Context, subscription string, handler MessageHandler) error
	Close() error
	Native() interface{}
}

type Message struct {
	ID         string
	Data       []byte
	Attributes map[string]string
	// AckFunc and NackFunc are retained for provider compatibility. Subscribers
	// own final acknowledgement; handlers should return an error to request a retry.
	AckFunc  func()
	NackFunc func()
}

type MessageHandler func(ctx context.Context, msg *Message) error
