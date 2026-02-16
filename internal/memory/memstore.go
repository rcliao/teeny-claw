package memory

import (
	"context"
	"strings"
	"sync"
)

// MemStore is an in-memory Store implementation for testing and lightweight use.
type MemStore struct {
	mu      sync.RWMutex
	entries []Entry
}

// NewMemStore creates an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{}
}

func (s *MemStore) Save(_ context.Context, entries ...Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entries...)
	return nil
}

func (s *MemStore) Search(_ context.Context, query string, opts SearchOpts) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	var results []Entry
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	for _, e := range s.entries {
		if len(opts.Kinds) > 0 && !containsKind(opts.Kinds, e.Kind) {
			continue
		}
		if strings.Contains(strings.ToLower(e.Content), query) {
			results = append(results, e)
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func (s *MemStore) List(_ context.Context, kind Kind, limit int) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}
	var results []Entry
	// Iterate reverse for newest-first.
	for i := len(s.entries) - 1; i >= 0 && len(results) < limit; i-- {
		if s.entries[i].Kind == kind {
			results = append(results, s.entries[i])
		}
	}
	return results, nil
}

func (s *MemStore) Delete(_ context.Context, ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if _, del := idSet[e.ID]; !del {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered
	return nil
}

func containsKind(kinds []Kind, k Kind) bool {
	for _, kk := range kinds {
		if kk == k {
			return true
		}
	}
	return false
}
