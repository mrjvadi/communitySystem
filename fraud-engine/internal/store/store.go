package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const dbName = "fraud_engine"

type Store struct {
	db *mongo.Database
}

func New(client *mongo.Client) *Store {
	db := client.Database(dbName)
	return &Store{db: db}
}

func (s *Store) col(name string) *mongo.Collection {
	return s.db.Collection(name)
}

// EnsureIndexes ایجاد index های ضروری.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	indexes := map[string][][2]string{
		"user_memberships": {
			{"telegram_id", "community_id"},
			{"telegram_id", "joined_at"},
		},
		"user_activity": {
			{"telegram_id", "community_id"},
			{"telegram_id", "date"},
		},
		"user_scores": {
			{"telegram_id", "calculated_at"},
		},
		"community_scores": {
			{"community_id", "calculated_at"},
		},
		"fraud_events": {
			{"telegram_id", "detected_at"},
			{"community_id", "detected_at"},
		},
		"user_profile_history": {
			{"telegram_id", "changed_at"},
		},
	}

	for col, idxList := range indexes {
		for _, fields := range idxList {
			keys := bson.D{}
			for _, f := range fields {
				if f != "" {
					keys = append(keys, bson.E{Key: f, Value: 1})
				}
			}
			s.col(col).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys})
		}
	}
	return nil
}

// ── User Profile ───────────────────────────────────────────

func (s *Store) UpsertProfile(ctx context.Context, p *UserProfile) error {
	p.UpdatedAt = time.Now()
	if p.FirstSeen.IsZero() {
		p.FirstSeen = time.Now()
	}
	p.LastSeen = time.Now()

	_, err := s.col("user_profiles").UpdateOne(ctx,
		bson.M{"_id": p.TelegramID},
		bson.M{"$set": p, "$setOnInsert": bson.M{"first_seen": p.FirstSeen}},
		options.Update().SetUpsert(true),
	)
	return err
}

