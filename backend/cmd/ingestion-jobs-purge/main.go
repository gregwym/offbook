// Ingestion-jobs-purge CLI. Reclaims abandoned AI-import staging rows: an
// ingestion_jobs row that sat at status='extracted' with its staged `extraction`
// JSONB past the retention window (ADR-0019 §7, #337) has its payload nulled and
// the row moved to a terminal 'failed' state. The audit row itself survives —
// ingestion_jobs is append-only.
//
//	go run ./cmd/ingestion-jobs-purge                       # dry run (default)
//	go run ./cmd/ingestion-jobs-purge --apply               # actually purge
//	go run ./cmd/ingestion-jobs-purge --retention-days 14   # override window
//
// Idempotent — re-running is a no-op once nothing is stale. The in-app job
// runner (#359) runs this same purge daily; this CLI is for an out-of-band run.
// Uses config.Load() for DB selection.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/service"
)

func main() {
	apply := flag.Bool("apply", false, "actually purge (default is dry-run)")
	retentionDays := flag.Int("retention-days", 7, "purge extracted stages older than this many days")
	flag.Parse()

	if *retentionDays < 0 {
		log.Fatal("--retention-days must be >= 0")
	}
	retention := time.Duration(*retentionDays) * 24 * time.Hour

	cfg := config.MustLoad()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is empty")
	}
	g, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db.Open: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	now := time.Now()
	if !*apply {
		n, err := service.CountStaleAIStaging(ctx, g, now, retention)
		if err != nil {
			log.Fatalf("count: %v", err)
		}
		log.Printf("ingestion-jobs-purge: DRY RUN — %d stale AI-staging job(s) older than %d day(s) would be purged. Re-run with --apply to execute.",
			n, *retentionDays)
		return
	}

	res, err := service.PurgeStaleAIStaging(ctx, g, now, retention)
	if err != nil {
		log.Fatalf("purge: %v", err)
	}
	log.Printf("ingestion-jobs-purge: purged %d stale AI-staging job(s) older than %d day(s)",
		res.JobsPurged, *retentionDays)
}
