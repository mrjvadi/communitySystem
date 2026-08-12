package ports

import (
	"context"
	"time"
)

// DocumentStore is the interface for document/NoSQL storage (MongoDB).
// All bot operational data goes here — codes, files, verifications, stats, logs.
// Every document has an InstanceID field for multi-tenant filtering.
//
// Swap to a different document DB: implement this interface and wire in main.go.
type DocumentStore interface {
	// Collection returns a typed collection helper for a given name.
	Collection(name string) Collection

	// Ping checks the connection.
	Ping(ctx context.Context) error

	// Close closes the connection.
	Close(ctx context.Context) error
}

// Collection is a typed helper for a single MongoDB collection.
type Collection interface {
	// InsertOne inserts a single document.
	InsertOne(ctx context.Context, doc any) (string, error)

	// FindOne finds a single document matching filter.
	FindOne(ctx context.Context, filter any, result any) error

	// Find finds multiple documents matching filter.
	Find(ctx context.Context, filter any, results any, opts ...FindOption) error

	// UpdateOne updates the first document matching filter.
	UpdateOne(ctx context.Context, filter any, update any) error

	// DeleteOne deletes the first document matching filter.
	DeleteOne(ctx context.Context, filter any) error

	// CountDocuments counts documents matching filter.
	CountDocuments(ctx context.Context, filter any) (int64, error)

	// CreateIndex creates an index on the collection.
	CreateIndex(ctx context.Context, keys any, unique bool) error
}

// FindOption configures a Find call.
type FindOption func(*FindConfig)

// FindConfig holds options for a Find call.
type FindConfig struct {
	Limit int64
	Skip  int64
	Sort  any
}

func WithLimit(n int64) FindOption { return func(c *FindConfig) { c.Limit = n } }
func WithSkip(n int64) FindOption  { return func(c *FindConfig) { c.Skip = n } }
func WithSort(sort any) FindOption { return func(c *FindConfig) { c.Sort = sort } }

// ---- Common document fields ----

// DocBase is embedded in every MongoDB document.
// InstanceID enables multi-tenant filtering — every query must include it.
type DocBase struct {
	InstanceID string    `bson:"instance_id" json:"instance_id"`
	CreatedAt  time.Time `bson:"created_at"  json:"created_at"`
	UpdatedAt  time.Time `bson:"updated_at"  json:"updated_at"`
}
