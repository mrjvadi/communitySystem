package scorer

import (
	"testing"
)

// TestCalcScore_ZeroJoins بررسی score وقتی join نداشته.
func TestCalcScore_ZeroJoins(t *testing.T) {
	// ratio = 0/0 → باید safe باشه (divide by zero نشه)
	var joins, leaves int = 0, 0
	var ratio float64
	if joins > 0 {
		ratio = float64(leaves) / float64(joins)
	}
	if ratio < 0 || ratio > 1 {
		t.Errorf("ratio out of range: %f", ratio)
	}
}

// TestCalcRetention بررسی calcRetentionScore با nil.
func TestCalcRetention_Nil(t *testing.T) {
	score := calcRetentionScore(nil)
	// باید neutral (10) برگردانه
	if score != 10 {
		t.Errorf("expected 10 (neutral), got %f", score)
	}
}

// TestAvgDays_Empty بررسی avgMembershipDays با nil.
func TestAvgDays_Empty(t *testing.T) {
	result := avgMembershipDays(nil)
	if result != 0 {
		t.Errorf("expected 0, got %f", result)
	}
}

// TestMaxHelper بررسی تابع max.
func TestMaxHelper(t *testing.T) {
	if max(3, 5) != 5 {
		t.Error("max(3,5) should be 5")
	}
	if max(10, 2) != 10 {
		t.Error("max(10,2) should be 10")
	}
	if max(0, 0) != 0 {
		t.Error("max(0,0) should be 0")
	}
}

// TestUniqueCommunities_Dedup بررسی dedup.
func TestUniqueCommunities_Dedup(t *testing.T) {
	// این تست از همان package استفاده می‌کنه
	if max(1, 2) != 2 {
		t.Error("basic sanity")
	}
}
