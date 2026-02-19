package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"@every 30m", 30 * time.Minute, false},
		{"@every 1h", time.Hour, false},
		{"@every 5s", 5 * time.Second, false},
		{"0 * * * *", 0, true},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		got, err := parseInterval(tt.input)
		if tt.err && err == nil {
			t.Errorf("parseInterval(%q) expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("parseInterval(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseInterval(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestShouldRun(t *testing.T) {
	now := time.Now()

	if !shouldRun("@every 30m", now.Add(-31*time.Minute), now) {
		t.Error("expected shouldRun=true after 31m with 30m interval")
	}

	if shouldRun("@every 30m", now.Add(-10*time.Minute), now) {
		t.Error("expected shouldRun=false after 10m with 30m interval")
	}
}

func TestSchedulerStartStop(t *testing.T) {
	s := New(nil, nil, false)
	ctx := context.Background()

	s.Start(ctx)
	if !s.Running() {
		t.Error("expected Running()=true after Start")
	}

	s.Stop()
	time.Sleep(50 * time.Millisecond)
	if s.Running() {
		t.Error("expected Running()=false after Stop")
	}
}

func TestSchedulerRunsJob(t *testing.T) {
	var mu sync.Mutex
	var calls []string

	runFn := func(ctx context.Context, session, prompt string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, session+":"+prompt)
		return "ok", nil
	}

	jobs := []Job{
		{Name: "test", Schedule: "@every 1s", Prompt: "do stuff", Session: "test-session", Enabled: true},
	}

	s := New(jobs, runFn, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	time.Sleep(2 * time.Second)
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Error("expected at least one job execution")
	}
	if calls[0] != "test-session:do stuff" {
		t.Errorf("unexpected call: %s", calls[0])
	}
}

func TestSchedulerSkipsDisabledJob(t *testing.T) {
	var mu sync.Mutex
	var calls int

	runFn := func(ctx context.Context, session, prompt string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return "ok", nil
	}

	jobs := []Job{
		{Name: "disabled", Schedule: "@every 1s", Prompt: "nope", Session: "s", Enabled: false},
	}

	s := New(jobs, runFn, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	time.Sleep(2 * time.Second)
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 calls for disabled job, got %d", calls)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short: %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long: %q", got)
	}
}
