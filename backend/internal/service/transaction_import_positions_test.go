package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

func brokerageExtraction(symbol, qty, closing string) *ingestion.Extraction {
	asOf := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	bal, at, _ := ingestion.ParseClosingBalance(closing, "2026-05-31")
	return &ingestion.Extraction{
		Source: "pdf",
		Rows: []ingestion.ParsedRow{
			ingestion.NewRow(1, "2026-05-15", "100.00", "Dividend", 0.95),
		},
		Positions: []ingestion.ParsedPosition{
			ingestion.NewPosition(1, symbol, "Fixture Fund", "fund", qty, "10", "", "", asOf, 0.95),
			ingestion.NewPosition(2, symbol+"X", "", "fund", "lots", "", "", "", asOf, 0.95),
		},
		ClosingBalance:   &bal,
		ClosingBalanceAt: &at,
	}
}

// TestImportStatement_PositionsReconcile: preview shows positions + resulting
// adjustments and writes nothing; commit reconciles through ReconcilePosition;
// a repeat commit is a no-op (idempotent); a changed statement writes an
// adjustment linked to its observation.
func TestImportStatement_PositionsReconcile(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	assets := repository.NewAssetRepository(g)
	svc = svc.WithStatementPositions(g, assets)
	ctx := context.Background()
	symbol := fmt.Sprintf("T%d", time.Now().UnixNano()%1_000_000_000)

	t.Cleanup(func() { g.Unscoped().Where("symbol IN ?", []string{symbol, symbol + "X"}).Delete(&model.Asset{}) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Position{}) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{}) })
	t.Cleanup(func() { g.Where("account_id = ?", accountID).Delete(&model.AccountBalanceObservation{}) })

	// Preview: nothing written, new asset flagged, effects computed.
	prev, err := svc.ImportStatement(ctx, userID, accountID, brokerageExtraction(symbol, "12.5", "1100.00"), false)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(prev.Positions) != 2 {
		t.Fatalf("positions = %d, want 2", len(prev.Positions))
	}
	p0 := prev.Positions[0]
	if p0.AssetStatus != service.AssetNew || p0.Effect == nil || !p0.Effect.Adjustment.Equal(decimal.RequireFromString("12.5")) || p0.Effect.Kind != model.KindOpeningBalance {
		t.Errorf("position preview = %+v effect=%+v", p0, p0.Effect)
	}
	if prev.Positions[1].Error == "" || prev.Positions[1].Effect != nil {
		t.Errorf("invalid position must carry an error and no effect: %+v", prev.Positions[1])
	}
	// Cash: reported 1100, fold 0 + 100 new row → opening_balance of 1000.
	if cb := prev.ClosingBalance; cb == nil || !cb.Effect.Adjustment.Equal(decimal.RequireFromString("1000")) || cb.Effect.Kind != model.KindOpeningBalance {
		t.Errorf("closing balance preview = %+v", cb)
	}
	var n int64
	g.Model(&model.AccountBalanceObservation{}).Where("account_id = ?", accountID).Count(&n)
	if n != 0 {
		t.Fatalf("preview wrote %d observations", n)
	}

	// Commit.
	res, err := svc.ImportStatement(ctx, userID, accountID, brokerageExtraction(symbol, "12.5", "1100.00"), true)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.InsertedCount != 1 || res.PositionsReconciled != 2 || res.AdjustmentsWritten != 2 {
		t.Errorf("commit result inserted=%d reconciled=%d adjustments=%d", res.InsertedCount, res.PositionsReconciled, res.AdjustmentsWritten)
	}
	asset, err := assets.GetBySymbolKind(ctx, symbol, "fund")
	if err != nil {
		t.Fatalf("asset not created: %v", err)
	}
	txRepo := repository.NewTransactionRepository(g)
	assertFold(t, txRepo, userID, accountID, asset.ID, "12.5")
	assertFold(t, txRepo, userID, accountID, usdID(t, g, accountID), "1100")
	var pos model.Position
	if err := g.Where("account_id = ? AND asset_id = ?", accountID, asset.ID).First(&pos).Error; err != nil || !pos.Quantity.Equal(decimal.RequireFromString("12.5")) {
		t.Errorf("position row = %+v err=%v", pos, err)
	}

	// Re-import identical: no new reconciling rows, no duplicate flows.
	res, err = svc.ImportStatement(ctx, userID, accountID, brokerageExtraction(symbol, "12.5", "1100.00"), true)
	if err != nil {
		t.Fatalf("re-commit: %v", err)
	}
	if res.InsertedCount != 0 || res.AdjustmentsWritten != 0 {
		t.Errorf("re-import must be a no-op: inserted=%d adjustments=%d", res.InsertedCount, res.AdjustmentsWritten)
	}
	assertFold(t, txRepo, userID, accountID, asset.ID, "12.5")

	// A later statement reports a different quantity → an adjustment (never a trade).
	prev, _ = svc.ImportStatement(ctx, userID, accountID, brokerageExtraction(symbol, "15", "1100.00"), false)
	if e := prev.Positions[0].Effect; prev.Positions[0].AssetStatus != service.AssetExisting || e.Kind != model.KindAdjustment || !e.Adjustment.Equal(decimal.RequireFromString("2.5")) {
		t.Errorf("drift preview effect = %+v", e)
	}
	if _, err = svc.ImportStatement(ctx, userID, accountID, brokerageExtraction(symbol, "15", "1100.00"), true); err != nil {
		t.Fatal(err)
	}
	assertFold(t, txRepo, userID, accountID, asset.ID, "15")
	var adj model.Transaction
	if err := g.Where("account_id = ? AND asset_id = ? AND kind = ?", accountID, asset.ID, model.KindAdjustment).First(&adj).Error; err != nil || adj.CausedByObservationID == nil {
		t.Errorf("adjustment missing or unlinked: %+v err=%v", adj, err)
	}
}

