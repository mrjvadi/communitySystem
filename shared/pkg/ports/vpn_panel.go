package ports

import (
	"context"
	"time"
)

// VPNPanel is the interface every VPN panel adapter must implement.
// Implementations: MarzbanPanel, MarzneshinPanel, HiddifyPanel, XUIPanel, etc.
// Add a new panel by implementing this interface — no other file changes needed.
type VPNPanel interface {
	// Name returns the panel type identifier (e.g. "marzban", "hiddify").
	Name() string

	// Login authenticates and caches credentials internally.
	Login(ctx context.Context) error

	// CreateUser creates a VPN user and returns its info.
	CreateUser(ctx context.Context, req CreateVPNUserRequest) (*VPNUser, error)

	// GetUser retrieves a user by username.
	GetUser(ctx context.Context, username string) (*VPNUser, error)

	// UpdateUser updates expiry and/or data limit for an existing user.
	UpdateUser(ctx context.Context, username string, req UpdateVPNUserRequest) (*VPNUser, error)

	// EnableUser re-activates a disabled user.
	EnableUser(ctx context.Context, username string) error

	// DisableUser deactivates a user (does not delete).
	DisableUser(ctx context.Context, username string) error

	// DeleteUser permanently removes a user.
	DeleteUser(ctx context.Context, username string) error

	// ActiveCount returns the number of currently active users.
	ActiveCount(ctx context.Context) (int, error)
}

// VPNUserStatus is the status of a VPN user on the panel.
type VPNUserStatus string

const (
	VPNUserActive   VPNUserStatus = "active"
	VPNUserDisabled VPNUserStatus = "disabled"
	VPNUserExpired  VPNUserStatus = "expired"
	VPNUserLimited  VPNUserStatus = "limited"
)

// VPNUser is the common representation of a user across all panels.
type VPNUser struct {
	Username  string
	Status    VPNUserStatus
	DataLimit int64     // bytes; 0 = unlimited
	UsedData  int64     // bytes
	ExpiresAt time.Time // zero = never
	Links     []string  // subscription links
}

// CreateVPNUserRequest is the input for CreateUser.
type CreateVPNUserRequest struct {
	Username  string
	DataLimit int64     // bytes; 0 = unlimited
	ExpiresAt time.Time // zero = never
}

// UpdateVPNUserRequest is the input for UpdateUser.
type UpdateVPNUserRequest struct {
	DataLimit *int64     // nil = no change
	ExpiresAt *time.Time // nil = no change
}
