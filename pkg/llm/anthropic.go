package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultAnthropicVersion = "2023-06-01"
	defaultMaxTokens        = 8192

	// Claude Code identity required for OAuth/setup-token auth.
	claudeCodeSystemPrefix = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeVersion      = "2.1.42"
)

// AuthMode determines how to authenticate with Anthropic.
type AuthMode string

const (
	AuthAPIKey     AuthMode = "api_key"      // x-api-key header
	AuthSetupToken AuthMode = "setup_token"  // Bearer token (OAuth)
)

// AnthropicConfig configures the Anthropic client.
type AnthropicConfig struct {
	// APIKey or setup-token value. Required.
	APIKey string

	// Auth determines the auth mode. If empty, auto-detected from key prefix.
	Auth AuthMode

	// BaseURL overrides the API base URL. Default: https://api.anthropic.com
	BaseURL string

	// DefaultModel used when Request.Model is empty.
	DefaultModel string

	// HTTPClient overrides the default HTTP client. Useful for testing.
	HTTPClient *http.Client
}

// detectAuthMode infers auth mode from the key format.
func detectAuthMode(key string) AuthMode {
	if strings.Contains(key, "sk-ant-oat") {
		return AuthSetupToken
	}
	return AuthAPIKey
}

// Anthropic implements Client and StreamClient for the Anthropic Messages API.
type Anthropic struct {
	cfg        AnthropicConfig
	httpClient *http.Client
	authMode   AuthMode
	baseURL    string
}

// NewAnthropic creates a new Anthropic client.
func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	c := &Anthropic{cfg: cfg}

	if cfg.HTTPClient != nil {
		c.httpClient = cfg.HTTPClient
	} else {
		c.httpClient = http.DefaultClient
	}

	if cfg.BaseURL != "" {
		c.baseURL = strings.TrimRight(cfg.BaseURL, "/")
	} else {
		c.baseURL = defaultAnthropicBaseURL
	}

	if cfg.Auth != "" {
		c.authMode = cfg.Auth
	} else {
		c.authMode = detectAuthMode(cfg.APIKey)
	}

	return c
}

// ---------- Interface compliance ----------

var (
	_ Client       = (*Anthropic)(nil)
	_ StreamClient = (*Anthropic)(nil)
)

// ---------- Request building ----------

// anthropicRequest is the wire format for /v1/messages.
type anthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	Stream    bool              `json:"stream,omitempty"`
	System    json.RawMessage   `json:"system,omitempty"`
	Messages  []anthropicMsg    `json:"messages"`
	Tools     []anthropicTool   `json:"tools,omitempty"`
	Thinking  *anthropicThink   `json:"thinking,omitempty"`
	Metadata  *anthropicMeta    `json:"metadata,omitempty"`

	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
}

type anthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicThink struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicMeta struct {
	UserID string `json:"user_id,omitempty"`
}

func (a *Anthropic) buildRequest(req *Request, stream bool) (*anthropicRequest, error) {
	model := req.Model
	if model == "" {
		model = a.cfg.DefaultModel
	}
	if model == "" {
		return nil, fmt.Errorf("no model specified and no default configured")
	}

	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = defaultMaxTokens
	}

	ar := &anthropicRequest{
		Model:     model,
		MaxTokens: maxTok,
		Stream:    stream,
	}

	// System prompt.
	ar.System = a.buildSystem(req.System)

	// Messages.
	msgs, err := a.buildMessages(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("building messages: %w", err)
	}
	ar.Messages = msgs

	// Tools.
	if len(req.Tools) > 0 {
		ar.Tools = make([]anthropicTool, len(req.Tools))
		for i, t := range req.Tools {
			ar.Tools[i] = anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			}
		}
	}

	// Temperature / TopP.
	if req.Temperature > 0 {
		t := req.Temperature
		ar.Temperature = &t
	}
	if req.TopP > 0 {
		p := req.TopP
		ar.TopP = &p
	}

	// Thinking.
	if req.Thinking != "" {
		ar.Thinking = &anthropicThink{
			Type:         "enabled",
			BudgetTokens: thinkingBudget(req.Thinking, maxTok),
		}
	}

	return ar, nil
}

