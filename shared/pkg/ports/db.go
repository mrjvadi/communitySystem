package ports

import (
	"context"

	"gorm.io/gorm"
)

// DB is the interface for the relational database layer.
// Default implementation: PostgresDB (adapters/postgres).
// Swap to MySQL/SQLite by implementing this interface and wiring in main.go.
type DB interface {
	// Conn returns the underlying *gorm.DB for advanced queries.
	// Prefer typed store methods over direct access when possible.
	Conn() *gorm.DB

	// Ping checks the database connection.
	Ping(ctx context.Context) error

	// Migrate runs AutoMigrate for the given model pointers.
	Migrate(models ...any) error

	// Close releases the connection pool.
	Close() error
}
