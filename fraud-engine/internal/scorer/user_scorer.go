// Package scorer محاسبه User Trust Score و Community Quality Score.
//
// اصول:
//   - هیچ سیگنالی به تنهایی کافی نیست
//   - رفتار بلندمدت مهم‌تر از پروفایل است
//   - هر امتیاز باید قابل توضیح باشد
package scorer

import (
	"context"
	"math"

	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

// UserScorer محاسبه User Trust Score.
type UserScorer struct {
	store *store.Store
	log   ports.Logger
}

func NewUserScorer(st *store.Store, log ports.Logger) *UserScorer {
	return &UserScorer{store: st, log: log}
}

// Calculate امتیاز اعتماد کاربر را محاسبه می‌کند.
// امتیاز از ۰ تا ۱۰۰ — هر چه بیشتر بهتر.
func (s *UserScorer) Calculate(ctx context.Context, telegramID int64) (int, store.ScoreBreakdown, error) {
	breakdown := store.ScoreBreakdown{}

	profile, _ := s.store.GetProfile(ctx, telegramID)
	if profile == nil {
		// کاربر ناشناخته — امتیاز میانگین
		return 50, breakdown, nil
	}

	// ── فاکتور ۱: مدت عضویت (0-25) ────────────────────────
	// کاربرانی که مدت بیشتری عضو می‌مانند معتمدترند
	memberships, _ := s.store.GetRecentMemberships(ctx, telegramID, 90) // ۹۰ روز اخیر
	avgDuration := avgMembershipDays(memberships)

	durationScore := 0.0
	switch {
	case avgDuration >= 30:
		durationScore = 25
	case avgDuration >= 14:
		durationScore = 20
	case avgDuration >= 7:
		durationScore = 15
	case avgDuration >= 3:
		durationScore = 10
	case avgDuration >= 1:
		durationScore = 5
	default:
		durationScore = 0
		breakdown.Reasons = append(breakdown.Reasons, "مدت عضویت خیلی کوتاه")
	}
	breakdown.MembershipDuration = durationScore

	// ── فاکتور ۲: نسبت Join/Leave (0-20) ───────────────────
	// join بیش از leave → مشکوک
	joinLeaveScore := 0.0
	if profile.TotalJoins == 0 {
		joinLeaveScore = 10 // neutral
	} else {
		ratio := float64(profile.TotalLeaves) / float64(profile.TotalJoins)
		switch {
		case ratio > 0.9:
			// تقریباً هر join با leave همراه → بسیار مشکوک
			joinLeaveScore = 0
			breakdown.Reasons = append(breakdown.Reasons, "رفتار join/leave غیرعادی")
		case ratio > 0.7:
			joinLeaveScore = 5
		case ratio > 0.5:
			joinLeaveScore = 10
		case ratio > 0.3:
			joinLeaveScore = 15
		default:
			joinLeaveScore = 20
		}
	}
	breakdown.JoinLeaveRatio = joinLeaveScore

	// ── فاکتور ۳: فعالیت (0-20) ───────────────────────────
	// روزهای فعال در ۳۰ روز اخیر
	activeDays, _ := s.store.GetActivityDays(ctx, telegramID, 30)
	activityScore := math.Min(float64(activeDays)*0.67, 20) // max در ۳۰ روز
	if activeDays == 0 {
		breakdown.Reasons = append(breakdown.Reasons, "هیچ فعالیتی ثبت نشده")
	}
	breakdown.ActivityScore = activityScore

	// ── فاکتور ۴: retention (0-20) ────────────────────────
	// چند درصد از کانال‌هایی که عضو شده هنوز عضو است
	retentionScore := calcRetentionScore(memberships)
	if retentionScore < 5 {
		breakdown.Reasons = append(breakdown.Reasons, "نرخ ماندن پایین")
	}
	breakdown.RetentionScore = retentionScore

	// ── فاکتور ۵: تنوع communities (0-10) ─────────────────
	// کاربر در چند community مختلف عضو است (تنوع طبیعی)
	communityIDs := uniqueCommunities(memberships)
	diversityScore := 0.0
	switch len(communityIDs) {
	case 0:
		diversityScore = 0
	case 1, 2:
		diversityScore = 3
	case 3, 4, 5:
		diversityScore = 7
	default:
		// بیش از ۵ community → ممکنه غیرطبیعی باشه
		if len(communityIDs) > 20 {
			diversityScore = 3
			breakdown.Reasons = append(breakdown.Reasons, "تعداد community خیلی زیاد")
		} else {
			diversityScore = 10
		}
	}
	breakdown.DiversityScore = diversityScore

	// ── فاکتور ۶: کیفیت تبلیغات (0-5) ───────────────────
	adScore := 0.0
	if profile.TotalCampaigns > 0 {
		completionRate := float64(profile.AdCompletions) / float64(profile.TotalCampaigns)
		adScore = completionRate * 5
	}
	breakdown.AdQualityScore = adScore

	// ── جمع نهایی ─────────────────────────────────────────
	total := durationScore + joinLeaveScore + activityScore + retentionScore + diversityScore + adScore
	score := int(math.Min(total, 100))

	// کاهش اضافی برای کاربران با پروفایل مشکوک
	if !profile.HasPhoto && profile.Username == "" {
		score = int(float64(score) * 0.85)
		breakdown.Reasons = append(breakdown.Reasons, "پروفایل ناقص")
	}
	if profile.IsBot {
		score = int(float64(score) * 0.5)
		breakdown.Reasons = append(breakdown.Reasons, "اکانت bot")
	}

	return max(score, 0), breakdown, nil
}

// ── helpers ────────────────────────────────────────────────

func avgMembershipDays(memberships []store.UserMembership) float64 {
	if len(memberships) == 0 {
		return 0
	}
	total := 0.0
	for _, m := range memberships {
		total += m.DurationDays()
	}
	return total / float64(len(memberships))
}

func calcRetentionScore(memberships []store.UserMembership) float64 {
	if len(memberships) == 0 {
		return 10 // neutral
	}
	active := 0
	for _, m := range memberships {
		if m.LeftAt == nil {
			active++
		}
	}
	ratio := float64(active) / float64(len(memberships))
	return ratio * 20
}

func uniqueCommunities(memberships []store.UserMembership) map[int64]bool {
	ids := map[int64]bool{}
	for _, m := range memberships {
		ids[m.CommunityID] = true
	}
	return ids
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