func thinkingBudget(level string, maxTokens int) int {
	switch strings.ToLower(level) {
	case "low":
		return max(1024, maxTokens/4)
	case "medium":
		return max(2048, maxTokens/2)
	case "high":
		return max(4096, maxTokens*3/4)
	default:
		return max(2048, maxTokens/2)
	}
}

func (a *Anthropic) buildSystem(system string) json.RawMessage {
	var parts []map[string]string

	// OAuth auth requires Claude Code identity prefix.
	if a.authMode == AuthSetupToken {
		parts = append(parts, map[string]string{
			"type": "text",
			"text": claudeCodeSystemPrefix,
		})
	}

	if system != "" {
		parts = append(parts, map[string]string{
			"type": "text",
			"text": system,
		})
	}

	if len(parts) == 0 {
		return nil
	}

	data, _ := json.Marshal(parts)
	return data
}

func (a *Anthropic) buildMessages(msgs []Message) ([]anthropicMsg, error) {
	var out []anthropicMsg

	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			content := buildUserContent(m.Content)
			data, _ := json.Marshal(content)
			out = append(out, anthropicMsg{Role: "user", Content: data})

		case RoleAssistant:
			content := buildAssistantContent(m.Content)
			data, _ := json.Marshal(content)
			out = append(out, anthropicMsg{Role: "assistant", Content: data})

		case RoleTool:
			// Tool results are sent as user messages with tool_result blocks.
			content := buildToolResultContent(m.Content)
			data, _ := json.Marshal(content)
			out = append(out, anthropicMsg{Role: "user", Content: data})

		case RoleSystem:
			// System messages are handled separately; skip.
			continue

		default:
			return nil, fmt.Errorf("unsupported role: %s", m.Role)
		}
	}

	return out, nil
}

func buildUserContent(parts []ContentPart) any {
	// If all text, return as plain string.
	allText := true
	for _, p := range parts {
		if p.Type != ContentText {
			allText = false
			break
		}
	}
	if allText {
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.Text)
		}
		return sb.String()
	}

	// Mixed content — return as blocks.
	var blocks []map[string]any
	for _, p := range parts {
		switch p.Type {
		case ContentText:
			blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
		}
	}
	return blocks
}

func buildAssistantContent(parts []ContentPart) []map[string]any {
	var blocks []map[string]any
	for _, p := range parts {
		switch p.Type {
		case ContentText:
			blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
		case ContentThinking:
			blocks = append(blocks, map[string]any{"type": "thinking", "thinking": p.Text})
		case ContentToolCall:
			input := map[string]any{}
			if len(p.ToolInput) > 0 {
				_ = json.Unmarshal(p.ToolInput, &input)
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    p.ToolCallID,
				"name":  p.ToolName,
				"input": input,
			})
		}
	}
	return blocks
}

func buildToolResultContent(parts []ContentPart) []map[string]any {
	var blocks []map[string]any
	for _, p := range parts {
		if p.Type == ContentToolResult {
			blocks = append(blocks, map[string]any{
				"type":        "tool_result",
				"tool_use_id": p.ToolCallID,
				"content":     p.Text,
				"is_error":    p.IsError,
			})
		}
	}
	return blocks
}

// ---------- HTTP ----------

func (a *Anthropic) buildHeaders() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("anthropic-version", defaultAnthropicVersion)

	if a.authMode == AuthSetupToken {
		h.Set("Authorization", "Bearer "+a.cfg.APIKey)
		h.Set("anthropic-beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14")
		h.Set("User-Agent", fmt.Sprintf("claude-cli/%s (external, cli)", claudeCodeVersion))
		h.Set("x-app", "cli")
	} else {
		h.Set("x-api-key", a.cfg.APIKey)
		h.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
	}

	return h
}

