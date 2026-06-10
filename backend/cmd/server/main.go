package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/router"
	"github.com/gregwym/offbook/backend/internal/service/prices"
)

func main() {
	cfg := config.MustLoad()

	if cfg.SessionSecret == "" {
		log.Fatal("SESSION_SECRET is empty — required from M2.5+. Generate with: openssl rand -hex 32")
	}

	gormDB, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer func() {
		if cerr := db.Close(gormDB); cerr != nil {
			log.Printf("db close: %v", cerr)
		}
	}()

	if cfg.MigrationsPath != "" {
		if err := db.RunMigrations(cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
			log.Fatalf("migrations: %v", err)
		}
	}

	r := router.New(cfg, gormDB)

	// Scheduled price refresh (#338 Phase 3): daily background pass over
	// users who opted in via Settings (ADR-0014 §3 — background egress
	// needs stored consent). Stops with the server context below.
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	settingsRepo := repository.NewUserSettingsRepository(gormDB)
	prices.NewScheduler(
		prices.NewService(
			repository.NewUserRepository(gormDB),
			repository.NewPositionRepository(gormDB),
			repository.NewAssetRepository(gormDB),
			repository.NewPriceRepository(gormDB),
			prices.NewCoinGecko(), prices.NewFrankfurter(),
		),
		settingsRepo.ListAutoRefreshUserIDs,
	).Start(schedulerCtx)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("offbook backend listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server exited: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}
