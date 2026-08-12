package main

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/mrjvadi/communitySystem/community-service/internal/api"
	"github.com/mrjvadi/communitySystem/community-service/internal/engine"
	"github.com/mrjvadi/communitySystem/community-service/internal/store"
	natsclient "github.com/mrjvadi/communitySystem/shared/pkg/adapters/nats"
	"github.com/mrjvadi/communitySystem/shared/pkg/config"
	"github.com/mrjvadi/communitySystem/shared/pkg/logger"
	"github.com/mrjvadi/communitySystem/shared/pkg/ports"
)

type Config struct {
	PostgresDSN string `mapstructure:"POSTGRES_DSN"`
	NatsURL     string `mapstructure:"NATS_URL"`
	NatsUser    string `mapstructure:"NATS_USERNAME"`
	NatsPass    string `mapstructure:"NATS_PASSWORD"`
	Port        int    `mapstructure:"PORT"`
	AdminKey    string `mapstructure:"ADMIN_KEY"`
}

func main() {
	var cfg Config
	config.MustLoad(&cfg)
	log := logger.MustNew(false)

	if cfg.Port == 0 {
		cfg.Port = 8093
	}

	// ── PostgreSQL ─────────────────────────────────────────
	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN))
	if err != nil {
		log.Fatal("postgres", ports.F("err", err))
	}
	if err := store.AutoMigrate(db); err != nil {
		log.Fatal("migrate", ports.F("err", err))
	}
	// community-service فعلاً فقط PostgreSQL دارد
	st := store.New(db, nil)

	// ── NATS ──────────────────────────────────────────────
	nc, err := natsclient.New(natsclient.Config{
		URL:      cfg.NatsURL,
		Username: cfg.NatsUser,
		Password: cfg.NatsPass,
		Name:     "community-service",
	})
	if err != nil {
		log.Fatal("nats", ports.F("err", err))
	}
	defer nc.Close()
	log.AttachNATS(nc, "community-service")

	// ── Engine ────────────────────────────────────────────
	// engine.New(st, nc, log) — بدون payClient و fraudclient در این نسخه
	eng := engine.New(st, nc, log)
	eng.RegisterNATSListeners(nc)

	// ── API ───────────────────────────────────────────────
	apiHandler := api.New(st, eng, nc, cfg.AdminKey)
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	apiHandler.Register(r)

	// ── Start ─────────────────────────────────────────────
	shutCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// RunValidationWorker (نام صحیح در engine.go)
	go eng.RunValidationWorker(shutCtx)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Info("community-service started", ports.F("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http", ports.F("err", err))
		}
	}()

	<-shutCtx.Done()
	log.Info("shutting down...")
	timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(timeout)
}
