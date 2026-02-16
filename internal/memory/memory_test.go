package memory

import (
	"context"
	"testing"
)

func TestManagerRememberAndRecall(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	mgr := NewManager(store)

	if err := mgr.Remember(ctx, "Go tests should be table-driven", KindLesson, "testing"); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if err := mgr.Remember(ctx, "Use context.Context for cancellation", KindLesson, "go"); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if err := mgr.Remember(ctx, "Deployed v1.2 to production", KindEpisodic); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// Recall by keyword.
	results, err := mgr.Recall(ctx, "table-driven", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Kind != KindLesson {
		t.Errorf("expected KindLesson, got %s", results[0].Kind)
	}
}

func TestManagerLessons(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	mgr := NewManager(store)

	mgr.Remember(ctx, "lesson 1", KindLesson)
	mgr.Remember(ctx, "episode 1", KindEpisodic)
	mgr.Remember(ctx, "lesson 2", KindLesson)

	lessons, err := mgr.Lessons(ctx, 10)
	if err != nil {
		t.Fatalf("Lessons: %v", err)
	}
	if len(lessons) != 2 {
		t.Fatalf("expected 2 lessons, got %d", len(lessons))
	}
	// Newest first.
	if lessons[0].Content != "lesson 2" {
		t.Errorf("expected newest first, got %q", lessons[0].Content)
	}
}

func TestManagerRecentEpisodes(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	mgr := NewManager(store)

	mgr.Remember(ctx, "lesson", KindLesson)
	mgr.Remember(ctx, "ep1", KindEpisodic)
	mgr.Remember(ctx, "ep2", KindEpisodic)

	eps, err := mgr.RecentEpisodes(ctx, 1)
	if err != nil {
		t.Fatalf("RecentEpisodes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}
	if eps[0].Content != "ep2" {
		t.Errorf("expected ep2, got %q", eps[0].Content)
	}
}

func TestMemStoreDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	mgr := NewManager(store)

	mgr.Remember(ctx, "to delete", KindSemantic)
	entries, _ := mgr.Recall(ctx, "delete", 10)
	if len(entries) == 0 {
		t.Fatal("expected entry before delete")
	}

	store.Delete(ctx, entries[0].ID)

	entries, _ = mgr.Recall(ctx, "delete", 10)
	if len(entries) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(entries))
	}
}
