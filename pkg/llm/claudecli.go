package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ClaudeCLI uses the official claude CLI as an LLM backend.
// This is a text-in/text-out provider that uses JSON schema output
// to get structured responses including tool calls.
type ClaudeCLI struct {
	model   string
	binary  string
	timeout time.Duration
	verbose bool
}

// ClaudeCLIOption configures ClaudeCLI.
type ClaudeCLIOption func(*ClaudeCLI)

// WithClaudeBinary overrides the CLI binary path/name (default: "claude").
func WithClaudeBinary(bin string) ClaudeCLIOption {
	return func(c *ClaudeCLI) { c.binary = bin }
}

// WithClaudeVerbose enables richer error diagnostics.
func WithClaudeVerbose(v bool) ClaudeCLIOption {
	return func(c *ClaudeCLI) { c.verbose = v }
}

// NewClaudeCLI creates a Claude CLI-backed LLM client.
func NewClaudeCLI(model string, opts ...ClaudeCLIOption) *ClaudeCLI {
	if model == "" {
		model = "sonnet"
	}
	c := &ClaudeCLI{model: model, binary: "claude", timeout: 2 * time.Minute}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Send implements llm.Client by invoking the claude CLI.
func (c *ClaudeCLI) Send(ctx context.Context, req *Request) (*Response, error) {
	if _, err := exec.LookPath(c.binary); err != nil {
		return nil, fmt.Errorf("claude-cli: %q not found in PATH", c.binary)
	}

	prompt := buildClaudePrompt(req)
	if len(req.Tools) > 0 {
		prompt = buildToolAwarePrompt(prompt, req.Tools)
	}

	// Use structured JSON output so we can map back to llm.Response.
	schema := `{"type":"object","properties":{"content":{"type":"string"},"tool_calls":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"arguments":{"type":"object"}},"required":["name","arguments"],"additionalProperties":false}}},"required":["content","tool_calls"],"additionalProperties":false}`
	args := []string{
		"-p", prompt,
		"--model", c.model,
		"--output-format", "json",
		"--json-schema", schema,
		"--tools", "", // disable Claude built-in tools; we handle tools externally
	}

	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, c.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		if c.verbose {
			out := strings.TrimSpace(stdout.String())
			if len(out) > 1200 {
				out = out[:1200] + "..."
			}
			return nil, fmt.Errorf("claude-cli: %s (binary=%q args=%q stdout=%q)", errMsg, c.binary, strings.Join(args, " "), out)
		}
		return nil, fmt.Errorf("claude-cli: %s", errMsg)
	}

	resp, err := parseClaudeJSONOutput(stdout.String())
	if err != nil {
		// Fallback: treat raw output as plain text if JSON parsing fails.
		return &Response{
			Message: TextMessage(RoleAssistant, strings.TrimSpace(stdout.String())),
		}, nil
	}
	return resp, nil
}

type claudeToolCall struct {
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type claudeJSONResponse struct {
	Content          string           `json:"content"`
	ToolCalls        []claudeToolCall `json:"tool_calls"`
	Result           string           `json:"result,omitempty"`
	StructuredOutput *struct {
		Content   string           `json:"content"`
		ToolCalls []claudeToolCall `json:"tool_calls"`
	} `json:"structured_output,omitempty"`
}

func parseClaudeJSONOutput(out string) (*Response, error) {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return &Response{Message: TextMessage(RoleAssistant, "")}, nil
	}
	var parsed claudeJSONResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, err
	}

	content := parsed.Content
	toolCalls := parsed.ToolCalls

	// Claude CLI --output-format json often wraps schema output under structured_output.
	if parsed.StructuredOutput != nil {
		if parsed.StructuredOutput.Content != "" || len(parsed.StructuredOutput.ToolCalls) > 0 {
			content = parsed.StructuredOutput.Content
			toolCalls = parsed.StructuredOutput.ToolCalls
		}
	}
	// Fallback for non-schema json mode where text is in result.
	if content == "" && len(toolCalls) == 0 && parsed.Result != "" {
		content = parsed.Result
	}

	var parts []ContentPart
	if content != "" {
		parts = append(parts, ContentPart{Type: ContentText, Text: content})
	}
	for i, tc := range toolCalls {
		args, _ := json.Marshal(tc.Arguments)
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("claudecli_tool_%d", i+1)
		}
		parts = append(parts, ContentPart{
			Type:       ContentToolCall,
			ToolCallID: id,
			ToolName:   tc.Name,
			ToolInput:  args,
		})
	}

	stopReason := StopEnd
	if len(toolCalls) > 0 {
		stopReason = StopTool
	}

	return &Response{
		Message:    Message{Role: RoleAssistant, Content: parts},
		StopReason: stopReason,
	}, nil
}

func buildToolAwarePrompt(base string, tools []ToolDef) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n[AVAILABLE_TOOLS]\n")
	for _, t := range tools {
		params, _ := json.Marshal(t.Parameters)
		b.WriteString("- ")
		b.WriteString(t.Name)
		b.WriteString(": ")
		b.WriteString(t.Description)
		b.WriteString("\n  params: ")
		b.WriteString(string(params))
		b.WriteString("\n")
	}
	b.WriteString("\nWhen tool use is needed, return tool_calls with precise JSON arguments matching the tool schema.\n")
	return b.String()
}

func buildClaudePrompt(req *Request) string {
	var b strings.Builder

	if req.System != "" {
		b.WriteString("[SYSTEM]\n")
		b.WriteString(req.System)
		b.WriteString("\n\n")
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			b.WriteString("[SYSTEM]\n")
			b.WriteString(m.Text())
			b.WriteString("\n\n")
		case RoleUser:
			b.WriteString("[USER]\n")
			b.WriteString(m.Text())
			b.WriteString("\n\n")
		case RoleAssistant:
			b.WriteString("[ASSISTANT]\n")
			b.WriteString(m.Text())
			b.WriteString("\n\n")
		case RoleTool:
			b.WriteString("[TOOL_RESULT")
			for _, p := range m.Content {
				if p.Type == ContentToolResult && p.ToolCallID != "" {
					b.WriteString(" id=")
					b.WriteString(p.ToolCallID)
					break
				}
			}
			b.WriteString("]\n")
			b.WriteString(m.Text())
			b.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}
