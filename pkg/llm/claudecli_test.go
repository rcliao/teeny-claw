package llm

import (
	"context"
	"strings"
	"testing"
)

func TestClaudeCLI_MissingBinary(t *testing.T) {
	p := NewClaudeCLI("sonnet", WithClaudeBinary("definitely-not-a-real-binary-xyz"))
	_, err := p.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildClaudePrompt(t *testing.T) {
	req := &Request{
		System: "rules",
		Messages: []Message{
			TextMessage(RoleUser, "hi"),
			TextMessage(RoleAssistant, "hello"),
			ToolResultMessage("tc1", "tool output", false),
		},
	}
	prompt := buildClaudePrompt(req)
	checks := []string{"[SYSTEM]", "rules", "[USER]", "[ASSISTANT]", "[TOOL_RESULT id=tc1]"}
	for _, c := range checks {
		if !strings.Contains(prompt, c) {
			t.Fatalf("prompt missing %q: %s", c, prompt)
		}
	}
}

func TestParseClaudeJSONOutput_WithToolCalls(t *testing.T) {
	out := `{"content":"I'll use a tool","tool_calls":[{"name":"todo-mgmt.add","arguments":{"text":"Write tests"}}]}`
	resp, err := parseClaudeJSONOutput(out)
	if err != nil {
		t.Fatalf("parseClaudeJSONOutput error: %v", err)
	}
	if resp.Message.Text() == "" {
		t.Fatal("expected content")
	}
	calls := resp.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ToolName != "todo-mgmt.add" {
		t.Fatalf("unexpected tool name: %s", calls[0].ToolName)
	}
	if !strings.Contains(string(calls[0].ToolInput), "Write tests") {
		t.Fatalf("unexpected arguments: %s", string(calls[0].ToolInput))
	}
}

func TestParseClaudeJSONOutput_StructuredOutputWrapper(t *testing.T) {
	out := `{"type":"result","result":"","structured_output":{"content":"Wrapped content","tool_calls":[]}}`
	resp, err := parseClaudeJSONOutput(out)
	if err != nil {
		t.Fatalf("parseClaudeJSONOutput error: %v", err)
	}
	if resp.Message.Text() != "Wrapped content" {
		t.Fatalf("expected wrapped content, got %q", resp.Message.Text())
	}
}

func TestParseClaudeJSONOutput_ResultFallback(t *testing.T) {
	out := `{"type":"result","result":"Plain result text"}`
	resp, err := parseClaudeJSONOutput(out)
	if err != nil {
		t.Fatalf("parseClaudeJSONOutput error: %v", err)
	}
	if resp.Message.Text() != "Plain result text" {
		t.Fatalf("expected result fallback, got %q", resp.Message.Text())
	}
}

func TestBuildToolAwarePrompt(t *testing.T) {
	base := "[USER]\nDo thing"
	prompt := buildToolAwarePrompt(base, []ToolDef{{
		Name:        "todo-mgmt.add",
		Description: "Add todo",
		Parameters:  map[string]any{"type": "object"},
	}})
	checks := []string{"[AVAILABLE_TOOLS]", "todo-mgmt.add", "Add todo", "params"}
	for _, c := range checks {
		if !strings.Contains(prompt, c) {
			t.Fatalf("tool-aware prompt missing %q: %s", c, prompt)
		}
	}
}

func TestClaudeCLI_VerboseErrorIncludesCommandContext(t *testing.T) {
	p := NewClaudeCLI("sonnet", WithClaudeBinary("sh"), WithClaudeVerbose(true))
	_, err := p.Send(context.Background(), &Request{
		Messages: []Message{TextMessage(RoleUser, "hi")},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "binary=") || !strings.Contains(msg, "args=") {
		t.Fatalf("expected verbose diagnostics in error, got: %v", err)
	}
}
