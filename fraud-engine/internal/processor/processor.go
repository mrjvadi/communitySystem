// Package processor رویدادهای NATS را دریافت و امتیازها را آپدیت می‌کند.
package processor

import (
	"context"

	"github.com/nats-io/nats.go"
	"encoding/json"
	"time"

	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
	"github.com/mrjvadi/communitySystem/fraud-engine/internal/scorer"
	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
)

// Processor پردازش رویدادهای ورودی.
type Processor struct {
	store            *store.Store
	nc               *natsclient.Client
	userScorer       *scorer.UserScorer
	communityScorer  *scorer.CommunityScorer
	log              ports.Logger
}

func New(
	st *store.Store,
	nc *natsclient.Client,
	us *scorer.UserScorer,
	cs *scorer.CommunityScorer,
	log ports.Logger,
) *Processor {
	return &Processor{
		store:           st,
		nc:              nc,
		userScorer:      us,
		communityScorer: cs,
		log:             log,
	}
}

// RegisterListeners همه NATS handler ها را ثبت می‌کند.
func (p *Processor) RegisterListeners() {
	// ── رویدادهای ورودی ─────────────────────────────────────

	p.nc.Subscribe("membership.joined", func(data []byte) {
		var e struct {
			TelegramID  int64  `json:"telegram_id"`
			CommunityID int64  `json:"community_id"`
			Source      string `json:"source"`
			CampaignID  string `json:"campaign_id"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleJoin(e.TelegramID, e.CommunityID, e.Source, e.CampaignID)
	})

	p.nc.Subscribe("membership.left", func(data []byte) {
		var e struct {
			TelegramID  int64 `json:"telegram_id"`
			CommunityID int64 `json:"community_id"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleLeave(e.TelegramID, e.CommunityID)
	})

	p.nc.Subscribe("community.activity.updated", func(data []byte) {
		var e struct {
			TelegramID  int64 `json:"telegram_id"`
			CommunityID int64 `json:"community_id"`
			Messages    int   `json:"messages"`
			Replies     int   `json:"replies"`
			Reactions   int   `json:"reactions"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		ctx := context.Background()
		p.store.RecordActivity(ctx, e.TelegramID, e.CommunityID, e.Messages, e.Replies, e.Reactions)
		p.recalcUserScore(ctx, e.TelegramID)
	})

	p.nc.Subscribe("profile.updated", func(data []byte) {
		var e struct {
			TelegramID int64  `json:"telegram_id"`
			Username   string `json:"username"`
			FirstName  string `json:"first_name"`
			LastName   string `json:"last_name"`
			HasPhoto   bool   `json:"has_photo"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleProfileUpdate(e.TelegramID, e.Username, e.FirstName, e.LastName, e.HasPhoto)
	})

	p.nc.Subscribe("campaign.completed", func(data []byte) {
		var e struct {
			TelegramID int64  `json:"telegram_id"`
			CampaignID string `json:"campaign_id"`
			Stayed     bool   `json:"stayed"` // آیا بعد از تبلیغ ماند
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleCampaignComplete(e.TelegramID, e.Stayed)
	})

	p.log.Info("fraud-engine listeners registered")
}

// ── handlers ──────────────────────────────────────────────

func (p *Processor) handleJoin(telegramID, communityID int64, source, campaignID string) {
	ctx := context.Background()

	m := &store.UserMembership{
		TelegramID:  telegramID,
		CommunityID: communityID,
		Source:      source,
		CampaignID:  campaignID,
	}
	p.store.RecordJoin(ctx, m)

	// بررسی fraud — خیلی سریع join می‌کند؟
	p.detectJoinLoopFraud(ctx, telegramID, communityID)

	// recalc امتیازها
	p.recalcUserScore(ctx, telegramID)
	p.recalcCommunityScore(ctx, communityID)
}

func (p *Processor) handleLeave(telegramID, communityID int64) {
	ctx := context.Background()
	p.store.RecordLeave(ctx, telegramID, communityID)
	p.recalcUserScore(ctx, telegramID)
	p.recalcCommunityScore(ctx, communityID)
}

func (p *Processor) handleProfileUpdate(telegramID int64, username, firstName, lastName string, hasPhoto bool) {
	ctx := context.Background()

	existing, _ := p.store.GetProfile(ctx, telegramID)

	// ثبت تغییرات
	if existing != nil {
		if existing.Username != username {
			p.store.RecordProfileChange(ctx, &store.UserProfileHistory{
				TelegramID: telegramID,
				Field:      "username",
				OldValue:   existing.Username,
				NewValue:   username,
			})
		}
		if existing.FirstName != firstName {
			p.store.RecordProfileChange(ctx, &store.UserProfileHistory{
				TelegramID: telegramID,
				Field:      "first_name",
				OldValue:   existing.FirstName,
				NewValue:   firstName,
			})
		}
	}

	profile := &store.UserProfile{
		TelegramID: telegramID,
		Username:   username,
		FirstName:  firstName,
		LastName:   lastName,
		HasPhoto:   hasPhoto,
	}
	p.store.UpsertProfile(ctx, profile)
	p.recalcUserScore(ctx, telegramID)
}

func (p *Processor) handleCampaignComplete(telegramID int64, stayed bool) {
	ctx := context.Background()
	profile, _ := p.store.GetProfile(ctx, telegramID)
	if profile == nil {
		return
	}

	update := &store.UserProfile{TelegramID: telegramID}
	update.TotalCampaigns = profile.TotalCampaigns + 1
	if stayed {
		update.AdCompletions = profile.AdCompletions + 1
	}
	p.store.UpsertProfile(ctx, update)
	p.recalcUserScore(ctx, telegramID)
}

// ── recalc ────────────────────────────────────────────────

func (p *Processor) recalcUserScore(ctx context.Context, telegramID int64) {
	score, breakdown, err := p.userScorer.Calculate(ctx, telegramID)
	if err != nil {
		p.log.Error("user score calc", ports.F("err", err))
		return
	}

	p.store.UpdateTrustScore(ctx, telegramID, score, breakdown)

	// publish
	p.nc.PublishCore("user.score.updated", map[string]any{
		"telegram_id": telegramID,
		"score":       score,
		"label":       store.UserScoreLabel(score),
		"breakdown":   breakdown,
		"updated_at":  time.Now().Unix(),
	})

	p.log.Info("user score updated",
		ports.F("user", telegramID),
		ports.F("score", score),
		ports.F("label", store.UserScoreLabel(score)))
}

func (p *Processor) recalcCommunityScore(ctx context.Context, communityID int64) {
	score, breakdown, err := p.communityScorer.Calculate(ctx, communityID)
	if err != nil {
		return
	}

	p.store.UpdateCommunityScore(ctx, communityID, score, breakdown)

	revenueMultiplier := scorer.RevenueMultiplier(score)

	p.nc.PublishCore("community.score.updated", map[string]any{
		"community_id":       communityID,
		"score":              score,
		"revenue_status":     store.CommunityRevenueStatus(score),
		"revenue_multiplier": revenueMultiplier,
		"updated_at":         time.Now().Unix(),
	})
}

// ── fraud detection ────────────────────────────────────────

func (p *Processor) detectJoinLoopFraud(ctx context.Context, telegramID, communityID int64) {
	// بررسی join/leave سریع به همین کانال
	history, _ := p.store.GetMembershipHistory(ctx, telegramID, communityID)
	if len(history) < 3 {
		return
	}

	// اگه ۳+ بار در ۷ روز join/leave کرده → مشکوک
	recentJoins := 0
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, m := range history {
		if m.JoinedAt.After(cutoff) {
			recentJoins++
		}
	}

	if recentJoins >= 3 {
		p.store.RecordFraudEvent(ctx, &store.FraudEvent{
			EventType:   store.FraudJoinLeaveLoop,
			TelegramID:  &telegramID,
			CommunityID: &communityID,
			Score:       70,
			Description: "کاربر ۳+ بار در ۷ روز به این کانال join/leave کرده",
			Evidence: map[string]any{
				"recent_joins": recentJoins,
				"days":         7,
			},
		})

		p.nc.PublishCore("fraud.detected", map[string]any{
			"type":         "join_leave_loop",
			"telegram_id":  telegramID,
			"community_id": communityID,
			"score":        70,
		})
	}
}

// RunPeriodicRecalc همه امتیازها را به صورت دوره‌ای recalc می‌کند.
func (p *Processor) RunPeriodicRecalc(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	p.log.Info("periodic recalc started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.batchRecalcUsers(ctx)
		}
	}
}

// batchRecalcUsers همه user هایی که ۶+ ساعت پیش آپدیت نشدند را recalc می‌کند.
func (p *Processor) batchRecalcUsers(ctx context.Context) {
	p.log.Info("starting batch recalc")

	// cursor pagination روی user_profiles
	var processed int
	var lastID int64 = 0
	batchSize := 100

	for {
		// بگیر N تا user که lastID < آن‌ها
		users, err := p.store.ListUsersForRecalc(ctx, lastID, batchSize)
		if err != nil || len(users) == 0 {
			break
		}

		for _, uid := range users {
			select {
			case <-ctx.Done():
				return
			default:
			}
			p.recalcUserScore(ctx, uid)
			lastID = uid
			processed++
		}

		if len(users) < batchSize {
			break
		}
	}

	p.log.Info("batch recalc done", ports.F("users", processed))
}

// ── NATS Request/Reply ────────────────────────────────────

// RegisterScoreHandlers handler هایی که به score request ها جواب می‌دهند.
func (p *Processor) RegisterScoreHandlers() {
	// ── user score request ────────────────────────────────
	p.nc.SubscribeRaw("fraud.user.score.request", func(msg *nats.Msg) {
		var req struct {
			TelegramID int64 `json:"telegram_id"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return
		}

		ctx := context.Background()
		profile, _ := p.store.GetProfile(ctx, req.TelegramID)

		score := 60
		label := "normal"
		known := false

		if profile != nil {
			score = profile.TrustScore
			label = profile.ScoreLabel
			known = true
		}

		resp, _ := json.Marshal(map[string]any{
			"telegram_id": req.TelegramID,
			"score":       score,
			"label":       label,
			"known":       known,
		})
		msg.Respond(resp)
	})

	// ── community score request ───────────────────────────
	p.nc.SubscribeRaw("fraud.community.score.request", func(msg *nats.Msg) {
		var req struct {
			CommunityID int64 `json:"community_id"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return
		}

		ctx := context.Background()
		snap, _ := p.store.GetLatestCommunityScore(ctx, req.CommunityID)

		score := 70
		revenueStatus := "monitored"
		revenueMultiplier := 0.8
		known := false

		if snap != nil {
			score = snap.Score
			revenueStatus = snap.RevenueStatus
			revenueMultiplier = revenueMultiplierFromScore(score)
			known = true
		}

		resp, _ := json.Marshal(map[string]any{
			"community_id":       req.CommunityID,
			"score":              score,
			"revenue_status":     revenueStatus,
			"revenue_multiplier": revenueMultiplier,
			"known":              known,
		})
		msg.Respond(resp)
	})

	// ── event handlers ────────────────────────────────────
	p.nc.Subscribe("fraud.event.join", func(data []byte) {
		var e struct {
			TelegramID  int64  `json:"telegram_id"`
			CommunityID int64  `json:"community_id"`
			Source      string `json:"source"`
			CampaignID  string `json:"campaign_id"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleJoin(e.TelegramID, e.CommunityID, e.Source, e.CampaignID)
	})

	p.nc.Subscribe("fraud.event.leave", func(data []byte) {
		var e struct {
			TelegramID  int64 `json:"telegram_id"`
			CommunityID int64 `json:"community_id"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleLeave(e.TelegramID, e.CommunityID)
	})

	p.nc.Subscribe("fraud.event.activity", func(data []byte) {
		var e struct {
			TelegramID  int64 `json:"telegram_id"`
			CommunityID int64 `json:"community_id"`
			Messages    int   `json:"messages"`
			Replies     int   `json:"replies"`
			Reactions   int   `json:"reactions"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		ctx := context.Background()
		p.store.RecordActivity(ctx, e.TelegramID, e.CommunityID, e.Messages, e.Replies, e.Reactions)
		p.recalcUserScore(ctx, e.TelegramID)
	})

	p.nc.Subscribe("fraud.event.profile", func(data []byte) {
		var e struct {
			TelegramID int64  `json:"telegram_id"`
			Username   string `json:"username"`
			FirstName  string `json:"first_name"`
			HasPhoto   bool   `json:"has_photo"`
		}
		if err := json.Unmarshal(data, &e); err != nil { return }
		p.handleProfileUpdate(e.TelegramID, e.Username, e.FirstName, "", e.HasPhoto)
	})

	p.log.Info("fraud-engine score handlers registered")
}

func revenueMultiplierFromScore(score int) float64 {
	switch {
	case score >= 80: return 1.0
	case score >= 50: return 0.8
	case score >= 30: return 0.5
	default:          return 0.0
	}
}
