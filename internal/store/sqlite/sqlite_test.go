package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"goto/internal/link"
	"goto/internal/store/storetest"
)

func TestSQLiteContract(t *testing.T) {
	storetest.RunStoreContractTests(t, func(t *testing.T) link.Store {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		s, err := Open(dbPath)
		if err != nil {
			t.Fatalf("failed to open sqlite store: %v", err)
		}
		t.Cleanup(func() {
			_ = s.Close()
		})
		return s
	})
}

func TestSQLitePersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	// Phase 1: Open, create link, update link, close
	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	l := &link.Link{
		Slug:   "persistent",
		Target: "https://example.com/v1",
	}
	if err := s1.Create(ctx, l); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	l.Target = "https://example.com/v2"
	if err := s1.Update(ctx, l); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if err := s1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Phase 2: Reopen same database file, verify data intact
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer s2.Close()

	got, err := s2.Get(ctx, "persistent")
	if err != nil {
		t.Fatalf("Get after reopen failed: %v", err)
	}

	if got.Slug != "persistent" {
		t.Errorf("got slug %q, want persistent", got.Slug)
	}
	if got.Target != "https://example.com/v2" {
		t.Errorf("got target %q, want https://example.com/v2", got.Target)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("expected non-zero timestamps, got %+v", got)
	}
}

func TestSQLiteConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer s.Close()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			l := &link.Link{
				Slug:      "test-link",
				Target:    "https://example.com",
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}
			_ = s.Create(ctx, l)
			_, _ = s.Get(ctx, "test-link")
			_ = s.Update(ctx, &link.Link{Slug: "test-link", Target: "https://example.com/2"})
			_, _ = s.List(ctx)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