func usdID(t *testing.T, g *gorm.DB, accountID int64) int64 {
	t.Helper()
	var a model.Account
	if err := g.First(&a, accountID).Error; err != nil {
		t.Fatal(err)
	}
	return a.PrimaryQuoteAssetID
}

// TestImportStatement_LiabilityClosingBalanceSigned: a credit-card closing
// balance arrives as the signed account value (the extractor contract) and is
// kept as-is: an amount owed stays negative, a credit stays positive (#670),
// and preview, observation, ledger and position agree; a re-import is a no-op.
func TestImportStatement_LiabilityClosingBalanceSigned(t *testing.T) {
	for _, tc := range []struct{ name, closing string }{
		{"owed", "-450.00"},
		{"in credit", "20.00"},
		{"zero", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, userID, accountID, g := newImportSvc(t)
			svc = svc.WithStatementPositions(g, repository.NewAssetRepository(g))
			if err := g.Model(&model.Account{}).Where("id = ?", accountID).Update("account_type", "credit_card").Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Position{}) })
			t.Cleanup(func() { g.Where("account_id = ?", accountID).Delete(&model.AccountBalanceObservation{}) })
			ctx := context.Background()
			want := decimal.RequireFromString(tc.closing)
			bal, at, _ := ingestion.ParseClosingBalance(tc.closing, "2026-05-31")
			ext := &ingestion.Extraction{
				Source:           "pdf",
				Rows:             []ingestion.ParsedRow{ingestion.NewRow(1, "2026-05-20", "-30.00", "Card purchase", 0.95)},
				ClosingBalance:   &bal,
				ClosingBalanceAt: &at,
			}
			prev, err := svc.ImportStatement(ctx, userID, accountID, ext, false)
			if err != nil {
				t.Fatal(err)
			}
			if prev.ClosingBalance == nil || !prev.ClosingBalance.Reported.Equal(want) ||
				!prev.ClosingBalance.Effect.Adjustment.Equal(want.Add(decimal.NewFromInt(30))) {
				t.Fatalf("preview = %+v, want reported %s and adjustment %s", prev.ClosingBalance, want, want.Add(decimal.NewFromInt(30)))
			}
			txRepo := repository.NewTransactionRepository(g)
			cash := usdID(t, g, accountID)
			for range 2 {
				if _, err := svc.ImportStatement(ctx, userID, accountID, ext, true); err != nil {
					t.Fatal(err)
				}
				assertFold(t, txRepo, userID, accountID, cash, want.String())
				var pos model.Position
				if err := g.Where("account_id = ? AND asset_id = ?", accountID, cash).First(&pos).Error; err != nil {
					t.Fatalf("position: %v", err)
				}
				if !pos.Quantity.Equal(want) {
					t.Errorf("position = %s, want %s", pos.Quantity, want)
				}
			}
			var obs []model.AccountBalanceObservation
			if err := g.Where("account_id = ?", accountID).Find(&obs).Error; err != nil {
				t.Fatal(err)
			}
			for _, o := range obs {
				if !o.ObservedQuantity.Equal(want) {
					t.Errorf("observation = %s, want %s", o.ObservedQuantity, want)
				}
			}
			var reconciling int64
			g.Model(&model.Transaction{}).Where("account_id = ? AND kind IN ?", accountID,
				[]string{model.KindOpeningBalance, model.KindAdjustment}).Count(&reconciling)
			if reconciling != 1 {
				t.Errorf("reconciling rows = %d, want 1 (re-import adds none)", reconciling)
			}
		})
	}
}
