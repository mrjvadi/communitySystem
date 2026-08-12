package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/gorm"
)

type Store struct {
	pg    *gorm.DB
	mongo *mongo.Database
}

func New(pg *gorm.DB, mdb *mongo.Database) *Store {
	return &Store{pg: pg, mongo: mdb}
}

// ── Community ─────────────────────────────────────────────

func (s *Store) CreateCommunity(ctx context.Context, c *Community) error {
	return s.pg.WithContext(ctx).Create(c).Error
}

func (s *Store) FindCommunityByChatID(ctx context.Context, telegramID int64) (*Community, error) {
	var c Community
	err := s.pg.WithContext(ctx).Where("chat_id = ?", telegramID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

func (s *Store) FindCommunityByInviteHash(ctx context.Context, hash string) (*Community, error) {
	var c Community
	err := s.pg.WithContext(ctx).Where("invite_hash = ? AND status = 'active'", hash).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

func (s *Store) FindCommunityByID(ctx context.Context, id uuid.UUID) (*Community, error) {
	var c Community
	err := s.pg.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

func (s *Store) UpdateCommunity(ctx context.Context, c *Community) error {
	return s.pg.WithContext(ctx).Save(c).Error
}

func (s *Store) ActivateCommunity(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return s.pg.WithContext(ctx).Model(&Community{}).Where("id = ?", id).Updates(map[string]any{
		"status": CommunityActive, "verified_at": &now,
	}).Error
}

func (s *Store) ListCommunities(ctx context.Context, ownerID uuid.UUID) ([]Community, error) {
	var list []Community
	return list, s.pg.WithContext(ctx).Where("owner_id = ?", ownerID).Find(&list).Error
}

func (s *Store) UpdateQualityScore(ctx context.Context, id uuid.UUID, score int) error {
	return s.pg.WithContext(ctx).Model(&Community{}).Where("id = ?", id).
		Update("quality_score", score).Error
}

// ── CampaignParticipant ───────────────────────────────────

func (s *Store) RecordParticipant(ctx context.Context, p *CampaignParticipant) error {
	return s.pg.WithContext(ctx).Create(p).Error
}

func (s *Store) FindParticipant(ctx context.Context, campaignID uuid.UUID, telegramID int64) (*CampaignParticipant, error) {
	var p CampaignParticipant
	err := s.pg.WithContext(ctx).
		Where("campaign_id = ? AND telegram_id = ?", campaignID, telegramID).
		First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

func (s *Store) ValidateParticipant(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return s.pg.WithContext(ctx).Model(&CampaignParticipant{}).Where("id = ?", id).Updates(map[string]any{
		"status": "validated", "validated_at": &now,
	}).Error
}

func (s *Store) InvalidateParticipant(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return s.pg.WithContext(ctx).Model(&CampaignParticipant{}).Where("id = ?", id).Updates(map[string]any{
		"status": "left", "left_at": &now,
	}).Error
}

func (s *Store) FindPendingValidations(ctx context.Context, before time.Time) ([]CampaignParticipant, error) {
	var list []CampaignParticipant
	return list, s.pg.WithContext(ctx).
		Where("status = 'pending' AND joined_at <= ?", before).
		Find(&list).Error
}

// ── CommunityRevenue ──────────────────────────────────────

func (s *Store) CreateRevenue(ctx context.Context, r *CommunityRevenue) error {
	return s.pg.WithContext(ctx).Create(r).Error
}

func (s *Store) FindRevenue(ctx context.Context, id uuid.UUID) (*CommunityRevenue, error) {
	var r CommunityRevenue
	err := s.pg.WithContext(ctx).Where("id = ?", id).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

// FindRevenueByCampaignCommunity یک CommunityRevenue موجود برای همین جفت
// (campaign, community) را برمی‌گرداند — برای idempotency روی رویداد NATS
// campaign.revenue.generated، چون این subject هیچ auth ندارد و هر کلاینتی
// که به NATS دسترسی دارد می‌تواند آن را replay/spoof کند (رجوع به گزارش
// امنیتی). بدون این چک، هر replay یک توزیع درآمد جدید و واقعی می‌ساخت.
func (s *Store) FindRevenueByCampaignCommunity(ctx context.Context, campaignID, communityID uuid.UUID) (*CommunityRevenue, error) {
	var r CommunityRevenue
	err := s.pg.WithContext(ctx).
		Where("campaign_id = ? AND community_id = ?", campaignID, communityID).
		First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

func (s *Store) MarkRevenueDistributed(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return s.pg.WithContext(ctx).Model(&CommunityRevenue{}).Where("id = ?", id).Updates(map[string]any{
		"status": "distributed", "distributed_at": &now,
	}).Error
}

func (s *Store) CreateDistribution(ctx context.Context, d *CommunityDistribution) error {
	return s.pg.WithContext(ctx).Create(d).Error
}

// ── Member Activity (MongoDB) ─────────────────────────────

type MemberActivity struct {
	TelegramID    int64     `bson:"telegram_id"`
	CommunityID   int64     `bson:"community_id"`
	Messages      int       `bson:"messages"`
	Replies       int       `bson:"replies"`
	Reactions     int       `bson:"reactions"`
	ActiveDays    int       `bson:"active_days"`
	ActivityScore int       `bson:"activity_score"`
	UpdatedAt     time.Time `bson:"updated_at"`
}

func (s *Store) UpdateMemberActivity(ctx context.Context, telegramID, communityID int64, msgs, replies, reactions int) error {
	if s.mongo == nil {
		return nil
	}
	_, err := s.mongo.Collection("community_activity").UpdateOne(ctx,
		bson.M{"telegram_id": telegramID, "community_id": communityID},
		bson.M{
			"$inc": bson.M{"messages": msgs, "replies": replies, "reactions": reactions},
			"$set": bson.M{"updated_at": time.Now()},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func (s *Store) GetActiveMembers(ctx context.Context, communityID int64, since time.Time) ([]MemberActivity, error) {
	if s.mongo == nil {
		return nil, nil
	}
	cur, err := s.mongo.Collection("community_activity").Find(ctx,
		bson.M{"community_id": communityID, "updated_at": bson.M{"$gte": since}},
		options.Find().SetSort(bson.M{"activity_score": -1}),
	)
	if err != nil {
		return nil, err
	}
	var list []MemberActivity
	cur.All(ctx, &list)
	return list, nil
}

// CalcActivityScore امتیاز فعالیت عضو.
func CalcActivityScore(m MemberActivity) int {
	score := 0
	score += min(m.Messages*2, 40)
	score += min(m.Replies*3, 30)
	score += min(m.Reactions, 15)
	score += min(m.ActiveDays*5, 15)
	if score > 100 {
		score = 100
	}
	return score
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Store) UpdateCommunityScore(ctx context.Context, chatID int64, score int) error {
	return s.pg.WithContext(ctx).Model(&Community{}).
		Where("chat_id = ?", chatID).Update("quality_score", score).Error
}

func (s *Store) UpdateCommunityStatus(ctx context.Context, id uuid.UUID, status CommunityStatus) error {
	return s.pg.WithContext(ctx).Model(&Community{}).
		Where("id = ?", id).Update("status", status).Error
}

func (s *Store) ListCommunitiesByOwner(ctx context.Context, ownerID int64) ([]Community, error) {
	var list []Community
	return list, s.pg.WithContext(ctx).
		Where("owner_telegram_id = ?", ownerID).Find(&list).Error
}

func (s *Store) ListPendingCommunities(ctx context.Context) ([]Community, error) {
	var list []Community
	return list, s.pg.WithContext(ctx).Where("status = ?", CommunityPending).Find(&list).Error
}

func (s *Store) UpdateValidationWindow(ctx context.Context, id uuid.UUID, windowSec int) error {
	return s.pg.WithContext(ctx).Model(&Community{}).
		Where("id = ?", id).Update("validation_window_sec", windowSec).Error
}

// DecrementMemberCount تعداد اعضا را یک واحد کاهش می‌دهد.
func (s *Store) DecrementMemberCount(ctx context.Context, communityID string) error {
	return s.pg.WithContext(ctx).
		Model(&Community{}).
		Where("id = ?", communityID).
		UpdateColumn("member_count", gorm.Expr("GREATEST(member_count - 1, 0)")).Error
}

// IncrementMemberCount تعداد اعضا را یک واحد افزایش می‌دهد.
func (s *Store) IncrementMemberCount(ctx context.Context, communityID string) error {
	return s.pg.WithContext(ctx).
		Model(&Community{}).
		Where("id = ?", communityID).
		UpdateColumn("member_count", gorm.Expr("member_count + 1")).Error
}
