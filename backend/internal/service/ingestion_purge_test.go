package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// seedIngestionJob inserts an ingestion_jobs row and back-dates created_at to
// ageDays ago (UpdateColumn bypasses GORM's autoCreateTime). Cleanup is
// registered since seedTestUser's cascade doesn't cover ingestion_jobs.
func seedIngestionJob(t *testing.T, g *gorm.DB, userID int64, status string, extraction json.RawMessage, ageDays int) int64 {
	t.Helper()
	job := &model.IngestionJob{
		UserID:     userID,
		Source:     "pdf",
		Status:     status,
		Extraction: extraction,
	}
	if err := g.Create(job).Error; err != nil {
		t.Fatalf("seed ingestion job: %v", err)
	}
	created := time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour)
	if err := g.Model(&model.IngestionJob{}).Where("id = ?", job.ID).
		UpdateColumn("created_at", created).Error; err != nil {
		t.Fatalf("backdate created_at: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.IngestionJob{}, job.ID) })
	return job.ID
}

func reload(t *testing.T, g *gorm.DB, id int64) model.IngestionJob {
	t.Helper()
	var j model.IngestionJob
	if err := g.First(&j, id).Error; err != nil {
		t.Fatalf("reload job %d: %v", id, err)
	}
	return j
}

// TestPurgeStaleAIStaging_PurgesOldExtractedOnly: an old, uncommitted
// 'extracted' stage is reclaimed (payload nulled, moved to 'failed'); a recent
// stage, a 'completed' job, and an already-emptied stage are all preserved.
func TestPurgeStaleAIStaging_PurgesOldExtractedOnly(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)

	payload := json.RawMessage(`{"rows":[{"amount":"1.00"}]}`)
	now := time.Now()

	oldStale := seedIngestionJob(t, g, userID, "extracted", payload, 10)  // > 7d → purge
	recent := seedIngestionJob(t, g, userID, "extracted", payload, 1)     // < 7d → keep
	completed := seedIngestionJob(t, g, userID, "completed", payload, 30) // not extracted → keep
	emptied := seedIngestionJob(t, g, userID, "extracted", nil, 30)       // no payload → keep (idempotent)

	// Dry-run count sees exactly the one purgeable row.
	n, err := service.CountStaleAIStaging(ctx, g, now, service.DefaultAIStagingRetention)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountStaleAIStaging = %d, want 1", n)
	}

	res, err := service.PurgeStaleAIStaging(ctx, g, now, service.DefaultAIStagingRetention)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if res.JobsPurged != 1 {
		t.Fatalf("JobsPurged = %d, want 1", res.JobsPurged)
	}

	// The old stale row: payload gone, moved to terminal 'failed' with a reason.
	got := reload(t, g, oldStale)
	if got.Status != "failed" {
		t.Errorf("oldStale status = %q, want failed", got.Status)
	}
	if got.Extraction != nil {
		t.Errorf("oldStale extraction should be nil, got %s", got.Extraction)
	}
	if got.ErrorMessage == nil || *got.ErrorMessage == "" {
		t.Error("oldStale should have an explanatory error_message")
	}
	if got.CompletedAt == nil {
		t.Error("oldStale should have completed_at set")
	}

	// Recent stage untouched — still committable.
	if r := reload(t, g, recent); r.Status != "extracted" || r.Extraction == nil {
		t.Errorf("recent stage altered: status=%q extraction=%v", r.Status, r.Extraction)
	}
	// Completed job untouched.
	if c := reload(t, g, completed); c.Status != "completed" || c.Extraction == nil {
		t.Errorf("completed job altered: status=%q extraction=%v", c.Status, c.Extraction)
	}
	// Already-emptied 'extracted' row is not matched (extraction IS NULL).
	if e := reload(t, g, emptied); e.Status != "extracted" {
		t.Errorf("emptied stage altered: status=%q", e.Status)
	}

	// Idempotent: a second pass reclaims nothing.
	res2, err := service.PurgeStaleAIStaging(ctx, g, now, service.DefaultAIStagingRetention)
	if err != nil {
		t.Fatalf("purge (2nd): %v", err)
	}
	if res2.JobsPurged != 0 {
		t.Errorf("second purge JobsPurged = %d, want 0", res2.JobsPurged)
	}
}

// TestPurgeStaleAIStaging_RetentionBoundary: retention is honored — a stage just
// inside the window is kept, one just outside is purged.
func TestPurgeStaleAIStaging_RetentionBoundary(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedTestUser(t, g)
	payload := json.RawMessage(`{"rows":[]}`)
	now := time.Now()

	inside := seedIngestionJob(t, g, userID, "extracted", payload, 5)  // 5d < 7d → keep
	outside := seedIngestionJob(t, g, userID, "extracted", payload, 9) // 9d > 7d → purge

	res, err := service.PurgeStaleAIStaging(ctx, g, now, service.DefaultAIStagingRetention)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if res.JobsPurged != 1 {
		t.Fatalf("JobsPurged = %d, want 1", res.JobsPurged)
	}
	if r := reload(t, g, inside); r.Status != "extracted" {
		t.Errorf("inside-window stage was purged: status=%q", r.Status)
	}
	if r := reload(t, g, outside); r.Status != "failed" {
		t.Errorf("outside-window stage not purged: status=%q", r.Status)
	}
}
