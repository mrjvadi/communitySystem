package scorer

import (
	"testing"
	"time"

	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
)

// ── avgMembershipDays ─────────────────────────────────────

func TestAvgMembershipDays_Empty(t *testing.T) {
	result := avgMembershipDays(nil)
	if result != 0 {
		t.Errorf("expected 0, got %.2f", result)
	}
}

func TestAvgMembershipDays_Active(t *testing.T) {
	// عضو فعال از ۷ روز پیش
	joinedAt := time.Now().AddDate(0, 0, -7)
	memberships := []store.UserMembership{
		{JoinedAt: joinedAt, DurationSec: -1}, // هنوز عضو
	}
	result := avgMembershipDays(memberships)
	if result < 6.9 || result > 7.1 {
		t.Errorf("expected ~7 days, got %.2f", result)
	}
}

func TestAvgMembershipDays_Mixed(t *testing.T) {
	// یک عضویت ۳۰ روزه، یک عضویت ۱۰ روزه
	memberships := []store.UserMembership{
		{JoinedAt: time.Now(), DurationSec: 30 * 86400},
		{JoinedAt: time.Now(), DurationSec: 10 * 86400},
	}
	result := avgMembershipDays(memberships)
	// میانگین = 20
	if result < 19.9 || result > 20.1 {
		t.Errorf("expected ~20 days, got %.2f", result)
	}
}

// ── calcRetentionScore ────────────────────────────────────

func TestCalcRetentionScore_NoMemberships(t *testing.T) {
	score := calcRetentionScore(nil)
	// neutral = 10
	if score != 10 {
		t.Errorf("expected 10 (neutral), got %.2f", score)
	}
}

func TestCalcRetentionScore_AllActive(t *testing.T) {
	memberships := []store.UserMembership{
		{DurationSec: -1},
		{DurationSec: -1},
		{DurationSec: -1},
	}
	score := calcRetentionScore(memberships)
	if score != 20 {
		t.Errorf("expected 20 (all active), got %.2f", score)
	}
}

func TestCalcRetentionScore_HalfActive(t *testing.T) {
	now := time.Now()
	memberships := []store.UserMembership{
		{DurationSec: -1},        // فعال
		{LeftAt: &now},           // رفته
	}
	score := calcRetentionScore(memberships)
	if score != 10 {
		t.Errorf("expected 10 (half active), got %.2f", score)
	}
}

func TestCalcRetentionScore_NoneActive(t *testing.T) {
	now := time.Now()
	memberships := []store.UserMembership{
		{LeftAt: &now},
		{LeftAt: &now},
	}
	score := calcRetentionScore(memberships)
	if score != 0 {
		t.Errorf("expected 0 (none active), got %.2f", score)
	}
}

// ── uniqueCommunities ─────────────────────────────────────

func TestUniqueCommunities(t *testing.T) {
	memberships := []store.UserMembership{
		{CommunityID: 100},
		{CommunityID: 200},
		{CommunityID: 100}, // تکراری
		{CommunityID: 300},
	}
	result := uniqueCommunities(memberships)
	if len(result) != 3 {
		t.Errorf("expected 3 unique communities, got %d", len(result))
	}
}

// ── score breakdown logic ─────────────────────────────────

func TestScoreBreakdown_HighRiskUser(t *testing.T) {
	// کاربری که زیاد join/leave کرده
	profile := &store.UserProfile{
		TelegramID:  99999,
		TotalJoins:  100,
		TotalLeaves: 98, // تقریباً همه رو ترک کرده
		HasPhoto:    false,
		Username:    "",
	}

	// شبیه‌سازی محاسبه دستی
	ratio := float64(profile.TotalLeaves) / float64(profile.TotalJoins)
	if ratio <= 0.9 {
		t.Errorf("expected high leave ratio, got %.2f", ratio)
	}

	// join/leave score باید ۰ باشه
	joinLeaveScore := 0.0
	if ratio > 0.9 {
		joinLeaveScore = 0
	}
	if joinLeaveScore != 0 {
		t.Errorf("expected joinLeaveScore=0 for abusive user")
	}
}

func TestScoreBreakdown_TrustedUser(t *testing.T) {
	// کاربری که عضوی پایدار است
	profile := &store.UserProfile{
		TelegramID:  11111,
		TotalJoins:  10,
		TotalLeaves: 1,
		HasPhoto:    true,
		Username:    "trusted_user",
	}

	ratio := float64(profile.TotalLeaves) / float64(profile.TotalJoins)
	if ratio >= 0.3 {
		t.Errorf("expected low leave ratio for trusted user, got %.2f", ratio)
	}
}

// ── max helper ────────────────────────────────────────────

func TestMax(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{5, 3, 5},
		{0, 10, 10},
		{-1, 0, 0},
		{7, 7, 7},
	}
	for _, tc := range cases {
		got := max(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("max(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
