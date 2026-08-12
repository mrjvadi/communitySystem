// Package store مدل‌های MongoDB برای fraud-engine.
//
// Collections:
//   user_profiles         — پروفایل فعلی کاربر
//   user_profile_history  — تاریخچه تغییرات پروفایل
//   user_memberships      — تاریخچه عضویت در گروه/کانال
//   user_activity         — فعالیت کاربر در communities
//   user_scores           — امتیاز اعتماد کاربر (تاریخچه)
//   community_scores      — امتیاز کیفیت کانال (تاریخچه)
//   community_statistics  — آمار لحظه‌ای کانال
//   fraud_events          — رویدادهای مشکوک
package store

import (
	"time"
)

// ── User Profile ───────────────────────────────────────────

// UserProfile پروفایل فعلی کاربر — یک doc به ازای هر TelegramID.
type UserProfile struct {
	TelegramID  int64     `bson:"_id"`
	Username    string    `bson:"username"`
	FirstName   string    `bson:"first_name"`
	LastName    string    `bson:"last_name"`
	HasPhoto    bool      `bson:"has_photo"`
	IsBot       bool      `bson:"is_bot"`
	FirstSeen   time.Time `bson:"first_seen"`
	LastSeen    time.Time `bson:"last_seen"`
	UpdatedAt   time.Time `bson:"updated_at"`

	// امتیاز فعلی — همیشه آپدیت می‌شود
	TrustScore  int       `bson:"trust_score"`  // 0-100
	ScoreLabel  string    `bson:"score_label"`  // high_risk | suspicious | normal | trusted

	// شمارنده‌های رفتاری
	TotalJoins      int `bson:"total_joins"`
	TotalLeaves     int `bson:"total_leaves"`
	TotalCampaigns  int `bson:"total_campaigns"`
	AdCompletions   int `bson:"ad_completions"`   // تبلیغ‌هایی که در آن‌ها ماند
}

// UserProfileHistory یک تغییر در پروفایل.
type UserProfileHistory struct {
	TelegramID int64     `bson:"telegram_id"`
	Field      string    `bson:"field"`      // username | first_name | photo | ...
	OldValue   string    `bson:"old_value"`
	NewValue   string    `bson:"new_value"`
	ChangedAt  time.Time `bson:"changed_at"`
}

// ── Membership ─────────────────────────────────────────────

// UserMembership یک دوره عضویت در یک کانال/گروه.
type UserMembership struct {
	TelegramID   int64      `bson:"telegram_id"`
	CommunityID  int64      `bson:"community_id"`
	JoinedAt     time.Time  `bson:"joined_at"`
	LeftAt       *time.Time `bson:"left_at,omitempty"`
	DurationSec  int64      `bson:"duration_sec"`  // -1 اگه هنوز عضو است
	RejoinCount  int        `bson:"rejoin_count"`  // چند بار به همین کانال برگشته
	Source       string     `bson:"source"`        // ad | organic | invite
	CampaignID   string     `bson:"campaign_id,omitempty"`
}

// DurationDays مدت عضویت به روز.
func (m *UserMembership) DurationDays() float64 {
	if m.DurationSec < 0 {
		return time.Since(m.JoinedAt).Hours() / 24
	}
	return float64(m.DurationSec) / 86400
}

// IsShortTerm کمتر از ۲۴ ساعت عضو بوده.
func (m *UserMembership) IsShortTerm() bool {
	return m.DurationDays() < 1
}

// ── Activity ───────────────────────────────────────────────

