package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/community-service/internal/engine"
	"github.com/mrjvadi/communitySystem/community-service/internal/store"
)

type Handler struct {
	store    *store.Store
	engine   *engine.Engine
	nc       *natsclient.Client
	adminKey string
}

func New(st *store.Store, eng *engine.Engine, nc *natsclient.Client, adminKey string) *Handler {
	return &Handler{store: st, engine: eng, nc: nc, adminKey: adminKey}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "service": "community-service"})
	})

	// community registration — از ربات تلگرام صدا زده می‌شود
	comm := r.Group("/community")
	comm.POST("/register",        h.registerCommunity)
	comm.GET("/:id",              h.getCommunity)
	comm.GET("/by-chat/:chat_id", h.getCommunityByChatID)
	comm.POST("/:id/validation-window", h.setValidationWindow)

	// owner endpoints
	owner := r.Group("/owner/:telegram_id")
	owner.GET("/communities", h.listOwnerCommunities)

	// admin
	admin := r.Group("/admin", h.authMiddleware)
	admin.GET("/communities/pending", h.listPendingCommunities)
	admin.POST("/communities/:id/approve", h.approveCommunity)
	admin.POST("/communities/:id/reject",  h.rejectCommunity)
	admin.POST("/revenue/distribute",      h.triggerDistribute)
}

// POST /community/register
func (h *Handler) registerCommunity(c *gin.Context) {
	var req struct {
		OwnerTelegramID int64  `json:"owner_telegram_id" binding:"required"`
		ChatID          int64  `json:"chat_id" binding:"required"`
		Type            string `json:"type" binding:"required"` // group | channel
		Name            string `json:"name" binding:"required"`
		Username        string `json:"username"`
		InviteLink      string `json:"invite_link"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// بررسی تکراری نبودن
	existing, _ := h.store.FindCommunityByChatID(ctx, req.ChatID)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"ok": false, "message": "community already registered",
			"community_id": existing.ID,
		})
		return
	}

	comm := &store.Community{
		TelegramID: req.ChatID,
		Type:            store.CommunityType(req.Type),
		Name:            req.Name,
		Username:        req.Username,
		InviteLink:      req.InviteLink,
		Status:          store.CommunityPending,
		QualityScore:    50, // default تا fraud-engine محاسبه کنه
	}

	if err := h.store.CreateCommunity(ctx, comm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"ok": true, "community": comm})
}

// GET /community/:id
func (h *Handler) getCommunity(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false})
		return
	}
	comm, _ := h.store.FindCommunityByID(c.Request.Context(), id)
	if comm == nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false})
		return
	}
	ownerPct, memberPct, platformPct := func() (float64, float64, float64) {
		if comm.Type == store.CommunityGroup { return 50, 40, 10 }
		return 90, 0, 10
	}()
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"community":    comm,
		"revenue_split": gin.H{
			"owner":    ownerPct,
			"members":  memberPct,
			"platform": platformPct,
		},
	})
}

// GET /community/by-chat/:chat_id
func (h *Handler) getCommunityByChatID(c *gin.Context) {
	chatID, _ := strconv.ParseInt(c.Param("chat_id"), 10, 64)
	comm, _ := h.store.FindCommunityByChatID(c.Request.Context(), chatID)
	if comm == nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "community": comm})
}

// POST /community/:id/validation-window
func (h *Handler) setValidationWindow(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	var req struct{ Hours int `json:"hours"` }
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false})
		return
	}
	h.store.UpdateValidationWindow(c.Request.Context(), id, req.Hours*3600)
	c.JSON(http.StatusOK, gin.H{"ok": true, "hours": req.Hours})
}

// GET /owner/:telegram_id/communities
func (h *Handler) listOwnerCommunities(c *gin.Context) {
	telegramID, _ := strconv.ParseInt(c.Param("telegram_id"), 10, 64)
	list, _ := h.store.ListCommunitiesByOwner(c.Request.Context(), telegramID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "communities": list, "count": len(list)})
}

// ── Admin ─────────────────────────────────────────────────

func (h *Handler) listPendingCommunities(c *gin.Context) {
	list, _ := h.store.ListPendingCommunities(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"ok": true, "communities": list, "count": len(list)})
}

func (h *Handler) approveCommunity(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	h.store.UpdateCommunityStatus(c.Request.Context(), id, store.CommunityActive)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) rejectCommunity(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	h.store.UpdateCommunityStatus(c.Request.Context(), id, store.CommunityRejected)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) triggerDistribute(c *gin.Context) {
	var req struct {
		CommunityID string  `json:"community_id"`
		CampaignID  string  `json:"campaign_id"`
		RevenueTON  float64 `json:"revenue_ton"`
		ValidJoins  int     `json:"valid_joins"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false})
		return
	}
	id, _ := uuid.Parse(req.CommunityID)
	go h.engine.DistributeRevenue(c.Request.Context(), id)
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": "queued"})
}

func (h *Handler) authMiddleware(c *gin.Context) {
	if c.GetHeader("X-Admin-Key") != h.adminKey {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false})
		return
	}
	c.Next()
}

// RegisterNATSListeners رویدادهای NATS را subscribe می‌کند.
func (h *Handler) RegisterNATSListeners(ctx context.Context) {
	// membership.joined → attribution
	h.nc.Subscribe("membership.joined", func(data []byte) {
		var e struct {
			TelegramID  int64  `json:"telegram_id"`
			CommunityID string `json:"community_id"`
			CampaignID  string `json:"campaign_id"`
			InviteHash  string `json:"invite_hash"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return
		}
		communityID, err := uuid.Parse(e.CommunityID)
		if err != nil && e.InviteHash != "" {
			community, _ := h.engine.ResolveInviteLink(ctx, e.InviteHash)
			if community != nil {
				communityID = community.ID
			}
		}
		if communityID == uuid.Nil {
			return
		}
		h.engine.HandleJoin(ctx, e.TelegramID, communityID, e.CampaignID)
	})

	// validate response از membership network
	h.nc.Subscribe("membership.validate_response", func(data []byte) {
		var e struct {
			ParticipantID string `json:"participant_id"`
			IsValid       bool   `json:"is_valid"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return
		}
		h.engine.ConfirmValidation(ctx, e.ParticipantID, e.IsValid)
	})

	// activity tracking
	h.nc.Subscribe("community.activity.updated", func(data []byte) {
		var e struct {
			TelegramID  int64 `json:"telegram_id"`
			CommunityID int64 `json:"community_id"`
			Messages    int   `json:"messages"`
			Replies     int   `json:"replies"`
			Reactions   int   `json:"reactions"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return
		}
		h.store.UpdateMemberActivity(ctx, e.TelegramID, e.CommunityID, e.Messages, e.Replies, e.Reactions)
	})

	// توزیع revenue
	h.nc.Subscribe("community.revenue.generated", func(data []byte) {
		var e struct {
			RevenueID string `json:"revenue_id"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return
		}
		revenueID, _ := uuid.Parse(e.RevenueID)
		h.engine.DistributeRevenue(ctx, revenueID)
	})
}
