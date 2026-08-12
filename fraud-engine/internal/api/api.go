package api

import (
	"crypto/subtle"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mrjvadi/communitySystem/fraud-engine/internal/scorer"
	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
)

type Handler struct {
	store           *store.Store
	userScorer      *scorer.UserScorer
	communityScorer *scorer.CommunityScorer
	adminKey        string
}

func New(st *store.Store, us *scorer.UserScorer, cs *scorer.CommunityScorer, adminKey string) *Handler {
	return &Handler{store: st, userScorer: us, communityScorer: cs, adminKey: adminKey}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.health)

	// public — سرویس‌های دیگر می‌توانند بدون auth بخوانند
	r.GET("/score/user/:id",      h.getUserScore)
	r.GET("/score/community/:id", h.getCommunityScore)

	// admin
	admin := r.Group("/admin", h.authMiddleware)
	admin.GET("/fraud-events",        h.getFraudEvents)
	admin.GET("/user/:id/profile",    h.getUserProfile)
	admin.POST("/score/user/:id/recalc",      h.recalcUser)
	admin.POST("/score/community/:id/recalc", h.recalcCommunity)
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(200, gin.H{"ok": true, "service": "fraud-engine"})
}

// GET /score/user/:id
func (h *Handler) getUserScore(c *gin.Context) {
	telegramID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "invalid id"})
		return
	}

	profile, _ := h.store.GetProfile(c.Request.Context(), telegramID)
	if profile == nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"telegram_id": telegramID,
			"score":       50,
			"label":       "normal",
			"known":       false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"telegram_id": telegramID,
		"score":       profile.TrustScore,
		"label":       profile.ScoreLabel,
		"known":       true,
		"first_seen":  profile.FirstSeen,
		"last_seen":   profile.LastSeen,
	})
}

// GET /score/community/:id
func (h *Handler) getCommunityScore(c *gin.Context) {
	communityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "invalid id"})
		return
	}

	snap, _ := h.store.GetLatestCommunityScore(c.Request.Context(), communityID)
	if snap == nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":                 true,
			"community_id":       communityID,
			"score":              50,
			"revenue_status":     "monitored",
			"revenue_multiplier": 0.8,
			"known":              false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":                 true,
		"community_id":       communityID,
		"score":              snap.Score,
		"revenue_status":     snap.RevenueStatus,
		"revenue_multiplier": scorer.RevenueMultiplier(snap.Score),
		"breakdown":          snap.Breakdown,
		"calculated_at":      snap.CalculatedAt,
		"known":              true,
	})
}

// POST /admin/score/user/:id/recalc
func (h *Handler) recalcUser(c *gin.Context) {
	telegramID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	ctx := c.Request.Context()

	score, breakdown, err := h.userScorer.Calculate(ctx, telegramID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": err.Error()})
		return
	}

	h.store.UpdateTrustScore(ctx, telegramID, score, breakdown)
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"score":     score,
		"label":     store.UserScoreLabel(score),
		"breakdown": breakdown,
	})
}

// POST /admin/score/community/:id/recalc
func (h *Handler) recalcCommunity(c *gin.Context) {
	communityID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	ctx := c.Request.Context()

	score, breakdown, err := h.communityScorer.Calculate(ctx, communityID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": err.Error()})
		return
	}

	h.store.UpdateCommunityScore(ctx, communityID, score, breakdown)
	c.JSON(http.StatusOK, gin.H{
		"ok":                 true,
		"score":              score,
		"revenue_status":     store.CommunityRevenueStatus(score),
		"revenue_multiplier": scorer.RevenueMultiplier(score),
		"breakdown":          breakdown,
	})
}

// GET /admin/fraud-events
func (h *Handler) getFraudEvents(c *gin.Context) {
	var req struct {
		TelegramID int64 `form:"telegram_id"`
	}
	c.ShouldBindQuery(&req)
	events, _ := h.store.GetFraudEvents(c.Request.Context(), req.TelegramID, 50)
	c.JSON(http.StatusOK, gin.H{"ok": true, "events": events, "count": len(events)})
}

// GET /admin/user/:id/profile
func (h *Handler) getUserProfile(c *gin.Context) {
	telegramID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	profile, _ := h.store.GetProfile(c.Request.Context(), telegramID)
	if profile == nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "message": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "profile": profile})
}

func (h *Handler) authMiddleware(c *gin.Context) {
	// fail-closed: اگر کلید ادمین تنظیم نشده باشد، هیچ درخواستی نباید به مسیرهای
	// ادمین برسد (در غیر این صورت هدر غایب == کلید خالی → دسترسی باز می‌شد).
	if h.adminKey == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false})
		return
	}
	got := c.GetHeader("X-Admin-Key")
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.adminKey)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false})
		return
	}
	c.Next()
}