func (a *Anthropic) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	url := a.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vs := range a.buildHeaders() {
		for _, v := range vs {
			httpReq.Header.Set(k, v)
		}
	}
	return a.httpClient.Do(httpReq)
}

// ---------- Send (non-streaming) ----------

func (a *Anthropic) Send(ctx context.Context, req *Request) (*Response, error) {
	ar, err := a.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(ar)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	httpResp, err := a.doRequest(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, parseAPIError(httpResp.StatusCode, respBody)
	}

	return parseResponse(respBody)
}

// ---------- Stream ----------

func (a *Anthropic) Stream(ctx context.Context, req *Request) iter.Seq[StreamEvent] {
	return func(yield func(StreamEvent) bool) {
		ar, err := a.buildRequest(req, true)
		if err != nil {
			yield(StreamEvent{Type: EventError, Err: err})
			return
		}

		body, err := json.Marshal(ar)
		if err != nil {
			yield(StreamEvent{Type: EventError, Err: fmt.Errorf("marshalling request: %w", err)})
			return
		}

		httpResp, err := a.doRequest(ctx, body)
		if err != nil {
			yield(StreamEvent{Type: EventError, Err: fmt.Errorf("http request: %w", err)})
			return
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(httpResp.Body)
			yield(StreamEvent{Type: EventError, Err: parseAPIError(httpResp.StatusCode, respBody)})
			return
		}

		a.consumeSSE(httpResp.Body, yield)
	}
}

// consumeSSE parses the SSE stream from Anthropic.
func (a *Anthropic) consumeSSE(r io.Reader, yield func(StreamEvent) bool) {
	resp := &Response{
		Message: Message{Role: RoleAssistant},
	}

	decoder := json.NewDecoder(r)
	// SSE is line-based: "event: <type>\ndata: <json>\n\n"
	// We use a simple scanner approach.
	buf := make([]byte, 0, 4096)
	readBuf := make([]byte, 4096)

	for {
		n, err := r.Read(readBuf)
		if n > 0 {
			buf = append(buf, readBuf[:n]...)
		}
		// Process complete SSE frames.
		for {
			// Find "data: " line.
			idx := bytes.Index(buf, []byte("data: "))
			if idx < 0 {
				break
			}
			// Find end of that line.
			endIdx := bytes.Index(buf[idx:], []byte("\n"))
			if endIdx < 0 {
				break // incomplete line
			}
			dataLine := buf[idx+6 : idx+endIdx]
			buf = buf[idx+endIdx+1:]

			if string(dataLine) == "[DONE]" {
				continue
			}

			if !a.handleSSEData(dataLine, resp, yield) {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				yield(StreamEvent{Type: EventError, Err: err})
			}
			break
		}
	}

	// We need to avoid double-using the decoder variable above since we switched to raw reads.
	_ = decoder // unused but keep for clarity

	yield(StreamEvent{Type: EventDone, Response: resp})
}