// UserActivity فعالیت کاربر در یک کانال.
type UserActivity struct {
	TelegramID  int64     `bson:"telegram_id"`
	CommunityID int64     `bson:"community_id"`
	Date        string    `bson:"date"`    // "2024-01-15" — یک record روزانه
	Messages    int       `bson:"messages"`
	Replies     int       `bson:"replies"`
	Reactions   int       `bson:"reactions"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

// ── Scores ─────────────────────────────────────────────────

// UserScoreSnapshot یک امتیاز در یک لحظه.
type UserScoreSnapshot struct {
	TelegramID   int64     `bson:"telegram_id"`
	Score        int       `bson:"score"`
	Breakdown    ScoreBreakdown `bson:"breakdown"`
	CalculatedAt time.Time `bson:"calculated_at"`
}

// ScoreBreakdown جزئیات محاسبه امتیاز.
type ScoreBreakdown struct {
	MembershipDuration float64 `bson:"membership_duration"` // 0-25
	JoinLeaveRatio     float64 `bson:"join_leave_ratio"`    // 0-20
	ActivityScore      float64 `bson:"activity_score"`      // 0-20
	RetentionScore     float64 `bson:"retention_score"`     // 0-20
	DiversityScore     float64 `bson:"diversity_score"`     // 0-10
	AdQualityScore     float64 `bson:"ad_quality_score"`    // 0-5
	Reasons            []string `bson:"reasons"`
}

// CommunityScoreSnapshot امتیاز یک کانال.
type CommunityScoreSnapshot struct {
	CommunityID   int64     `bson:"community_id"`
	Score         int       `bson:"score"`
	Breakdown     CommunityBreakdown `bson:"breakdown"`
	CalculatedAt  time.Time `bson:"calculated_at"`

	// وضعیت پرداخت بر اساس امتیاز
	RevenueStatus string    `bson:"revenue_status"` // normal | monitored | partial_hold | frozen
}

type CommunityBreakdown struct {
	RetentionRate    float64 `bson:"retention_rate"`     // % که می‌مانند
	ActivityRate     float64 `bson:"activity_rate"`      // % که فعالند
	AvgMemberQuality float64 `bson:"avg_member_quality"` // میانگین trust score اعضا
	JoinLeaveRate    float64 `bson:"join_leave_rate"`    // نسبت join/leave
	AdConversionRate float64 `bson:"ad_conversion_rate"` // % که بعد از تبلیغ می‌مانند
	Reasons          []string `bson:"reasons"`
}

// CommunityStatistics آمار لحظه‌ای کانال.
type CommunityStatistics struct {
	CommunityID    int64     `bson:"_id"`
	MemberCount    int       `bson:"member_count"`
	ActiveMembers  int       `bson:"active_members"`   // ۳۰ روز اخیر
	TotalJoins     int       `bson:"total_joins"`
	TotalLeaves    int       `bson:"total_leaves"`
	AvgTrustScore  float64   `bson:"avg_trust_score"`
	UpdatedAt      time.Time `bson:"updated_at"`
}

// ── Fraud Events ───────────────────────────────────────────

type FraudEventType string

const (
	FraudJoinLeaveLoop     FraudEventType = "join_leave_loop"
	FraudMassJoin          FraudEventType = "mass_join"
	FraudSuspiciousProfile FraudEventType = "suspicious_profile"
	FraudBotBehavior       FraudEventType = "bot_behavior"
	FraudClickFarm         FraudEventType = "click_farm"
	FraudLowRetention      FraudEventType = "low_retention"
)

// FraudEvent یک رویداد مشکوک تشخیص داده‌شده.
type FraudEvent struct {
	EventType   FraudEventType `bson:"event_type"`
	TelegramID  *int64         `bson:"telegram_id,omitempty"`
	CommunityID *int64         `bson:"community_id,omitempty"`
	Score       int            `bson:"score"`       // شدت (0-100)
	Description string         `bson:"description"`
	Evidence    map[string]any `bson:"evidence"`
	DetectedAt  time.Time      `bson:"detected_at"`
	Reviewed    bool           `bson:"reviewed"`
}

// ── Score Labels ───────────────────────────────────────────

func UserScoreLabel(score int) string {
	switch {
	case score >= 80:
		return "trusted"
	case score >= 60:
		return "normal"
	case score >= 30:
		return "suspicious"
	default:
		return "high_risk"
	}
}

func CommunityRevenueStatus(score int) string {
	switch {
	case score >= 80:
		return "normal"
	case score >= 50:
		return "monitored"
	case score >= 30:
		return "partial_hold"
	default:
		return "frozen"
	}
}
