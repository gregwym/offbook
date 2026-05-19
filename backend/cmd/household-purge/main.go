// Household-purge CLI. Seals in-grace member rows whose grace window has
// elapsed and physically removes the corresponding account_shares (see
// ADR-0007 § Purge on expiry).
//
//	go run ./cmd/household-purge
//
// Idempotent — re-running on a clean DB is a no-op. Operator wires this
// into cron / launchd / k8s CronJob. The aggregator's read-side behavior
// is unchanged with or without this runner (it filters lazily by time).
package main

import (
	"context"
	"log"
	"time"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

func main() {
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

	res, err := household.RunPurge(ctx, g, time.Now())
	if err != nil {
		log.Fatalf("purge: %v", err)
	}
	log.Printf("household-purge: purged %d members, removed %d account_shares rows",
		res.MembersPurged, res.SharesDeleted)
}
