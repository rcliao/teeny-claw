package llm

import (
	"context"
	"testing"
)

func TestMockSend(t *testing.T) {
	m := NewMock("hello")
	resp, err := m.Send(context.Background(), &Request{
		Model:    "test",
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Message.Text(); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if len(m.Requests) != 1 {
		t.Errorf("expected 1 request, got %d", len(m.Requests))
	}
}

func TestMockImplementsClient(t *testing.T) {
	var _ Client = (*Mock)(nil)
}

func TestAnthropicImplementsStreamClient(t *testing.T) {
	var _ StreamClient = (*Anthropic)(nil)
}

func TestTextMessage(t *testing.T) {
	m := TextMessage(RoleUser, "hello")
	if m.Role != RoleUser {
		t.Errorf("expected RoleUser, got %s", m.Role)
	}
	if m.Text() != "hello" {
		t.Errorf("expected 'hello', got %q", m.Text())
	}
}

func TestToolResultMessage(t *testing.T) {
	m := ToolResultMessage("call_1", "result text", false)
	if m.Role != RoleTool {
		t.Errorf("expected RoleTool, got %s", m.Role)
	}
	if len(m.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(m.Content))
	}
	if m.Content[0].ToolCallID != "call_1" {
		t.Errorf("expected tool call ID 'call_1', got %q", m.Content[0].ToolCallID)
	}
}

func TestResponseHasToolCalls(t *testing.T) {
	resp := &Response{
		Message: Message{
			Role: RoleAssistant,
			Content: []ContentPart{
				{Type: ContentText, Text: "thinking..."},
				{Type: ContentToolCall, ToolCallID: "1", ToolName: "shell"},
			},
		},
	}
	if !resp.HasToolCalls() {
		t.Error("expected HasToolCalls to be true")
	}

	resp2 := &Response{
		Message: TextMessage(RoleAssistant, "just text"),
	}
	if resp2.HasToolCalls() {
		t.Error("expected HasToolCalls to be false")
	}
}

func TestGenerateHelper(t *testing.T) {
	m := NewMock("world")
	got, err := Generate(context.Background(), m, "test-model", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "world" {
		t.Errorf("expected 'world', got %q", got)
	}
	if m.Requests[0].Model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", m.Requests[0].Model)
	}
}

func TestDetectAuthMode(t *testing.T) {
	tests := []struct {
		key  string
		want AuthMode
	}{
		{"sk-ant-oat01-abc123", AuthSetupToken},
		{"sk-ant-api03-abc123", AuthAPIKey},
		{"some-random-key", AuthAPIKey},
	}
	for _, tt := range tests {
		got := detectAuthMode(tt.key)
		if got != tt.want {
			t.Errorf("detectAuthMode(%q) = %s, want %s", tt.key, got, tt.want)
		}
	}
}

func TestUsageTotalTokens(t *testing.T) {
	u := Usage{InputTokens: 100, OutputTokens: 50, CacheRead: 10, CacheWrite: 5}
	if u.TotalTokens() != 165 {
		t.Errorf("expected 165, got %d", u.TotalTokens())
	}
}