func (s *Store) GetProfile(ctx context.Context, telegramID int64) (*UserProfile, error) {
	var p UserProfile
	err := s.col("user_profiles").FindOne(ctx, bson.M{"_id": telegramID}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &p, err
}

func (s *Store) UpdateTrustScore(ctx context.Context, telegramID int64, score int, breakdown ScoreBreakdown) error {
	_, err := s.col("user_profiles").UpdateOne(ctx,
		bson.M{"_id": telegramID},
		bson.M{"$set": bson.M{
			"trust_score": score,
			"score_label": UserScoreLabel(score),
			"updated_at":  time.Now(),
		}},
	)
	if err != nil {
		return err
	}
	// snapshot
	snap := &UserScoreSnapshot{
		TelegramID:   telegramID,
		Score:        score,
		Breakdown:    breakdown,
		CalculatedAt: time.Now(),
	}
	_, err = s.col("user_scores").InsertOne(ctx, snap)
	return err
}

func (s *Store) RecordProfileChange(ctx context.Context, h *UserProfileHistory) error {
	h.ChangedAt = time.Now()
	_, err := s.col("user_profile_history").InsertOne(ctx, h)
	return err
}

// ── Membership ─────────────────────────────────────────────

func (s *Store) RecordJoin(ctx context.Context, m *UserMembership) error {
	m.JoinedAt = time.Now()
	m.DurationSec = -1

	// بررسی rejoin
	count, _ := s.col("user_memberships").CountDocuments(ctx, bson.M{
		"telegram_id":  m.TelegramID,
		"community_id": m.CommunityID,
	})
	m.RejoinCount = int(count)

	// آپدیت شمارنده
	s.col("user_profiles").UpdateOne(ctx,
		bson.M{"_id": m.TelegramID},
		bson.M{"$inc": bson.M{"total_joins": 1}},
	)

	// آپدیت stats کانال
	s.col("community_statistics").UpdateOne(ctx,
		bson.M{"_id": m.CommunityID},
		bson.M{
			"$inc": bson.M{"total_joins": 1, "member_count": 1},
			"$set": bson.M{"updated_at": time.Now()},
		},
		options.Update().SetUpsert(true),
	)

	_, err := s.col("user_memberships").InsertOne(ctx, m)
	return err
}

func (s *Store) RecordLeave(ctx context.Context, telegramID, communityID int64) error {
	now := time.Now()

	// پیدا کردن آخرین join
	var m UserMembership
	err := s.col("user_memberships").FindOne(ctx,
		bson.M{
			"telegram_id":  telegramID,
			"community_id": communityID,
			"left_at":      nil,
		},
		options.FindOne().SetSort(bson.M{"joined_at": -1}),
	).Decode(&m)

	if err == nil {
		duration := int64(now.Sub(m.JoinedAt).Seconds())
		s.col("user_memberships").UpdateOne(ctx,
			bson.M{
				"telegram_id":  telegramID,
				"community_id": communityID,
				"left_at":      nil,
			},
			bson.M{"$set": bson.M{
				"left_at":      &now,
				"duration_sec": duration,
			}},
		)
	}

	// آپدیت شمارنده‌ها
	s.col("user_profiles").UpdateOne(ctx,
		bson.M{"_id": telegramID},
		bson.M{"$inc": bson.M{"total_leaves": 1}},
	)
	s.col("community_statistics").UpdateOne(ctx,
		bson.M{"_id": communityID},
		bson.M{
			"$inc": bson.M{"total_leaves": 1, "member_count": -1},
			"$set": bson.M{"updated_at": now},
		},
	)
	return nil
}

func (s *Store) GetRecentMemberships(ctx context.Context, telegramID int64, days int) ([]UserMembership, error) {
	since := time.Now().AddDate(0, 0, -days)
	cur, err := s.col("user_memberships").Find(ctx,
		bson.M{"telegram_id": telegramID, "joined_at": bson.M{"$gte": since}},
	)
	if err != nil {
		return nil, err
	}
	var list []UserMembership
	cur.All(ctx, &list)
	return list, nil
}

func (s *Store) GetMembershipHistory(ctx context.Context, telegramID, communityID int64) ([]UserMembership, error) {
	cur, err := s.col("user_memberships").Find(ctx,
		bson.M{"telegram_id": telegramID, "community_id": communityID},
		options.Find().SetSort(bson.M{"joined_at": -1}),
	)
	if err != nil {
		return nil, err
	}
	var list []UserMembership
	cur.All(ctx, &list)
	return list, nil
}

// ── Activity ───────────────────────────────────────────────

func (s *Store) RecordActivity(ctx context.Context, telegramID, communityID int64, msgCount, replies, reactions int) error {
	date := time.Now().Format("2006-01-02")
	_, err := s.col("user_activity").UpdateOne(ctx,
		bson.M{"telegram_id": telegramID, "community_id": communityID, "date": date},
		bson.M{
			"$inc": bson.M{
				"messages":  msgCount,
				"replies":   replies,
				"reactions": reactions,
			},
			"$set": bson.M{"updated_at": time.Now()},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func (s *Store) GetActivityDays(ctx context.Context, telegramID int64, days int) (int, error) {
	since := time.Now().AddDate(0, 0, -days)
	count, err := s.col("user_activity").CountDocuments(ctx,
		bson.M{"telegram_id": telegramID, "updated_at": bson.M{"$gte": since}},
	)
	return int(count), err
}

// ── Community Score ────────────────────────────────────────

func (s *Store) UpdateCommunityScore(ctx context.Context, communityID int64, score int, breakdown CommunityBreakdown) error {
	revenueStatus := CommunityRevenueStatus(score)
	snap := &CommunityScoreSnapshot{
		CommunityID:   communityID,
		Score:         score,
		Breakdown:     breakdown,
		CalculatedAt:  time.Now(),
		RevenueStatus: revenueStatus,
	}
	_, err := s.col("community_scores").InsertOne(ctx, snap)
	return err
}

func (s *Store) GetLatestCommunityScore(ctx context.Context, communityID int64) (*CommunityScoreSnapshot, error) {
	var snap CommunityScoreSnapshot
	err := s.col("community_scores").FindOne(ctx,
		bson.M{"community_id": communityID},
		options.FindOne().SetSort(bson.M{"calculated_at": -1}),
	).Decode(&snap)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &snap, err
}

func (s *Store) GetCommunityStats(ctx context.Context, communityID int64) (*CommunityStatistics, error) {
	var stats CommunityStatistics
	err := s.col("community_statistics").FindOne(ctx, bson.M{"_id": communityID}).Decode(&stats)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &stats, err
}

func (s *Store) GetAvgTrustScoreForCommunity(ctx context.Context, communityID int64) (float64, error) {
	// میانگین trust score کاربرانی که عضو این کانال هستند
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"community_id": communityID, "left_at": nil}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "user_profiles",
			"localField":   "telegram_id",
			"foreignField": "_id",
			"as":           "profile",
		}}},
		{{Key: "$unwind", Value: "$profile"}},
		{{Key: "$group", Value: bson.M{
			"_id": nil,
			"avg": bson.M{"$avg": "$profile.trust_score"},
		}}},
	}
	cur, err := s.col("user_memberships").Aggregate(ctx, pipeline)
	if err != nil {
		return 50, err
	}
	var result []struct{ Avg float64 `bson:"avg"` }
	cur.All(ctx, &result)
	if len(result) == 0 {
		return 50, nil
	}
	return result[0].Avg, nil
}

// ── Fraud Events ───────────────────────────────────────────

func (s *Store) RecordFraudEvent(ctx context.Context, e *FraudEvent) error {
	e.DetectedAt = time.Now()
	_, err := s.col("fraud_events").InsertOne(ctx, e)
	return err
}

func (s *Store) GetFraudEvents(ctx context.Context, telegramID int64, limit int) ([]FraudEvent, error) {
	cur, err := s.col("fraud_events").Find(ctx,
		bson.M{"telegram_id": telegramID},
		options.Find().SetSort(bson.M{"detected_at": -1}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	var events []FraudEvent
	cur.All(ctx, &events)
	return events, nil
}

// ListUsersForRecalc لیست TelegramID هایی که باید recalc شوند.
// cursor-based pagination با lastID.
func (s *Store) ListUsersForRecalc(ctx context.Context, afterID int64, limit int) ([]int64, error) {
	cur, err := s.col("user_profiles").Find(ctx,
		bson.M{"_id": bson.M{"$gt": afterID}},
		options.Find().SetSort(bson.M{"_id": 1}).SetLimit(int64(limit)).
			SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return nil, err
	}
	var results []struct{ ID int64 `bson:"_id"` }
	cur.All(ctx, &results)
	ids := make([]int64, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids, nil
}
