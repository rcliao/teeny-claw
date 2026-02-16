// Package llm provides the LLM client abstraction layer.
//
// The design separates concerns into three interfaces:
//   - Client: the main interface for sending messages and getting responses
//   - StreamClient: optional streaming support
//   - ToolProvider: defines tools the model can call
//
// Implementations handle provider-specific details (auth, headers, tool format)
// while the agent layer works only with these abstractions.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
)

// ---------- Core types ----------

// Role identifies who sent a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // tool result fed back to the model
)

// Message is a single turn in a conversation.
type Message struct {
	Role    Role          `json:"role"`
	Content []ContentPart `json:"content"`
}

// TextMessage is a convenience constructor.
func TextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentPart{{Type: ContentText, Text: text}}}
}

// ToolResultMessage creates a tool-result message.
func ToolResultMessage(toolCallID, content string, isError bool) Message {
	return Message{
		Role: RoleTool,
		Content: []ContentPart{{
			Type:       ContentToolResult,
			ToolCallID: toolCallID,
			Text:       content,
			IsError:    isError,
		}},
	}
}

// Text returns the concatenated text of all text parts (convenience).
func (m Message) Text() string {
	var s string
	for _, p := range m.Content {
		if p.Type == ContentText {
			s += p.Text
		}
	}
	return s
}

// ToolCalls returns all tool-call parts (convenience).
func (m Message) ToolCalls() []ContentPart {
	var calls []ContentPart
	for _, p := range m.Content {
		if p.Type == ContentToolCall {
			calls = append(calls, p)
		}
	}
	return calls
}

// ---------- Content parts ----------

// ContentType discriminates content block types.
type ContentType string

const (
	ContentText       ContentType = "text"
	ContentToolCall   ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
	ContentThinking   ContentType = "thinking"
)

// ContentPart is one block inside a Message.
// Which fields are populated depends on Type.
type ContentPart struct {
	Type ContentType `json:"type"`

	// Text content (ContentText, ContentThinking, ContentToolResult).
	Text string `json:"text,omitempty"`

	// Tool call fields (ContentToolCall).
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	// Tool result fields (ContentToolResult).
	IsError bool `json:"is_error,omitempty"`
}

// ---------- Tool definitions ----------

// ToolDef describes a tool the model can invoke.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema object
}

// ---------- Request / Response ----------

// Request is everything needed to make one LLM call.
type Request struct {
	Model    string    // model ID (e.g. "claude-sonnet-4-20250514")
	System   string    // system prompt
	Messages []Message // conversation history
	Tools    []ToolDef // available tools (may be nil)

	MaxTokens   int     // 0 = provider default
	Temperature float64 // 0 = provider default
	TopP        float64 // 0 = provider default

	// Thinking controls extended thinking / reasoning.
	// Empty string = off, "low"/"medium"/"high" = effort levels.
	Thinking string
}

// Response is the model's reply.
type Response struct {
	Message Message `json:"message"` // the assistant message

	// Usage stats.
	Usage Usage `json:"usage"`

	// StopReason explains why generation stopped.
	StopReason StopReason `json:"stop_reason"`

	// Model actually used (may differ from request if aliased).
	Model string `json:"model,omitempty"`
}

// StopReason enumerates why the model stopped.
type StopReason string

const (
	StopEnd     StopReason = "stop"     // natural end
	StopLength  StopReason = "length"   // hit max tokens
	StopTool    StopReason = "tool_use" // wants to call a tool
	StopError   StopReason = "error"
	StopAborted StopReason = "aborted"
)

// HasToolCalls returns true if the response contains tool calls.
func (r Response) HasToolCalls() bool {
	for _, p := range r.Message.Content {
		if p.Type == ContentToolCall {
			return true
		}
	}
	return false
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheRead    int `json:"cache_read,omitempty"`
	CacheWrite   int `json:"cache_write,omitempty"`
}

func (u Usage) TotalTokens() int {
	return u.InputTokens + u.OutputTokens + u.CacheRead + u.CacheWrite
}

// ---------- Streaming ----------

// StreamEvent is one chunk during streaming.
type StreamEvent struct {
	Type StreamEventType

	// Text delta (for EventTextDelta / EventThinkingDelta).
	Delta string

	// Full partial message so far (for EventDone).
	Response *Response

	// Error (for EventError).
	Err error
}

// StreamEventType discriminates stream events.
type StreamEventType string

const (
	EventTextDelta     StreamEventType = "text_delta"
	EventThinkingDelta StreamEventType = "thinking_delta"
	EventToolCallStart StreamEventType = "tool_call_start"
	EventToolCallDelta StreamEventType = "tool_call_delta"
	EventToolCallEnd   StreamEventType = "tool_call_end"
	EventDone          StreamEventType = "done"
	EventError         StreamEventType = "error"
)

// ---------- Interfaces ----------

// Client is the core LLM interface. Every provider must implement this.
type Client interface {
	// Send makes a single (non-streaming) request.
	Send(ctx context.Context, req *Request) (*Response, error)
}

// StreamClient adds streaming support. Optional — not all providers need it.
type StreamClient interface {
	Client
	// Stream returns an iterator of events. The final event is always EventDone or EventError.
	Stream(ctx context.Context, req *Request) iter.Seq[StreamEvent]
}

// ---------- Helpers ----------

// Generate is a convenience: send a single user message, get text back.
func Generate(ctx context.Context, c Client, model, prompt string) (string, error) {
	resp, err := c.Send(ctx, &Request{
		Model:    model,
		Messages: []Message{TextMessage(RoleUser, prompt)},
	})
	if err != nil {
		return "", err
	}
	return resp.Message.Text(), nil
}

// GenerateWithSystem is a convenience: single user message + system prompt.
func GenerateWithSystem(ctx context.Context, c Client, model, system, prompt string) (string, error) {
	resp, err := c.Send(ctx, &Request{
		Model:    model,
		System:   system,
		Messages: []Message{TextMessage(RoleUser, prompt)},
	})
	if err != nil {
		return "", err
	}
	return resp.Message.Text(), nil
}

// Chat sends a full conversation and returns the response.
func Chat(ctx context.Context, c Client, req *Request) (*Response, error) {
	return c.Send(ctx, req)
}

// ---------- Errors ----------

// APIError represents a provider API error with status code.
type APIError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm api error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}
