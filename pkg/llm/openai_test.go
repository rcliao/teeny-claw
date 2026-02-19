package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOpenAI_Defaults(t *testing.T) {
	o := NewOpenAI("test-key", "")
	if o.model != "gpt-4o" {
		t.Errorf("default model = %q, want gpt-4o", o.model)
	}
	if o.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want test-key", o.apiKey)
	}
	if o.baseURL != openaiDefaultURL {
		t.Errorf("baseURL = %q, want %q", o.baseURL, openaiDefaultURL)
	}
}

func TestNewOpenAI_CustomBaseURL(t *testing.T) {
	o := NewOpenAI("key", "model", WithBaseURL("http://localhost:11434/v1/chat/completions"))
	if o.baseURL != "http://localhost:11434/v1/chat/completions" {
		t.Errorf("baseURL = %q", o.baseURL)
	}
}

func TestOpenAI_NoAPIKey(t *testing.T) {
	o := NewOpenAI("", "model")
	o.apiKey = ""
	_, err := o.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestOpenAI_Send_SimpleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("bad auth header: %q", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)

		if req.Model != "gpt-4o" {
			t.Errorf("model = %q", req.Model)
		}
		// Should have system + user = 2 messages
		if len(req.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(req.Messages))
		}

		resp := openaiResponse{
			Choices: []struct {
				Message struct {
					Content   string           `json:"content"`
					ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
			}{
				{Message: struct {
					Content   string           `json:"content"`
					ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
				}{Content: "Hello!"}},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}{80, 15},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	o := NewOpenAI("test-key", "", WithBaseURL(server.URL))
	resp, err := o.Send(context.Background(), &Request{
		System:   "You are helpful.",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if resp.Message.Text() != "Hello!" {
		t.Errorf("content = %q", resp.Message.Text())
	}
	if resp.Usage.InputTokens != 80 || resp.Usage.OutputTokens != 15 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestOpenAI_Send_ToolCallResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"choices": [{
				"message": {
					"content": "",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "search", "arguments": "{\"q\":\"test\"}"}
					}]
				}
			}],
			"usage": {"prompt_tokens": 50, "completion_tokens": 20}
		}`
		w.Write([]byte(resp))
	}))
	defer server.Close()

	o := NewOpenAI("test-key", "gpt-4o", WithBaseURL(server.URL))
	resp, err := o.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "search for test")},
		Tools: []ToolDef{{
			Name:        "search",
			Description: "Search",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	calls := resp.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ToolName != "search" {
		t.Errorf("tool name = %q", calls[0].ToolName)
	}
	if calls[0].ToolCallID != "call_1" {
		t.Errorf("tool id = %q", calls[0].ToolCallID)
	}
}

func TestOpenAI_Send_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":0}}`))
	}))
	defer server.Close()

	o := NewOpenAI("test-key", "gpt-4o", WithBaseURL(server.URL))
	resp, err := o.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if resp.Message.Text() != "" {
		t.Errorf("expected empty content, got %q", resp.Message.Text())
	}
}

func TestOpenAI_Send_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"type":"rate_limit","message":"slow down"}}`))
	}))
	defer server.Close()

	o := NewOpenAI("test-key", "gpt-4o", WithBaseURL(server.URL))
	_, err := o.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
