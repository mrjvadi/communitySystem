package engine

import (
	"context"
	"encoding/json"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/mrjvadi/communitySystem/community-service/internal/store"
	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

// RegisterNATSListeners همه NATS subscriptions را ثبت می‌کند.
func (e *Engine) RegisterNATSListeners(nc *natsclient.Client) {

	// ── membership.joined ─────────────────────────────────
	nc.Subscribe("membership.joined", func(data []byte) {
		var event struct {
			TelegramID int64  `json:"telegram_id"`
			ChatID     int64  `json:"community_id"`
			CampaignID string `json:"campaign_id"`
			InviteLink string `json:"invite_link"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		ctx := context.Background()

		// community رو از chatID پیدا کن
		comm, _ := e.store.FindCommunityByChatID(ctx, event.ChatID)
		if comm == nil {
			return
		}
		// campaignID رو parse کن اگه UUID هست
		var campID uuid.UUID
		var err error
		if event.CampaignID != "" {
			campID, err = uuid.Parse(event.CampaignID)
		}
		if err != nil || event.CampaignID == "" {
			campID = uuid.Nil
		}
		// campID رو به string تبدیل کن
		campStr := ""
		if campID != uuid.Nil {
			campStr = campID.String()
		}
		e.HandleJoin(ctx, event.TelegramID, comm.ID, campStr)
	})

	// ── membership.left ───────────────────────────────────
	nc.Subscribe("membership.left", func(data []byte) {
		var event struct {
			TelegramID int64 `json:"telegram_id"`
			ChatID     int64 `json:"community_id"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		ctx := context.Background()
		comm, _ := e.store.FindCommunityByChatID(ctx, event.ChatID)
		if comm == nil {
			return
		}
		e.HandleLeave(ctx, event.TelegramID, comm.TelegramID)
	})

	// ── activity ──────────────────────────────────────────
	nc.Subscribe("community.activity.updated", func(data []byte) {
		var event struct {
			TelegramID int64 `json:"telegram_id"`
			ChatID     int64 `json:"community_id"`
			Messages   int   `json:"messages"`
			Replies    int   `json:"replies"`
			Reactions  int   `json:"reactions"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		ctx := context.Background()
		comm, _ := e.store.FindCommunityByChatID(ctx, event.ChatID)
		if comm == nil || comm.Type != store.CommunityGroup {
			return
		}
		e.store.UpdateMemberActivity(ctx, event.TelegramID, event.ChatID,
			event.Messages, event.Replies, event.Reactions)
	})

	// ── campaign.revenue.generated ────────────────────────
	nc.Subscribe("campaign.revenue.generated", func(data []byte) {
		var event struct {
			CommunityID string  `json:"community_id"`
			CampaignID  string  `json:"campaign_id"`
			RevenueTON  float64 `json:"revenue_ton"`
			ValidJoins  int     `json:"valid_joins"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		ctx := context.Background()

		if event.RevenueTON <= 0 || math.IsNaN(event.RevenueTON) || math.IsInf(event.RevenueTON, 0) || event.ValidJoins < 0 {
			e.log.Error("campaign.revenue.generated: invalid amount, ignoring", ports.F("revenue_ton", event.RevenueTON))
			return
		}

		// community_id ممکنه UUID یا chat_id عددی باشه
		var comm *store.Community
		if commID, err := uuid.Parse(event.CommunityID); err == nil {
			comm, _ = e.store.FindCommunityByID(ctx, commID)
		} else if chatID, err := strconv.ParseInt(event.CommunityID, 10, 64); err == nil {
			comm, _ = e.store.FindCommunityByChatID(ctx, chatID)
		}
		if comm == nil {
			return
		}

		// CampaignID parse کن
		campUUID, err := uuid.Parse(event.CampaignID)
		if err != nil {
			return
		}

		// idempotency: اگر این جفت (campaign, community) قبلاً پردازش شده،
		// دوباره توزیع نکن — این subject هیچ service-auth ندارد (رجوع به
		// گزارش امنیتی) و هرکس با دسترسی NATS می‌تواند آن را replay کند.
		if existing, ferr := e.store.FindRevenueByCampaignCommunity(ctx, campUUID, comm.ID); ferr == nil && existing != nil {
			e.log.Info("duplicate campaign.revenue.generated skipped",
				ports.F("campaign_id", campUUID), ports.F("community_id", comm.ID))
			return
		}

		// ساخت CommunityRevenue و توزیع
		rev := &store.CommunityRevenue{
			CommunityID: comm.ID,
			CampaignID:  campUUID,
			TotalAmount: event.RevenueTON,
			ValidJoins:  event.ValidJoins,
		}
		if err := e.store.CreateRevenue(ctx, rev); err != nil {
			return
		}
		e.DistributeRevenue(ctx, rev.ID)
	})

	// ── fraud score update ────────────────────────────────
	nc.Subscribe("community.score.updated", func(data []byte) {
		var event struct {
			CommunityID int64 `json:"community_id"`
			Score       int   `json:"score"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		ctx := context.Background()
		e.store.UpdateCommunityScore(ctx, event.CommunityID, event.Score)
	})

	e.log.Info("community-service NATS listeners registered")
}

var _ = ports.F // suppress unused
