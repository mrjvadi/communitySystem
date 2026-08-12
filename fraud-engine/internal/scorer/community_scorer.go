package scorer

import (
	"context"
	"math"
	"time"

	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

// CommunityScorer محاسبه Community Quality Score.
type CommunityScorer struct {
	store *store.Store
	log   ports.Logger
}

func NewCommunityScorer(st *store.Store, log ports.Logger) *CommunityScorer {
	return &CommunityScorer{store: st, log: log}
}

// Calculate امتیاز کیفیت یک community را محاسبه می‌کند.
func (s *CommunityScorer) Calculate(ctx context.Context, communityID int64) (int, store.CommunityBreakdown, error) {
	breakdown := store.CommunityBreakdown{}

	stats, _ := s.store.GetCommunityStats(ctx, communityID)
	if stats == nil || stats.MemberCount == 0 {
		return 50, breakdown, nil
	}

	// ── فاکتور ۱: Retention Rate (0-30) ───────────────────
	// چند درصد از join ها می‌مانند (در ۳۰ روز اخیر)
	retentionRate := calcCommunityRetention(stats)
	retentionScore := retentionRate * 30
	breakdown.RetentionRate = retentionRate

	if retentionRate < 0.3 {
		breakdown.Reasons = append(breakdown.Reasons, "نرخ ماندن اعضا پایین است")
	}

	// ── فاکتور ۲: Activity Rate (0-20) ─────────────────────
	// چند درصد از اعضا فعالند
	activityRate := 0.0
	if stats.MemberCount > 0 {
		activityRate = float64(stats.ActiveMembers) / float64(stats.MemberCount)
	}
	activityScore := math.Min(activityRate*20, 20)
	breakdown.ActivityRate = activityRate

	if activityRate < 0.1 {
		breakdown.Reasons = append(breakdown.Reasons, "فعالیت اعضا خیلی پایین است")
	}

	// ── فاکتور ۳: Member Quality (0-30) ────────────────────
	// میانگین trust score اعضا
	avgTrust, _ := s.store.GetAvgTrustScoreForCommunity(ctx, communityID)
	memberQualityScore := avgTrust * 0.30 // max 30
	breakdown.AvgMemberQuality = avgTrust

	if avgTrust < 40 {
		breakdown.Reasons = append(breakdown.Reasons, "میانگین اعتماد اعضا پایین است")
	}

	// ── فاکتور ۴: Join/Leave Balance (0-10) ───────────────
	joinLeaveScore := 0.0
	if stats.TotalJoins > 0 {
		leaveRatio := float64(stats.TotalLeaves) / float64(stats.TotalJoins)
		breakdown.JoinLeaveRate = leaveRatio
		switch {
		case leaveRatio > 0.9:
			joinLeaveScore = 0
			breakdown.Reasons = append(breakdown.Reasons, "نرخ خروج خیلی بالاست — احتمال farm")
		case leaveRatio > 0.7:
			joinLeaveScore = 3
		case leaveRatio > 0.5:
			joinLeaveScore = 6
		default:
			joinLeaveScore = 10
		}
	} else {
		joinLeaveScore = 5
	}

	// ── فاکتور ۵: Ad Conversion Quality (0-10) ────────────
	// کاربرانی که از طریق تبلیغ آمدند و ماندند
	adConversionScore := 5.0 // default neutral
	breakdown.AdConversionRate = 0.5

	// ── جمع ───────────────────────────────────────────────
	total := retentionScore + activityScore + memberQualityScore + joinLeaveScore + adConversionScore
	score := int(math.Min(total, 100))

	return max(score, 0), breakdown, nil
}

// ── helpers ────────────────────────────────────────────────

func calcCommunityRetention(stats *store.CommunityStatistics) float64 {
	if stats.TotalJoins == 0 {
		return 0.5
	}
	// ساده: اعضای فعلی / کل join ها
	retention := float64(stats.MemberCount) / float64(stats.TotalJoins)
	return math.Min(retention, 1.0)
}

// WeightedMemberCount تعداد اعضا با وزن trust score.
// به‌جای raw count از این استفاده می‌شود.
//
// مثال:
//
//	User A: score=100 → weight=1.0
//	User B: score=50  → weight=0.5
//	User C: score=10  → weight=0.1
//
//	WeightedCount = 1.0 + 0.5 + 0.1 = 1.6 (به جای 3)
func WeightedMemberCount(memberScores []int) float64 {
	total := 0.0
	for _, score := range memberScores {
		total += float64(score) / 100.0
	}
	return total
}

// RevenueMultiplier ضریب درآمد بر اساس امتیاز community.
func RevenueMultiplier(communityScore int) float64 {
	switch {
	case communityScore >= 80:
		return 1.0 // پرداخت کامل
	case communityScore >= 50:
		return 0.8 // ۸۰٪
	case communityScore >= 30:
		return 0.5 // ۵۰٪ — نیمی نگه داشته می‌شود
	default:
		return 0.0 // فریز — بررسی دستی
	}
}

// now برای test مفید است.
var now = time.Now
