package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Agent.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", cfg.Agent.MaxRetries)
	}
	if !cfg.Agent.ReflectionEnabled {
		t.Error("expected ReflectionEnabled=true")
	}
	if cfg.Memory.MaxWorkingTokens != 8000 {
		t.Errorf("expected MaxWorkingTokens=8000, got %d", cfg.Memory.MaxWorkingTokens)
	}
	if cfg.Process.MaxConcurrent != 4 {
		t.Errorf("expected MaxConcurrent=4, got %d", cfg.Process.MaxConcurrent)
	}
}

func TestLoadEmpty(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Agent.MaxRetries != 3 {
		t.Error("empty path should return defaults")
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Agent.MaxRetries != 3 {
		t.Error("missing file should return defaults")
	}
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"agent":{"max_retries":5},"memory":{"dir":"custom"}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Agent.MaxRetries != 5 {
		t.Errorf("expected MaxRetries=5, got %d", cfg.Agent.MaxRetries)
	}
	if cfg.Memory.Dir != "custom" {
		t.Errorf("expected Dir=custom, got %s", cfg.Memory.Dir)
	}
	// Unset fields keep defaults since we unmarshal into a pre-filled struct.
	if cfg.Process.DefaultTimeout != 5*time.Minute {
		t.Errorf("expected default 5m timeout for unset field, got %v", cfg.Process.DefaultTimeout)
	}
}

func TestLoadInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDurations(t *testing.T) {
	cfg := Default()
	if cfg.Process.DefaultTimeout != 5*time.Minute {
		t.Errorf("expected 5m timeout, got %v", cfg.Process.DefaultTimeout)
	}
	if cfg.Scheduler.HeartbeatInterval != 30*time.Minute {
		t.Errorf("expected 30m heartbeat, got %v", cfg.Scheduler.HeartbeatInterval)
	}
}
