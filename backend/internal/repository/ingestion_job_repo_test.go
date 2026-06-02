package repository_test

import (
	"context"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// TestIngestionJobRepo_GetForUser_Tenancy verifies a job is only returned to
// its owner — the multi-tenant guard required for the AI staging store, since
// CommitJob trusts GetForUser to scope by user before applying staged rows.
func TestIngestionJobRepo_GetForUser_Tenancy(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewIngestionJobRepository(g)
	ctx := context.Background()

	userA := seedTestUser(t, g)
	userB := seedTestUser(t, g)

	job := &model.IngestionJob{
		UserID: userA,
		Source: "pdf",
		Status: "extracted",
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.IngestionJob{}, job.ID) })

	// Owner can read it.
	got, err := repo.GetForUser(ctx, userA, job.ID)
	if err != nil {
		t.Fatalf("owner GetForUser: %v", err)
	}
	if got.ID != job.ID {
		t.Errorf("got id %d, want %d", got.ID, job.ID)
	}

	// A different user must not.
	if _, err := repo.GetForUser(ctx, userB, job.ID); err != repository.ErrNotFound {
		t.Errorf("cross-user GetForUser err = %v, want ErrNotFound", err)
	}
}

func TestIngestionJobRepo_Update(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewIngestionJobRepository(g)
	ctx := context.Background()
	userID := seedTestUser(t, g)

	job := &model.IngestionJob{UserID: userID, Source: "pdf", Status: "extracted"}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.IngestionJob{}, job.ID) })

	job.Status = "completed"
	imported := 3
	job.RowsImported = &imported
	if err := repo.Update(ctx, job); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.GetForUser(ctx, userID, job.ID)
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if got.Status != "completed" || got.RowsImported == nil || *got.RowsImported != 3 {
		t.Errorf("after update: status=%q rows_imported=%v", got.Status, got.RowsImported)
	}
}
