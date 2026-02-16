package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicSend(t *testing.T) {
	// Mock Anthropic API server.
	var receivedBody map[string]any
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_123",
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "Hello!"}},
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer server.Close()

	client := NewAnthropic(AnthropicConfig{
		APIKey:       "sk-test-key",
		Auth:         AuthAPIKey,
		BaseURL:      server.URL,
		DefaultModel: "claude-sonnet-4-20250514",
	})

	resp, err := client.Send(context.Background(), &Request{
		System:   "Be helpful.",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Text() != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", resp.Message.Text())
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.Usage.InputTokens)
	}

	// Verify API key auth header.
	if got := receivedHeaders.Get("x-api-key"); got != "sk-test-key" {
		t.Errorf("expected x-api-key 'sk-test-key', got %q", got)
	}
}

func TestAnthropicSendSetupToken(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_456",
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "Hi!"}},
			"usage":       map[string]any{"input_tokens": 20, "output_tokens": 3},
		})
	}))
	defer server.Close()

	token := "sk-ant-oat01-testtoken123"
	client := NewAnthropic(AnthropicConfig{
		APIKey:       token,
		BaseURL:      server.URL,
		DefaultModel: "claude-sonnet-4-20250514",
	})

	resp, err := client.Send(context.Background(), &Request{
		System:   "Custom system prompt.",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Text() != "Hi!" {
		t.Errorf("expected 'Hi!', got %q", resp.Message.Text())
	}

	// Verify Bearer auth.
	if got := receivedHeaders.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("expected Bearer auth, got %q", got)
	}
	// Verify x-api-key is NOT set.
	if got := receivedHeaders.Get("x-api-key"); got != "" {
		t.Errorf("expected no x-api-key for setup-token, got %q", got)
	}
	// Verify Claude Code betas.
	beta := receivedHeaders.Get("anthropic-beta")
	if beta == "" {
		t.Error("expected anthropic-beta header")
	}

	// Verify system prompt includes Claude Code prefix.
	systemRaw, ok := receivedBody["system"]
	if !ok {
		t.Fatal("expected system in request body")
	}
	systemJSON, _ := json.Marshal(systemRaw)
	var systemParts []map[string]string
	if err := json.Unmarshal(systemJSON, &systemParts); err != nil {
		t.Fatalf("parsing system: %v", err)
	}
	if len(systemParts) < 2 {
		t.Fatalf("expected at least 2 system parts, got %d", len(systemParts))
	}
	if systemParts[0]["text"] != claudeCodeSystemPrefix {
		t.Errorf("expected Claude Code prefix, got %q", systemParts[0]["text"])
	}
	if systemParts[1]["text"] != "Custom system prompt." {
		t.Errorf("expected custom system prompt, got %q", systemParts[1]["text"])
	}
}

func TestAnthropicSendToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_789",
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{"type": "text", "text": "Let me check that."},
				{"type": "tool_use", "id": "call_1", "name": "shell", "input": map[string]string{"command": "ls"}},
			},
			"usage": map[string]any{"input_tokens": 15, "output_tokens": 10},
		})
	}))
	defer server.Close()

	client := NewAnthropic(AnthropicConfig{
		APIKey:       "sk-test",
		Auth:         AuthAPIKey,
		BaseURL:      server.URL,
		DefaultModel: "claude-sonnet-4-20250514",
	})

	resp, err := client.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "list files")},
		Tools: []ToolDef{{
			Name:        "shell",
			Description: "Run a shell command",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.HasToolCalls() {
		t.Fatal("expected tool calls")
	}
	if resp.StopReason != StopTool {
		t.Errorf("expected StopTool, got %s", resp.StopReason)
	}

	calls := resp.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ToolName != "shell" {
		t.Errorf("expected tool 'shell', got %q", calls[0].ToolName)
	}
}

func TestAnthropicSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"type": "authentication_error", "message": "invalid key"},
		})
	}))
	defer server.Close()

	client := NewAnthropic(AnthropicConfig{
		APIKey:       "bad-key",
		Auth:         AuthAPIKey,
		BaseURL:      server.URL,
		DefaultModel: "test",
	})

	_, err := client.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", apiErr.StatusCode)
	}
}
