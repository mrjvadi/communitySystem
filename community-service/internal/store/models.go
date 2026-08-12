package store

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CommunityType string
const (
	CommunityGroup   CommunityType = "group"
	CommunityChannel CommunityType = "channel"
)

type CommunityStatus string
const (
	CommunityPending   CommunityStatus = "pending"
	CommunityActive    CommunityStatus = "active"
	CommunitySuspended CommunityStatus = "suspended"
	CommunityRejected  CommunityStatus = "rejected"
)

// Community یک گروه یا کانال ثبت‌شده.
type Community struct {
	ID         uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt  `gorm:"index"`

	OwnerID    uuid.UUID       `gorm:"not null;index"`
	TelegramID int64           `gorm:"uniqueIndex;not null"`
	Type       CommunityType   `gorm:"not null"`
	Name       string          `gorm:"not null"`
	Username   string
	Status     CommunityStatus `gorm:"default:'pending';index"`

	// invite link اختصاصی برای attribution tracking
	InviteLink string `gorm:"uniqueIndex"`
	InviteHash string `gorm:"uniqueIndex"`

	// درصدهای تقسیم درآمد (0 = پیش‌فرض نوع)
	OwnerPercent   float64 `gorm:"default:0"`
	MembersPercent float64 `gorm:"default:0"`
	PlatformPercent float64 `gorm:"default:0"`

	MemberCount         int `gorm:"default:0"`
	QualityScore        int `gorm:"default:50"`
	ValidationWindowSec int `gorm:"default:86400"` // 24h

	VerifiedAt *time.Time
}

// RevenuePercentages درصدهای واقعی تقسیم.
func (c *Community) RevenuePercentages() (owner, members, platform float64) {
	if c.OwnerPercent > 0 {
		return c.OwnerPercent, c.MembersPercent, c.PlatformPercent
	}
	switch c.Type {
	case CommunityGroup:
		return 50, 40, 10
	case CommunityChannel:
		return 90, 0, 10
	}
	return 80, 0, 20
}

// CampaignParticipant شرکت کاربر در کمپین از طریق community.
type CampaignParticipant struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt   time.Time
	CampaignID  uuid.UUID  `gorm:"not null;index;uniqueIndex:idx_camp_user"`
	CommunityID uuid.UUID  `gorm:"not null;index"`
	TelegramID  int64      `gorm:"not null;index;uniqueIndex:idx_camp_user"`
	JoinedAt    time.Time
	ValidatedAt *time.Time
	LeftAt      *time.Time
	Status      string     `gorm:"default:'pending'"` // pending|validated|invalid|left
	RevenueEarned float64  `gorm:"default:0"`
}

func (p *CampaignParticipant) IsValid() bool {
	return p.Status == "validated"
}

// CommunityRevenue درآمد یک community از یک کمپین.
type CommunityRevenue struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt       time.Time
	CommunityID     uuid.UUID `gorm:"not null;index"`
	CampaignID      uuid.UUID `gorm:"not null;index"`
	TotalAmount     float64
	OwnerAmount     float64
	MembersAmount   float64
	PlatformAmount  float64
	ValidJoins      int
	Status          string    `gorm:"default:'pending'"` // pending|distributed|held|frozen
	DistributedAt   *time.Time
}

// CommunityDistribution پرداخت به یک member از pool.
type CommunityDistribution struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt     time.Time
	RevenueID     uuid.UUID `gorm:"not null;index"`
	CommunityID   uuid.UUID `gorm:"not null;index"`
	TelegramID    int64     `gorm:"not null;index"`
	Amount        float64
	ActivityScore int
	TxID          string
	Status        string `gorm:"default:'pending'"`
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Community{},
		&CampaignParticipant{},
		&CommunityRevenue{},
		&CommunityDistribution{},
	)
}

const (
	MinValidationWindow     = 3600       // 1h
	MaxValidationWindow     = 604800     // 7d
	DefaultValidationWindow = 86400      // 24h
)
