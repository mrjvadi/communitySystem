package scorer

import (
	"testing"

	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
)

// ── RevenueMultiplier ─────────────────────────────────────

func TestRevenueMultiplier(t *testing.T) {
	cases := []struct {
		score    int
		expected float64
	}{
		{100, 1.0},
		{80, 1.0},
		{79, 0.8},
		{60, 0.8},
		{50, 0.8},
		{49, 0.5},
		{30, 0.5},
		{29, 0.0},
		{0, 0.0},
	}

	for _, tc := range cases {
		got := RevenueMultiplier(tc.score)
		if got != tc.expected {
			t.Errorf("RevenueMultiplier(%d) = %.1f, want %.1f",
				tc.score, got, tc.expected)
		}
	}
}

// ── WeightedMemberCount ───────────────────────────────────

func TestWeightedMemberCount_Empty(t *testing.T) {
	result := WeightedMemberCount(nil)
	if result != 0 {
		t.Errorf("expected 0 for empty, got %.2f", result)
	}
}

func TestWeightedMemberCount_AllPerfect(t *testing.T) {
	scores := []int{100, 100, 100}
	result := WeightedMemberCount(scores)
	if result != 3.0 {
		t.Errorf("expected 3.0, got %.2f", result)
	}
}

func TestWeightedMemberCount_Mixed(t *testing.T) {
	// spec: 100 + 50 + 10 = 1.6 (نه 3)
	scores := []int{100, 50, 10}
	result := WeightedMemberCount(scores)
	expected := 1.0 + 0.5 + 0.1 // = 1.6
	if result < expected-0.01 || result > expected+0.01 {
		t.Errorf("expected %.2f, got %.2f", expected, result)
	}
}

func TestWeightedMemberCount_AllFake(t *testing.T) {
	// همه با score 10 (high_risk)
	scores := []int{10, 10, 10, 10, 10}
	result := WeightedMemberCount(scores)
	// 5 × 0.1 = 0.5 (نه 5)
	expected := 0.5
	if result < expected-0.01 || result > expected+0.01 {
		t.Errorf("expected %.2f, got %.2f", expected, result)
	}
}

// ── calcCommunityRetention ────────────────────────────────

func TestCalcCommunityRetention_NoData(t *testing.T) {
	stats := &store.CommunityStatistics{MemberCount: 0, TotalJoins: 0}
	result := calcCommunityRetention(stats)
	if result != 0.5 {
		t.Errorf("expected 0.5 (neutral), got %.2f", result)
	}
}

func TestCalcCommunityRetention_HighRetention(t *testing.T) {
	// ۱۰۰۰ join، ۸۰۰ هنوز عضو
	stats := &store.CommunityStatistics{MemberCount: 800, TotalJoins: 1000}
	result := calcCommunityRetention(stats)
	if result < 0.79 || result > 0.81 {
		t.Errorf("expected ~0.80, got %.2f", result)
	}
}

func TestCalcCommunityRetention_LowRetention(t *testing.T) {
	// ۱۰۰۰ join، ۱۰۰ هنوز عضو (farm pattern)
	stats := &store.CommunityStatistics{MemberCount: 100, TotalJoins: 1000}
	result := calcCommunityRetention(stats)
	if result < 0.09 || result > 0.11 {
		t.Errorf("expected ~0.10, got %.2f", result)
	}
}

// ── Revenue rules ─────────────────────────────────────────

func TestCommunityRevenueRules(t *testing.T) {
	// score بالا → پرداخت کامل
	if RevenueMultiplier(85) != 1.0 {
		t.Error("score 85: expected full payout")
	}
	// score متوسط → ۸۰٪
	if RevenueMultiplier(65) != 0.8 {
		t.Error("score 65: expected 0.8 multiplier")
	}
	// score پایین → ۵۰٪
	if RevenueMultiplier(40) != 0.5 {
		t.Error("score 40: expected 0.5 multiplier")
	}
	// score خیلی پایین → فریز
	if RevenueMultiplier(20) != 0.0 {
		t.Error("score 20: expected 0.0 (frozen)")
	}
}
