package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"goto/internal/link"
)

type Store struct {
	mu    sync.RWMutex
	links map[string]*link.Link
}

func New() *Store {
	return &Store{
		links: make(map[string]*link.Link),
	}
}

func (s *Store) Get(ctx context.Context, slug string) (*link.Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	l, ok := s.links[slug]
	if !ok {
		return nil, link.ErrNotFound
	}
	cp := *l
	return &cp, nil
}

func (s *Store) List(ctx context.Context) ([]*link.Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]*link.Link, 0, len(s.links))
	for _, l := range s.links {
		cp := *l
		res = append(res, &cp)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Slug < res[j].Slug
	})

	return res, nil
}

func (s *Store) Create(ctx context.Context, l *link.Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.links[l.Slug]; exists {
		return link.ErrConflict
	}

	now := time.Now().UTC()
	cp := *l
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = now
	}
	s.links[l.Slug] = &cp
	return nil
}

func (s *Store) Update(ctx context.Context, l *link.Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.links[l.Slug]
	if !ok {
		return link.ErrNotFound
	}

	existing.Target = l.Target
	existing.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) Delete(ctx context.Context, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.links[slug]; !ok {
		return link.ErrNotFound
	}

	delete(s.links, slug)
	return nil
}
