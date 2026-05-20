// One-shot Plaid resync for a single user, bypassing the UI auth path.
// Wired against the running offbook_dev DB; primarily for local recovery
// after a categorization-related fix (e.g. #181) when the existing item
// has already drained its cursor.
//
// Usage: cd backend && APP_ENV=dev go run ./cmd/plaid-resync <user_id> <plaid_item_id>
//
// Effect: invokes service.SyncTransactions on the running service stack
// (same code path as POST /api/v1/plaid/items/:item_id/sync-transactions)
// until has_more=false. Re-imports every transaction from the cursor's
// current position. Reset the cursor with
// `UPDATE plaid_items SET cursor=NULL` first if you want a full replay.
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: plaid-resync <user_id> <plaid_item_id>")
	}
	userID, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil {
		log.Fatalf("invalid user_id: %v", err)
	}
	plaidItemID := os.Args[2]

	cfg := config.MustLoad()
	if !cfg.PlaidConfigured() {
		log.Fatal("Plaid not configured")
	}
	if dbName, err := databaseName(cfg.DatabaseURL); err == nil && dbName == "offbook_test" {
		log.Fatal("refusing to run against offbook_test")
	}
	gormDB, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}

	client, err := plaidsvc.NewSDKClient(plaidsvc.Config{
		ClientID: cfg.PlaidClientID,
		Secret:   cfg.PlaidSecret,
		Env:      cfg.PlaidEnv,
	})
	if err != nil {
		log.Fatalf("plaid client: %v", err)
	}
	box, err := crypto.NewSecretBox(cfg.PlaidTokenKey)
	if err != nil {
		log.Fatalf("crypto: %v", err)
	}

	itemRepo := repository.NewPlaidItemRepository(gormDB)
	acctRepo := repository.NewAccountRepository(gormDB)
	txRepo := repository.NewTransactionRepository(gormDB)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(gormDB), service.NewAccountService(acctRepo))
	mapper, err := plaidsvc.NewCategoryMapper(context.Background(),
		repository.NewPlaidCategoryMapRepository(gormDB))
	if err != nil {
		log.Fatalf("mapper: %v", err)
	}
	fmt.Printf("loaded %d PFC mappings\n", mapper.Size())

	svc := plaidsvc.NewService(
		client, box, itemRepo, acctRepo, txRepo,
		repository.NewPlaidSyncErrorRepository(gormDB), piiSvc, mapper, gormDB,
	).WithRuleRepo(repository.NewCategorizationRuleRepository(gormDB))

	r, err := svc.SyncTransactions(context.Background(), userID, plaidItemID)
	if err != nil {
		log.Fatalf("sync: %v", err)
	}
	fmt.Printf("inserted=%d resurrected=%d modified=%d removed=%d failed=%d\n",
		r.Inserted, r.Resurrected, r.Modified, r.Removed, r.Failed)
}

func databaseName(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "", fmt.Errorf("no database name in URL")
	}
	return name, nil
}
