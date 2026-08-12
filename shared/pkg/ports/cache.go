package ports

import (
	"context"
	"time"
)

// Cache is the interface for the key-value cache layer.
// Default implementation: RedisCache (adapters/redis).
// Swap to Memcached/in-memory by implementing this interface.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, key string) (bool, error)

	// SetNX sets key=value only if key does not exist. Returns true if set.
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)

	// BLPop blocks until an element is available in any of the lists or timeout.
	BLPop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error)

	// LPush prepends values to a list.
	LPush(ctx context.Context, key string, values ...any) error

	// SAdd adds members to a set.
	SAdd(ctx context.Context, key string, members ...any) error

	// SIsMember checks if value is a member of a set.
	SIsMember(ctx context.Context, key string, member any) (bool, error)

	// XAdd appends a message to a stream.
	XAdd(ctx context.Context, stream string, values map[string]any) (string, error)

	// XReadGroup reads new messages from a stream consumer group.
	XReadGroup(ctx context.Context, group, consumer, stream string, count int, block time.Duration) ([]StreamMessage, error)

	// XAck acknowledges a stream message.
	XAck(ctx context.Context, stream, group string, ids ...string) error

	// XGroupCreateMkStream creates a consumer group (idempotent).
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) error

	// Ping checks the cache connection.
	Ping(ctx context.Context) error
}

// StreamMessage is a single message from a Redis stream.
type StreamMessage struct {
	ID     string
	Values map[string]any
}
