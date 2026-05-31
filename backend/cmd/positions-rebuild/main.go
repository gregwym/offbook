// Regenerate positions from the transaction ledger (ADR-0017): positions is a
// cache, not a source of truth. This rebuilds positions.quantity = Σ
// transactions.amount per (account, asset) — and the average-cost basis — so a
// drifted or wiped positions table can be reconstructed entirely from facts.
//
// Usage: cd backend && APP_ENV=dev go run ./cmd/positions-rebuild <user_id|all>
//
// Effect: for each (account, asset) pair with transactions, recompute quantity
// + cost basis via the same valuation path the trade/Plaid surfaces use and
// upsert the position. Idempotent; one transaction per user.
package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: positions-rebuild <user_id|all>")
	}

	cfg := config.MustLoad()
	gormDB, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}

	prices := repository.NewPriceRepository(gormDB)
	users := repository.NewUserRepository(gormDB)
	ctx := context.Background()

	var ids []int64
	if os.Args[1] == "all" {
		ids, err = allUserIDs(gormDB)
		if err != nil {
			log.Fatalf("list users: %v", err)
		}
	} else {
		id, err := strconv.ParseInt(os.Args[1], 10, 64)
		if err != nil {
			log.Fatalf("invalid user_id: %v", err)
		}
		ids = []int64{id}
	}

	for _, id := range ids {
		res, err := service.RebuildPositions(ctx, gormDB, prices, users, id)
		if err != nil {
			log.Fatalf("rebuild user %d: %v", id, err)
		}
		log.Printf("user %d: rebuilt %d position(s) across %d (account, asset) pair(s)", id, res.Updated, res.Pairs)
	}
}

func allUserIDs(g *gorm.DB) ([]int64, error) {
	var ids []int64
	err := g.Model(&model.User{}).Where("deleted_at IS NULL").Order("id").Pluck("id", &ids).Error
	return ids, err
}