func (a *Anthropic) handleSSEData(data []byte, resp *Response, yield func(StreamEvent) bool) bool {
	var event struct {
		Type  string          `json:"type"`
		Delta json.RawMessage `json:"delta"`
		Message json.RawMessage `json:"message"`
		ContentBlock json.RawMessage `json:"content_block"`
		Index int             `json:"index"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return true // skip unparseable events
	}

	switch event.Type {
	case "message_start":
		// Extract model and initial usage.
		var msg struct {
			Model string `json:"model"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(event.Message, &msg) == nil {
			resp.Model = msg.Model
			resp.Usage.InputTokens = msg.Usage.InputTokens
			resp.Usage.OutputTokens = msg.Usage.OutputTokens
			resp.Usage.CacheRead = msg.Usage.CacheReadInputTokens
			resp.Usage.CacheWrite = msg.Usage.CacheCreationInputTokens
		}

	case "content_block_start":
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(event.ContentBlock, &block) == nil {
			switch block.Type {
			case "text":
				resp.Message.Content = append(resp.Message.Content, ContentPart{Type: ContentText})
			case "thinking":
				resp.Message.Content = append(resp.Message.Content, ContentPart{Type: ContentThinking})
			case "tool_use":
				resp.Message.Content = append(resp.Message.Content, ContentPart{
					Type:       ContentToolCall,
					ToolCallID: block.ID,
					ToolName:   block.Name,
				})
				return yield(StreamEvent{Type: EventToolCallStart})
			}
		}

	case "content_block_delta":
		var delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		}
		if json.Unmarshal(event.Delta, &delta) == nil {
			switch delta.Type {
			case "text_delta":
				if event.Index < len(resp.Message.Content) {
					resp.Message.Content[event.Index].Text += delta.Text
				}
				return yield(StreamEvent{Type: EventTextDelta, Delta: delta.Text})
			case "thinking_delta":
				if event.Index < len(resp.Message.Content) {
					resp.Message.Content[event.Index].Text += delta.Thinking
				}
				return yield(StreamEvent{Type: EventThinkingDelta, Delta: delta.Thinking})
			case "input_json_delta":
				// Accumulate partial JSON for tool input.
				if event.Index < len(resp.Message.Content) {
					part := &resp.Message.Content[event.Index]
					part.ToolInput = append(part.ToolInput, []byte(delta.PartialJSON)...)
				}
				return yield(StreamEvent{Type: EventToolCallDelta, Delta: delta.PartialJSON})
			}
		}

	case "content_block_stop":
		if event.Index < len(resp.Message.Content) {
			part := &resp.Message.Content[event.Index]
			if part.Type == ContentToolCall {
				// Parse accumulated JSON.
				var parsed json.RawMessage
				if json.Unmarshal(part.ToolInput, &parsed) == nil {
					part.ToolInput = parsed
				}
				return yield(StreamEvent{Type: EventToolCallEnd})
			}
		}

	case "message_delta":
		var delta struct {
			StopReason string `json:"stop_reason"`
		}
		if json.Unmarshal(event.Delta, &delta) == nil {
			resp.StopReason = mapAnthropicStopReason(delta.StopReason)
		}
		if event.Usage != nil {
			if event.Usage.InputTokens > 0 {
				resp.Usage.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens > 0 {
				resp.Usage.OutputTokens = event.Usage.OutputTokens
			}
		}
	}

	return true
}

// ---------- Response parsing (non-streaming) ----------

func parseResponse(data []byte) (*Response, error) {
	var raw struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	resp := &Response{
		Model:      raw.Model,
		StopReason: mapAnthropicStopReason(raw.StopReason),
		Message:    Message{Role: RoleAssistant},
		Usage: Usage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
			CacheRead:    raw.Usage.CacheReadInputTokens,
			CacheWrite:   raw.Usage.CacheCreationInputTokens,
		},
	}

	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			resp.Message.Content = append(resp.Message.Content, ContentPart{
				Type: ContentText,
				Text: block.Text,
			})
		case "thinking":
			resp.Message.Content = append(resp.Message.Content, ContentPart{
				Type: ContentThinking,
				Text: block.Thinking,
			})
		case "tool_use":
			inputData, _ := json.Marshal(block.Input)
			resp.Message.Content = append(resp.Message.Content, ContentPart{
				Type:       ContentToolCall,
				ToolCallID: block.ID,
				ToolName:   block.Name,
				ToolInput:  inputData,
			})
		}
	}

	return resp, nil
}

func parseAPIError(statusCode int, body []byte) error {
	var raw struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &raw) == nil && raw.Error.Message != "" {
		return &APIError{
			StatusCode: statusCode,
			Type:       raw.Error.Type,
			Message:    raw.Error.Message,
		}
	}
	return &APIError{
		StatusCode: statusCode,
		Type:       "unknown",
		Message:    string(body),
	}
}

func mapAnthropicStopReason(reason string) StopReason {
	switch reason {
	case "end_turn":
		return StopEnd
	case "max_tokens":
		return StopLength
	case "tool_use":
		return StopTool
	default:
		return StopEnd
	}
}
