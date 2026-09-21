package service_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// fakeCategorizer is an in-process categorization.Categorizer double. It
// returns a fixed verdict per candidate ref (by index into the batch it was
// called with), and counts how many times Categorize was invoked so tests
// can assert the AI call count / budget accounting.
type fakeCategorizer struct {
	calls int
	// verdicts, if set, is returned verbatim for every call (ignoring the
	// candidates); callers can index by Ref.
	respond func(candidates []categorization.Candidate) []categorization.Verdict
}

func (f *fakeCategorizer) Categorize(_ context.Context, candidates []categorization.Candidate, _ []categorization.CategoryOption) ([]categorization.Verdict, error) {
	f.calls++
	if f.respond == nil {
		return nil, nil
	}
	return f.respond(candidates), nil
}

func (f *fakeCategorizer) Name() string { return "fake" }

func categorySlugID(t *testing.T, g *gorm.DB, slug string) int64 {
	t.Helper()
	var c model.Category
	if err := g.Where("slug = ?", slug).First(&c).Error; err != nil {
		t.Fatalf("load category %q: %v", slug, err)
	}
	return c.ID
}

func seedCategorizationRepos(g *gorm.DB) (repository.TransactionRepository, repository.CategoryRepository, repository.AICategorizationVerdictRepository) {
	return repository.NewTransactionRepository(g),
		repository.NewCategoryRepository(g),
		repository.NewAICategorizationVerdictRepository(g)
}

