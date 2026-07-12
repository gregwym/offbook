package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/logging"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/router"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/diskspace"
	"github.com/gregwym/offbook/backend/internal/service/household"
	"github.com/gregwym/offbook/backend/internal/service/jobs"
	"github.com/gregwym/offbook/backend/internal/service/notify"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
	"github.com/gregwym/offbook/backend/internal/service/prices"
)

func main() {
	logging.Init()

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

	// Background job runner (#359, ADR-0020). One in-app scheduler owns every
	// periodic maintenance task; each job logs its outcome and alerts the
	// Notifier on failure. Stops with the server context below. The M13
	// notifier (#360) is wired here: ntfy/webhook when configured, log-only
	// otherwise (see internal/service/notify.Build).
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	notifier := notify.Build(cfg, log.Printf)
	runner := jobs.NewRunner(log.Printf, notifier)

	// price-refresh (#338 Phase 3): daily background pass over users who opted
	// in via Settings (ADR-0014 §3 — background egress needs stored consent).
	settingsRepo := repository.NewUserSettingsRepository(gormDB)
	priceScheduler := prices.NewScheduler(
		prices.NewService(
			repository.NewUserRepository(gormDB),
			repository.NewPositionRepository(gormDB),
			repository.NewAssetRepository(gormDB),
			repository.NewPriceRepository(gormDB),
			prices.NewCoinGecko(), prices.NewFrankfurter(),
		),
		settingsRepo.ListAutoRefreshUserIDs,
	)
	runner.Register(jobs.Job{
		Name:         "price-refresh",
		Interval:     24 * time.Hour,
		InitialDelay: time.Minute, // let boot settle before upstream calls
		Run: func(ctx context.Context) (string, error) {
			// RunOnce logs per-user detail and never aborts the pass on one
			// user's provider failure; the pass itself has no fatal error.
			priceScheduler.RunOnce(ctx)
			return "refresh pass complete", nil
		},
	})

	// household-purge (#359): grace-period purge is a privacy promise
	// (ADR-0007) — it must run without the owner remembering the CLI.
	runner.Register(jobs.Job{
		Name:         "household-purge",
		Interval:     24 * time.Hour,
		InitialDelay: time.Minute,
		Run: func(ctx context.Context) (string, error) {
			res, err := household.RunPurge(ctx, gormDB, time.Now())
			if err != nil {
				return "", err
			}
			if res.MembersPurged == 0 && res.SharesDeleted == 0 {
				return "nothing to purge", nil
			}
			return fmt.Sprintf("purged %d members, removed %d account_shares",
				res.MembersPurged, res.SharesDeleted), nil
		},
	})

	// ingestion-jobs-purge (#337): reclaim abandoned AI-import staging payloads
	// (status='extracted' past retention) so their JSONB doesn't accumulate.
	runner.Register(jobs.Job{
		Name:         "ingestion-jobs-purge",
		Interval:     24 * time.Hour,
		InitialDelay: time.Minute,
		Run: func(ctx context.Context) (string, error) {
			res, err := service.PurgeStaleAIStaging(ctx, gormDB, time.Now(), service.DefaultAIStagingRetention)
			if err != nil {
				return "", err
			}
			if res.JobsPurged == 0 {
				return "nothing to purge", nil
			}
			return fmt.Sprintf("purged %d stale AI-staging job(s)", res.JobsPurged), nil
		},
	})

	// plaid-transaction-sync (#363, docs/ADR/0021): daily jittered polling
	// pass over every active plaid_item, since a Tailscale-private host
	// can't receive Plaid webhooks (ADR-0016). Builds its own plaid.Service
	// instance — the same construction router.go's newPlaidService and
	// cmd/plaid-resync already duplicate — so the job runner doesn't share
	// mutable state with the HTTP-facing service. No-op when Plaid isn't
	// configured on this instance.
	if cfg.PlaidConfigured() {
		plaidClient, err := plaidsvc.NewSDKClient(plaidsvc.Config{
			ClientID: cfg.PlaidClientID,
			Secret:   cfg.PlaidSecret,
			Env:      cfg.PlaidEnv,
		})
		if err != nil {
			log.Fatalf("plaid: sdk client: %v", err)
		}
		plaidBox, err := crypto.NewSecretBox(cfg.PlaidTokenKey)
		if err != nil {
			log.Fatalf("plaid: secretbox: %v", err)
		}
		plaidMapper, err := plaidsvc.NewCategoryMapper(context.Background(), repository.NewPlaidCategoryMapRepository(gormDB))
		if err != nil {
			log.Fatalf("plaid: load category map: %v", err)
		}
		plaidAccountRepo := repository.NewAccountRepository(gormDB)
		plaidAssetRepo := repository.NewAssetRepository(gormDB)
		plaidPositionRepo := repository.NewPositionRepository(gormDB)
		plaidPiiSvc := service.NewPIIService(
			repository.NewPIIRepository(gormDB),
			service.NewAccountService(gormDB, plaidAccountRepo, plaidAssetRepo, plaidPositionRepo),
		)
		plaidItemRepo := repository.NewPlaidItemRepository(gormDB)
		plaidSyncSvc := plaidsvc.NewService(
			plaidClient, plaidBox, plaidItemRepo, plaidAccountRepo,
			repository.NewTransactionRepository(gormDB),
			repository.NewPlaidSyncErrorRepository(gormDB),
			plaidAssetRepo, plaidPositionRepo, plaidPiiSvc, plaidMapper, gormDB,
		).WithRuleRepo(repository.NewCategorizationRuleRepository(gormDB)).
			WithNotifier(notify.Build(cfg, log.Printf))

		plaidScheduler := plaidsvc.NewSyncScheduler(plaidSyncSvc, plaidItemRepo)
		runner.Register(jobs.Job{
			Name:         "plaid-transaction-sync",
			Interval:     24 * time.Hour,
			InitialDelay: 3 * time.Minute,
			Run: func(ctx context.Context) (string, error) {
				res := plaidScheduler.RunOnce(ctx)
				return fmt.Sprintf("synced %d, skipped %d, failed %d", res.Synced, res.Skipped, res.Failed), nil
			},
		})
	}

	// disk-space-check (#360): a full data volume degrades silently otherwise
	// (Postgres refuses writes, backups fail) — alert well before that.
	runner.Register(jobs.Job{
		Name:         "disk-space-check",
		Interval:     6 * time.Hour,
		InitialDelay: 2 * time.Minute,
		Run: func(ctx context.Context) (string, error) {
			free, err := diskspace.FreePercent(cfg.DiskCheckPath)
			if err != nil {
				return "", fmt.Errorf("check disk space: %w", err)
			}
			if free < cfg.LowDiskThresholdPercent {
				return "", fmt.Errorf("low disk space: %.1f%% free on %s (threshold %.1f%%)", free, cfg.DiskCheckPath, cfg.LowDiskThresholdPercent)
			}
			return fmt.Sprintf("%.1f%% free on %s", free, cfg.DiskCheckPath), nil
		},
	})

	runner.Start(schedulerCtx)

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
