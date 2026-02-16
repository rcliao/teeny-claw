// Package memory implements the three-layer memory system:
// working (session), episodic (logs), and semantic (indexed knowledge).
package memory

import (
	"context"
	"time"
)

// Entry represents a single memory entry.
type Entry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Kind      Kind      `json:"kind"`
	Tags      []string  `json:"tags,omitempty"`
	Score     float64   `json:"score,omitempty"`     // relevance score from search
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Kind classifies memory entries.
type Kind string

const (
	KindWorking  Kind = "working"  // current session context
	KindEpisodic Kind = "episodic" // session logs with outcomes
	KindSemantic Kind = "semantic" // distilled knowledge
	KindLesson   Kind = "lesson"   // self-improvement lessons
	KindStrategy Kind = "strategy" // evolved strategies
)

// Store is the interface for memory persistence.
type Store interface {
	// Save persists one or more entries.
	Save(ctx context.Context, entries ...Entry) error

	// Search finds entries matching a query, ordered by relevance.
	Search(ctx context.Context, query string, opts SearchOpts) ([]Entry, error)

	// List returns entries filtered by kind, ordered by time (newest first).
	List(ctx context.Context, kind Kind, limit int) ([]Entry, error)

	// Delete removes entries by ID.
	Delete(ctx context.Context, ids ...string) error
}

// SearchOpts controls search behavior.
type SearchOpts struct {
	// Kinds filters results to specific memory kinds.
	Kinds []Kind
	// Limit caps the number of results.
	Limit int
	// MinScore filters out low-relevance results.
	MinScore float64
}

// Manager orchestrates the memory layers and provides high-level operations.
type Manager struct {
	store Store
}

// NewManager creates a Manager backed by the given store.
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// Remember saves content as the given kind.
func (m *Manager) Remember(ctx context.Context, content string, kind Kind, tags ...string) error {
	now := time.Now()
	entry := Entry{
		ID:        generateID(),
		Content:   content,
		Kind:      kind,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return m.store.Save(ctx, entry)
}

// Recall searches memory for entries matching the query.
func (m *Manager) Recall(ctx context.Context, query string, limit int) ([]Entry, error) {
	return m.store.Search(ctx, query, SearchOpts{Limit: limit})
}

// RecentEpisodes returns the most recent episodic entries.
func (m *Manager) RecentEpisodes(ctx context.Context, limit int) ([]Entry, error) {
	return m.store.List(ctx, KindEpisodic, limit)
}

// Lessons returns stored lessons learned.
func (m *Manager) Lessons(ctx context.Context, limit int) ([]Entry, error) {
	return m.store.List(ctx, KindLesson, limit)
}

// generateID creates a simple time-based ID.
func generateID() string {
	return time.Now().Format("20060102-150405.000")
}