// seedAIEligibleTxn inserts an uncategorized manual transaction for userID
// against acctID — the shape RunCategorizationPass's scope selects.
func seedAIEligibleTxn(t *testing.T, g *gorm.DB, userID, acctID int64, merchant string, amt decimal.Decimal) *model.Transaction {
	t.Helper()
	m := merchant
	tx := &model.Transaction{
		UserID: userID, AccountID: acctID,
		MerchantName: &m, Amount: amt, Source: "manual",
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed ai-eligible txn: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
	return tx
}

func defaultPassConfig() service.CategorizationPassConfig {
	return service.CategorizationPassConfig{ConfidenceThreshold: 0.6, BatchSize: 20}
}

// TestRunCategorizationPass_CacheHit_NoAICall: a pre-seeded confident cache
// entry categorizes the row without ever calling the categorizer.
func TestRunCategorizationPass_CacheHit_NoAICall(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)
	acctID := seedAccount(t, g, userID)
	txRepo, catRepo, verdictRepo := seedCategorizationRepos(g)
	groceries := categorySlugID(t, g, "groceries")

	txn := seedAIEligibleTxn(t, g, userID, acctID, "Whole Foods", decimal.NewFromInt(-42))

	if err := verdictRepo.Upsert(ctx, &model.AICategorizationVerdict{
		UserID: userID, MerchantKey: "WHOLE FOODS", CategoryID: groceries, Confidence: 0.9, Provider: "fake",
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	fake := &fakeCategorizer{}
	budget := 10
	res, err := service.RunCategorizationPass(ctx, g, txRepo, catRepo, verdictRepo, fake, userID, defaultPassConfig(), &budget)
	if err != nil {
		t.Fatalf("RunCategorizationPass: %v", err)
	}
	if fake.calls != 0 {
		t.Errorf("categorizer called %d times, want 0 (cache hit should be free)", fake.calls)
	}
	if res.CacheHits != 1 || res.Categorized != 1 {
		t.Errorf("res = %+v, want CacheHits=1 Categorized=1", res)
	}
	if budget != 10 {
		t.Errorf("budget = %d, want unchanged 10 (cache hits are free)", budget)
	}

	got, err := txRepo.GetByID(ctx, userID, txn.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != groceries {
		t.Errorf("category_id = %v, want %d", got.CategoryID, groceries)
	}
	if got.CategorizationMethod == nil || *got.CategorizationMethod != "ai" {
		t.Errorf("categorization_method = %v, want ai", got.CategorizationMethod)
	}
}

// TestRunCategorizationPass_CacheMiss_CallsAIAndPopulatesCache: a cache miss
// queues into an AI batch; a confident verdict writes the row and populates
// the cache for next time.
func TestRunCategorizationPass_CacheMiss_CallsAIAndPopulatesCache(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)
	acctID := seedAccount(t, g, userID)
	txRepo, catRepo, verdictRepo := seedCategorizationRepos(g)

	txn := seedAIEligibleTxn(t, g, userID, acctID, "Trader Joes", decimal.NewFromInt(-18))

	fake := &fakeCategorizer{respond: func(candidates []categorization.Candidate) []categorization.Verdict {
		out := make([]categorization.Verdict, len(candidates))
		for i, c := range candidates {
			out[i] = categorization.Verdict{Ref: c.Ref, CategorySlug: "groceries", Confidence: 0.85}
		}
		return out
	}}
	budget := 10
	res, err := service.RunCategorizationPass(ctx, g, txRepo, catRepo, verdictRepo, fake, userID, defaultPassConfig(), &budget)
	if err != nil {
		t.Fatalf("RunCategorizationPass: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("categorizer called %d times, want 1", fake.calls)
	}
	if res.AICalls != 1 || res.Categorized != 1 || res.CacheHits != 0 {
		t.Errorf("res = %+v", res)
	}
	if budget != 9 {
		t.Errorf("budget = %d, want 9 (one AI call consumed)", budget)
	}

	groceries := categorySlugID(t, g, "groceries")
	got, err := txRepo.GetByID(ctx, userID, txn.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != groceries {
		t.Errorf("category_id = %v, want %d", got.CategoryID, groceries)
	}

	cached, err := verdictRepo.Get(ctx, userID, "TRADER JOES")
	if err != nil {
		t.Fatalf("cache should be populated: %v", err)
	}
	if cached.CategoryID != groceries || cached.Confidence != 0.85 {
		t.Errorf("cached verdict = %+v", cached)
	}
}

// TestRunCategorizationPass_LowConfidence_NotCommitted: a verdict below
// threshold is cached (so it isn't re-queried) but never written to the row
// — it stays exactly where "Needs review" already finds it.
func TestRunCategorizationPass_LowConfidence_NotCommitted(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)
	acctID := seedAccount(t, g, userID)
	txRepo, catRepo, verdictRepo := seedCategorizationRepos(g)

	txn := seedAIEligibleTxn(t, g, userID, acctID, "Mystery Merchant", decimal.NewFromInt(-5))

	fake := &fakeCategorizer{respond: func(candidates []categorization.Candidate) []categorization.Verdict {
		out := make([]categorization.Verdict, len(candidates))
		for i, c := range candidates {
			out[i] = categorization.Verdict{Ref: c.Ref, CategorySlug: "groceries", Confidence: 0.2}
		}
		return out
	}}
	budget := 10
	res, err := service.RunCategorizationPass(ctx, g, txRepo, catRepo, verdictRepo, fake, userID, defaultPassConfig(), &budget)
	if err != nil {
		t.Fatalf("RunCategorizationPass: %v", err)
	}
	if res.LowConfidence != 1 || res.Categorized != 0 {
		t.Errorf("res = %+v, want LowConfidence=1 Categorized=0", res)
	}

	got, err := txRepo.GetByID(ctx, userID, txn.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.CategoryID != nil {
		t.Errorf("category_id = %v, want nil (low-confidence must not commit)", got.CategoryID)
	}

	// Cached anyway, so a second pass doesn't pay for another AI call.
	fake2 := &fakeCategorizer{respond: func(candidates []categorization.Candidate) []categorization.Verdict {
		t.Fatal("categorizer should not be called again — low-confidence verdict was cached")
		return nil
	}}
	budget2 := 10
	if _, err := service.RunCategorizationPass(ctx, g, txRepo, catRepo, verdictRepo, fake2, userID, defaultPassConfig(), &budget2); err != nil {
		t.Fatalf("second pass: %v", err)
	}
}

// TestRunCategorizationPass_BudgetExhausted: a zero starting budget queues
// no AI calls and flags BudgetExhausted; the row is left untouched.
func TestRunCategorizationPass_BudgetExhausted(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)
	acctID := seedAccount(t, g, userID)
	txRepo, catRepo, verdictRepo := seedCategorizationRepos(g)

	txn := seedAIEligibleTxn(t, g, userID, acctID, "Budget Test Merchant", decimal.NewFromInt(-9))

	fake := &fakeCategorizer{}
	budget := 0
	res, err := service.RunCategorizationPass(ctx, g, txRepo, catRepo, verdictRepo, fake, userID, defaultPassConfig(), &budget)
	if err != nil {
		t.Fatalf("RunCategorizationPass: %v", err)
	}
	if fake.calls != 0 {
		t.Errorf("categorizer called %d times, want 0 (no budget)", fake.calls)
	}
	if !res.BudgetExhausted {
		t.Errorf("res.BudgetExhausted = false, want true")
	}

	got, err := txRepo.GetByID(ctx, userID, txn.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.CategoryID != nil {
		t.Errorf("category_id = %v, want nil (out of budget)", got.CategoryID)
	}
}

// TestRunCategorizationPass_SkipsManualAndRuleRows: rows already at
// categorization_method manual or rule are structurally outside the scope
// — the pass must never touch them, enforcing "AI never overrules a human
// or rule decision."
func TestRunCategorizationPass_SkipsManualAndRuleRows(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)
	acctID := seedAccount(t, g, userID)
	txRepo, catRepo, verdictRepo := seedCategorizationRepos(g)
	dining := categorySlugID(t, g, "food-and-dining")

	manualMethod := "manual"
	manualCat := dining
	manual := &model.Transaction{
		UserID: userID, AccountID: acctID, MerchantName: ptrStr("Manual Pick"),
		Amount: decimal.NewFromInt(-10), Source: "manual",
		CategoryID: &manualCat, CategorizationMethod: &manualMethod,
	}
	if err := g.Create(manual).Error; err != nil {
		t.Fatalf("seed manual txn: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, manual.ID) })

	fake := &fakeCategorizer{respond: func(candidates []categorization.Candidate) []categorization.Verdict {
		t.Fatal("categorizer should not be called — nothing eligible in scope")
		return nil
	}}
	budget := 10
	res, err := service.RunCategorizationPass(ctx, g, txRepo, catRepo, verdictRepo, fake, userID, defaultPassConfig(), &budget)
	if err != nil {
		t.Fatalf("RunCategorizationPass: %v", err)
	}
	if res.Scanned != 0 {
		t.Errorf("Scanned = %d, want 0 (manual row is structurally out of scope)", res.Scanned)
	}

	got, err := txRepo.GetByID(ctx, userID, manual.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if *got.CategorizationMethod != "manual" || *got.CategoryID != dining {
		t.Errorf("manual row was mutated: category=%v method=%v", got.CategoryID, got.CategorizationMethod)
	}
}
