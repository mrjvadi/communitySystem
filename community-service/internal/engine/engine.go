package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/community-service/internal/store"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

type Engine struct {
	store *store.Store
	nc    *natsclient.Client
	log   ports.Logger
}

func New(st *store.Store, nc *natsclient.Client, log ports.Logger) *Engine {
	return &Engine{store: st, nc: nc, log: log}
}

// RegisterCommunity ثبت گروه/کانال.
func (e *Engine) RegisterCommunity(ctx context.Context, ownerID uuid.UUID, telegramID int64, commType store.CommunityType, name, username string) (*store.Community, error) {
	existing, _ := e.store.FindCommunityByChatID(ctx, telegramID)
	if existing != nil {
		return nil, fmt.Errorf("community already registered")
	}

	hash := generateHash(telegramID, ownerID)
	c := &store.Community{
		OwnerID:             ownerID,
		TelegramID:          telegramID,
		Type:                commType,
		Name:                name,
		Username:            username,
		Status:              store.CommunityPending,
		InviteHash:          hash,
		InviteLink:          "https://t.me/+" + hash[:16],
		ValidationWindowSec: store.DefaultValidationWindow,
	}
	if err := e.store.CreateCommunity(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// HandleJoin ثبت ورود کاربر از طریق کمپین.
func (e *Engine) HandleJoin(ctx context.Context, telegramID int64, communityID uuid.UUID, campaignID string) error {
	community, _ := e.store.FindCommunityByID(ctx, communityID)
	if community == nil || community.Status != store.CommunityActive {
		return nil
	}

	campID, err := uuid.Parse(campaignID)
	if err != nil { return nil }

	existing, _ := e.store.FindParticipant(ctx, campID, telegramID)
	if existing != nil { return nil }

	p := &store.CampaignParticipant{
		CampaignID:  campID,
		CommunityID: communityID,
		TelegramID:  telegramID,
		JoinedAt:    time.Now(),
		Status:      "pending",
	}
	if err := e.store.RecordParticipant(ctx, p); err != nil {
		return err
	}
	_ = e.store.IncrementMemberCount(ctx, communityID.String())

	window := time.Duration(community.ValidationWindowSec) * time.Second
	go func() {
		timer := time.NewTimer(window)
		defer timer.Stop()
		<-timer.C
		e.nc.PublishCore("membership.validate_check", map[string]any{
			"participant_id": p.ID.String(),
			"community_id":   communityID.String(),
			"campaign_id":    campaignID,
		})
	}()

	return nil
}

// ConfirmValidation عضو validate شد → generate revenue.
func (e *Engine) ConfirmValidation(ctx context.Context, participantIDStr string, isValid bool) error {
	participantID, err := uuid.Parse(participantIDStr)
	if err != nil { return err }

	if !isValid {
		return e.store.InvalidateParticipant(ctx, participantID)
	}

	e.store.ValidateParticipant(ctx, participantID)
	e.nc.PublishCore("membership.validated", map[string]any{
		"participant_id": participantIDStr,
		"validated_at":   time.Now().Unix(),
	})
	return nil
}

// DistributeRevenue توزیع درآمد یک community revenue.
func (e *Engine) DistributeRevenue(ctx context.Context, revenueID uuid.UUID) error {
	rev, _ := e.store.FindRevenue(ctx, revenueID)
	if rev == nil { return fmt.Errorf("revenue not found") }
	if rev.Status != "pending" { return nil }

	community, _ := e.store.FindCommunityByID(ctx, rev.CommunityID)
	if community == nil { return fmt.Errorf("community not found") }

	multiplier := revenueMultiplier(community.QualityScore)
	ownerPct, membersPct, platformPct := community.RevenuePercentages()

	ownerAmt   := rev.TotalAmount * (ownerPct/100) * multiplier
	membersAmt := rev.TotalAmount * (membersPct/100) * multiplier
	platformAmt:= rev.TotalAmount * (platformPct/100) * multiplier

	// owner
	if ownerAmt > 0 {
		e.nc.PublishCore("earning.created", map[string]any{
			"type": "ad_income", "community_id": rev.CommunityID.String(),
			"amount_ton": ownerAmt, "description": "درآمد کمپین",
		})
	}

	// members pool (فقط group)
	if membersAmt > 0 && community.Type == store.CommunityGroup {
		e.distributeMemberRewards(ctx, rev, community, membersAmt)
	}

	// platform
	if platformAmt > 0 {
		e.nc.PublishCore("earning.created", map[string]any{
			"type": "commission", "amount_ton": platformAmt,
		})
	}

	e.store.MarkRevenueDistributed(ctx, revenueID)

	e.nc.PublishCore("community.revenue.distributed", map[string]any{
		"revenue_id": revenueID.String(),
		"community_id": rev.CommunityID.String(),
		"owner_amount": ownerAmt, "members_amount": membersAmt,
		"platform_amount": platformAmt,
	})

	e.log.Info("revenue distributed",
		ports.F("revenue", revenueID),
		ports.F("owner", ownerAmt),
		ports.F("members", membersAmt))

	return nil
}

func (e *Engine) distributeMemberRewards(ctx context.Context, rev *store.CommunityRevenue, community *store.Community, pool float64) {
	since := time.Now().AddDate(0, 0, -30)
	members, _ := e.store.GetActiveMembers(ctx, community.TelegramID, since)
	if len(members) == 0 { return }

	totalScore := 0
	type scored struct{ id int64; s int }
	var list []scored
	for _, m := range members {
		s := store.CalcActivityScore(m)
		if s > 0 {
			list = append(list, scored{m.TelegramID, s})
			totalScore += s
		}
	}
	if totalScore == 0 { return }

	for _, ms := range list {
		share := (float64(ms.s) / float64(totalScore)) * pool
		if share < 0.0001 {
			continue
		}
		// earning.created → revenue-service (همان مسیری که owner و platform استفاده می‌کنند)
		// قبلاً community.reward.created publish می‌شد که هیچ subscriber ای نداشت
		// و اعضا هیچ‌وقت واقعاً پول دریافت نمی‌کردند.
		e.nc.PublishCore("earning.created", map[string]any{
			"type":             "member_reward",
			"owner_telegram_id": ms.id,
			"total_nano":       int64(share * 1e9),
			"ref_id":           rev.ID.String() + "_m_" + fmt.Sprint(ms.id),
			"description":      "سهم اعضا از درآمد کمپین",
		})

		e.store.CreateDistribution(ctx, &store.CommunityDistribution{
			RevenueID: rev.ID, CommunityID: community.ID,
			TelegramID: ms.id, Amount: share,
			ActivityScore: ms.s, Status: "paid",
		})
	}
}

func (e *Engine) ResolveInviteLink(ctx context.Context, hash string) (*store.Community, error) {
	return e.store.FindCommunityByInviteHash(ctx, hash)
}

func (e *Engine) RunValidationWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done(): return
		case <-ticker.C:
			cutoff := time.Now().Add(-time.Duration(store.DefaultValidationWindow) * time.Second)
			pending, _ := e.store.FindPendingValidations(ctx, cutoff)
			for _, p := range pending {
				e.nc.PublishCore("membership.validate_check", map[string]any{
					"participant_id": p.ID.String(),
					"community_id":   p.CommunityID.String(),
					"campaign_id":    p.CampaignID.String(),
				})
			}
		}
	}
}

func generateHash(telegramID int64, ownerID uuid.UUID) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%d", telegramID, ownerID, time.Now().UnixNano())))
	return fmt.Sprintf("%x", h[:16])
}

func revenueMultiplier(score int) float64 {
	switch {
	case score >= 80: return 1.0
	case score >= 50: return 0.8
	case score >= 30: return 0.5
	default:          return 0.0
	}
}

// HandleLeave ترک کاربر از community.
func (e *Engine) HandleLeave(ctx context.Context, telegramID, chatID int64) error {
	e.log.Info("member left",
		ports.F("user", telegramID),
		ports.F("chat", chatID))

	// پیدا کردن community
	comm, err := e.store.FindCommunityByChatID(ctx, chatID)
	if err != nil || comm == nil {
		return nil // community ثبت‌نشده — نادیده بگیر
	}

	if err := e.store.DecrementMemberCount(ctx, comm.ID.String()); err != nil {
		e.log.Error("leave: decrement member count",
			ports.F("chat", chatID), ports.F("err", err))
	}

	e.log.Info("member left processed",
		ports.F("community", comm.ID),
		ports.F("user", telegramID))
	return nil
}
