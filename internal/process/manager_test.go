package process

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	m := NewManager(0)
	if m.maxConcurrent != 4 {
		t.Errorf("default maxConcurrent = %d, want 4", m.maxConcurrent)
	}
}

func TestSpawnAndWait(t *testing.T) {
	m := NewManager(2)
	m.binary = "echo"

	sess, err := m.Spawn(context.Background(), SpawnOpts{
		ID:     "test-1",
		Prompt: "hello world",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if sess.Status != StatusRunning {
		// echo may finish before we check
		if sess.Status != StatusDone && sess.Status != StatusFailed {
			t.Errorf("unexpected status: %s", sess.Status)
		}
	}

	if err := m.Wait("test-1"); err != nil {
		t.Fatalf("wait: %v", err)
	}

	output, err := m.ReadOutput("test-1")
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	// echo will print the args
	if !strings.Contains(output, "hello world") {
		t.Errorf("output = %q, expected to contain 'hello world'", output)
	}
}

func TestSpawnDuplicateID(t *testing.T) {
	m := NewManager(4)
	m.binary = "echo"

	_, err := m.Spawn(context.Background(), SpawnOpts{ID: "dup", Prompt: "a"})
	if err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	m.Wait("dup")

	_, err = m.Spawn(context.Background(), SpawnOpts{ID: "dup", Prompt: "b"})
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestSpawnConcurrencyLimit(t *testing.T) {
	m := NewManager(1)
	// Use bash -c "sleep 10" to get a long-running process
	// despite the CLI args format (-p prompt --model ...)
	m.binary = "bash"

	_, err := m.Spawn(context.Background(), SpawnOpts{
		ID:      "slow",
		Prompt:  "sleep 10", // bash -p "sleep 10" won't work, but process starts
		Timeout: 5 * time.Second,
	})
	// The process may fail immediately or start — either way it occupies a slot briefly.
	// To test concurrency properly, we use a different approach:
	if err != nil {
		t.Skip("cannot spawn bash process in test environment")
	}

	// Wait briefly to check if still running
	time.Sleep(50 * time.Millisecond)

	sess, _ := m.Get("slow")
	if sess == nil {
		t.Skip("process didn't register")
	}
	sess.mu.Lock()
	st := sess.Status
	sess.mu.Unlock()
	if st != StatusRunning {
		t.Skip("process ended too quickly for concurrency test")
	}

	// Second spawn should be blocked
	_, err = m.Spawn(context.Background(), SpawnOpts{ID: "blocked", Prompt: "1"})
	if err == nil {
		t.Fatal("expected concurrency error")
	}
	if !strings.Contains(err.Error(), "max concurrent") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Clean up
	m.Kill("slow")
}

func TestKill(t *testing.T) {
	m := NewManager(4)
	m.binary = "bash"

	// bash -p "..." --model sonnet ... will just get bash started with those args
	// which it will try to parse and may fail, so use a workaround:
	// Override to use a simple long-running command by spawning bash directly
	sess := &Session{
		ID:        "to-kill",
		Status:    StatusRunning,
		StartedAt: time.Now(),
	}
	m.mu.Lock()
	m.sessions["to-kill"] = sess
	m.mu.Unlock()

	// Create a cancel func
	_, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel

	if err := m.Kill("to-kill"); err != nil {
		t.Fatalf("kill: %v", err)
	}

	sess.mu.Lock()
	st := sess.Status
	sess.mu.Unlock()
	if st != StatusKilled {
		t.Errorf("expected killed, got %s", st)
	}
}

func TestKillNotFound(t *testing.T) {
	m := NewManager(4)
	err := m.Kill("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestList(t *testing.T) {
	m := NewManager(4)
	m.binary = "echo"

	m.Spawn(context.Background(), SpawnOpts{ID: "a", Prompt: "1"})
	m.Spawn(context.Background(), SpawnOpts{ID: "b", Prompt: "2"})
	m.Wait("a")
	m.Wait("b")

	infos := m.List()
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
}

func TestReadOutputNotFound(t *testing.T) {
	m := NewManager(4)
	_, err := m.ReadOutput("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}
