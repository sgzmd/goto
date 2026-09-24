package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"goto/internal/link"
)

func RunStoreContractTests(t *testing.T, newStore func(t *testing.T) link.Store) {
	ctx := context.Background()

	t.Run("Create and Get", func(t *testing.T) {
		s := newStore(t)
		l := &link.Link{
			Slug:      "test-link",
			Target:    "https://example.com/dest",
			CreatedAt: time.Now().UTC().Truncate(time.Second),
			UpdatedAt: time.Now().UTC().Truncate(time.Second),
		}

		err := s.Create(ctx, l)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		got, err := s.Get(ctx, "test-link")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Slug != l.Slug || got.Target != l.Target {
			t.Errorf("Get returned %+v, want %+v", got, l)
		}
	})

	t.Run("Get Not Found", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Get(ctx, "non-existent")
		if !errors.Is(err, link.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Create Conflict", func(t *testing.T) {
		s := newStore(t)
		l := &link.Link{Slug: "dup", Target: "https://example.com/1"}
		if err := s.Create(ctx, l); err != nil {
			t.Fatalf("first Create failed: %v", err)
		}

		err := s.Create(ctx, &link.Link{Slug: "dup", Target: "https://example.com/2"})
		if !errors.Is(err, link.ErrConflict) {
			t.Errorf("expected ErrConflict, got %v", err)
		}
	})

	t.Run("Update Existing", func(t *testing.T) {
		s := newStore(t)
		l := &link.Link{Slug: "upd", Target: "https://example.com/old"}
		if err := s.Create(ctx, l); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		l.Target = "https://example.com/new"
		if err := s.Update(ctx, l); err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		got, err := s.Get(ctx, "upd")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Target != "https://example.com/new" {
			t.Errorf("got target %q, want https://example.com/new", got.Target)
		}
	})

	t.Run("Update Not Found", func(t *testing.T) {
		s := newStore(t)
		err := s.Update(ctx, &link.Link{Slug: "missing", Target: "https://example.com"})
		if !errors.Is(err, link.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Delete Existing", func(t *testing.T) {
		s := newStore(t)
		l := &link.Link{Slug: "del", Target: "https://example.com"}
		if err := s.Create(ctx, l); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if err := s.Delete(ctx, "del"); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_, err := s.Get(ctx, "del")
		if !errors.Is(err, link.ErrNotFound) {
			t.Errorf("expected ErrNotFound after Delete, got %v", err)
		}
	})

	t.Run("Delete Not Found", func(t *testing.T) {
		s := newStore(t)
		err := s.Delete(ctx, "missing")
		if !errors.Is(err, link.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("List Links", func(t *testing.T) {
		s := newStore(t)
		links := []*link.Link{
			{Slug: "b", Target: "https://example.com/b"},
			{Slug: "a", Target: "https://example.com/a"},
		}
		for _, l := range links {
			if err := s.Create(ctx, l); err != nil {
				t.Fatalf("Create failed: %v", err)
			}
		}

		got, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 links, got %d", len(got))
		}
		if got[0].Slug != "a" || got[1].Slug != "b" {
			t.Errorf("expected sorted slugs [a, b], got [%s, %s]", got[0].Slug, got[1].Slug)
		}
	})

	t.Run("Concurrent Access", func(t *testing.T) {
		s := newStore(t)
		var wg sync.WaitGroup
		concurrency := 20

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				slug := "link"
				_ = s.Create(ctx, &link.Link{Slug: slug, Target: "https://example.com/init"})
				_, _ = s.Get(ctx, slug)
				_ = s.Update(ctx, &link.Link{Slug: slug, Target: "https://example.com/updated"})
				_, _ = s.List(ctx)
			}(i)
		}
		wg.Wait()
	})
}
