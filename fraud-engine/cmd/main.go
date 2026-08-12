package main

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/mrjvadi/communitySystem/fraud-engine/internal/api"
	"github.com/mrjvadi/communitySystem/fraud-engine/internal/processor"
	"github.com/mrjvadi/communitySystem/fraud-engine/internal/scorer"
	"github.com/mrjvadi/communitySystem/fraud-engine/internal/store"
	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/shared/pkg/config"
	"github.com/mrjvadi/communitySystem/shared/pkg/logger"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

type Config struct {
	MongoURI string `mapstructure:"MONGO_URI"`
	MongoDB  string `mapstructure:"MONGO_DB"`
	NatsURL  string `mapstructure:"NATS_URL"`
	NatsUser string `mapstructure:"NATS_USERNAME"`
	NatsPass string `mapstructure:"NATS_PASSWORD"`
	Port     int    `mapstructure:"PORT"`
	AdminKey string `mapstructure:"ADMIN_KEY"`
}

func main() {
	var cfg Config
	config.MustLoad(&cfg)
	log := logger.MustNew(false)

	if cfg.Port == 0 {
		cfg.Port = 8092
	}
	if cfg.MongoDB == "" {
		cfg.MongoDB = "creatorbot"
	}
	// fail-closed: بدون ADMIN_KEY مسیرهای /admin احراز هویت نمی‌شوند (fail-open) —
	// سرویس نباید با auth شکسته بالا بیاید.
	if cfg.AdminKey == "" {
		log.Fatal("ADMIN_KEY تنظیم نشده — مسیرهای admin بدون آن ناامن‌اند")
	}

	// ── MongoDB ───────────────────────────────────────────
	ctx := context.Background()
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatal("mongodb", ports.F("err", err))
	}
	if err := mongoClient.Ping(ctx, nil); err != nil {
		log.Fatal("mongodb ping", ports.F("err", err))
	}
	defer mongoClient.Disconnect(ctx)

	st := store.New(mongoClient)
	if err := st.EnsureIndexes(ctx); err != nil {
		log.Fatal("indexes", ports.F("err", err))
	}
	log.Info("mongodb connected")

	// ── NATS ──────────────────────────────────────────────
	nc, err := natsclient.New(natsclient.Config{
		URL:      cfg.NatsURL,
		Username: cfg.NatsUser,
		Password: cfg.NatsPass,
		Name:     "fraud-engine",
	})
	if err != nil {
		log.Fatal("nats", ports.F("err", err))
	}
	defer nc.Close()
	log.AttachNATS(nc, "fraud-engine")

	// ── Scorers ───────────────────────────────────────────
	userScorer := scorer.NewUserScorer(st, log)
	communityScorer := scorer.NewCommunityScorer(st, log)

	// ── Processor ─────────────────────────────────────────
	proc := processor.New(st, nc, userScorer, communityScorer, log)
	proc.RegisterListeners()
	proc.RegisterScoreHandlers() // request/reply برای سرویس‌های دیگر

	// ── API ───────────────────────────────────────────────
	apiHandler := api.New(st, userScorer, communityScorer, cfg.AdminKey)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	apiHandler.Register(r)

	// ── Shutdown ──────────────────────────────────────────
	shutCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go proc.RunPeriodicRecalc(shutCtx)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Info("fraud-engine started",
			ports.F("addr", addr),
			ports.F("mongo", cfg.MongoDB))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http", ports.F("err", err))
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic recovered", ports.F("panic", r))
		}
	}()

	<-shutCtx.Done()
	log.Info("shutting down...")
	timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(timeout)
}
