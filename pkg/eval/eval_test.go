package eval

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TokenEvalBinary != "token-eval" {
		t.Errorf("expected token-eval, got %s", cfg.TokenEvalBinary)
	}
	if cfg.AgentMemoryBinary != "agent-memory" {
		t.Errorf("expected agent-memory, got %s", cfg.AgentMemoryBinary)
	}
	if cfg.LookbackHours != 24 {
		t.Errorf("expected 24, got %d", cfg.LookbackHours)
	}
}

func TestNewClient(t *testing.T) {
	cfg := DefaultConfig()
	c := NewClient(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.cfg.TokenEvalBinary != "token-eval" {
		t.Errorf("config not stored correctly")
	}
}

func TestBuildReviewSummary_NoRecords(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TokenEvalBinary = "nonexistent-binary-xyz"
	c := NewClient(cfg)

	_, err := c.BuildReviewSummary(t.Context())
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestBuildLearningContext_MissingBinary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentMemoryBinary = "nonexistent-binary-xyz"
	c := NewClient(cfg)

	// Should gracefully degrade to empty string
	result, err := c.BuildLearningContext(t.Context(), "test", 5)
	if err != nil {
		t.Errorf("expected graceful degradation, got error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got: %s", result)
	}
}

func TestStoreLearning_MissingBinary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentMemoryBinary = "nonexistent-binary-xyz"
	c := NewClient(cfg)

	err := c.StoreLearning(t.Context(), "test learning", []string{"test"})
	if err == nil {
		t.Error("expected error for missing binary")
	}
}
