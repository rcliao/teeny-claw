package llm

import (
	"context"
	"testing"
)

func TestMockGenerate(t *testing.T) {
	m := &Mock{Response: "hello"}
	got, err := m.Generate(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if len(m.Calls) != 1 || m.Calls[0] != "test prompt" {
		t.Errorf("unexpected calls: %v", m.Calls)
	}
}

func TestMockImplementsClient(t *testing.T) {
	var _ Client = (*Mock)(nil)
}

func TestClaudeCodeImplementsClient(t *testing.T) {
	var _ Client = (*ClaudeCode)(nil)
}
