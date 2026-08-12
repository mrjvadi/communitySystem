package ports

import "context"

// Notifier sends notifications to server agents.
// Default implementation: NATS (adapters/nats).
// Swap to WebSocket/gRPC/NATS by implementing this interface.
type Notifier interface {
	// Publish sends a payload to a named channel.
	Publish(ctx context.Context, channel string, payload any) error
}

// Logger is the interface for structured logging.
// Default implementation: ZapLogger (adapters in logger package).
// Swap to slog/logrus by implementing this interface.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Field is a key-value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

func F(key string, value any) Field { return Field{Key: key, Value: value} }
