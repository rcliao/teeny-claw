package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const openaiDefaultURL = "https://api.openai.com/v1/chat/completions"

// OpenAI implements Client for OpenAI-compatible APIs.
// Works with OpenAI, Groq, Together, Ollama, and any OpenAI-compatible endpoint.
type OpenAI struct {
	apiKey  string
	model   string
	baseURL string
}

// OpenAIOption configures an OpenAI client.
type OpenAIOption func(*OpenAI)

// WithBaseURL sets a custom API base URL (for compatible providers).
func WithBaseURL(url string) OpenAIOption {
	return func(o *OpenAI) { o.baseURL = url }
}

// NewOpenAI creates an OpenAI-compatible LLM client.
// apiKey defaults to OPENAI_API_KEY env var if empty.
// model defaults to gpt-4o if empty.
func NewOpenAI(apiKey, model string, opts ...OpenAIOption) *OpenAI {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if model == "" {
		model = "gpt-4o"
	}
	o := &OpenAI{apiKey: apiKey, model: model, baseURL: openaiDefaultURL}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// OpenAI API wire types

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Tools    []openaiTool    `json:"tools,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Send implements llm.Client for OpenAI-compatible APIs.
func (o *OpenAI) Send(ctx context.Context, req *Request) (*Response, error) {
	if o.apiKey == "" {
		return nil, fmt.Errorf("openai: API key not set (OPENAI_API_KEY)")
	}

	model := req.Model
	if model == "" {
		model = o.model
	}

	// Convert messages from llm.Message → openaiMessage
	var msgs []openaiMessage

	// Inject system prompt
	if req.System != "" {
		msgs = append(msgs, openaiMessage{Role: "system", Content: req.System})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			msgs = append(msgs, openaiMessage{Role: "system", Content: m.Text()})
		case RoleUser:
			msgs = append(msgs, openaiMessage{Role: "user", Content: m.Text()})
		case RoleAssistant:
			msg := openaiMessage{Role: "assistant", Content: m.Text()}
			for _, p := range m.Content {
				if p.Type == ContentToolCall {
					msg.ToolCalls = append(msg.ToolCalls, openaiToolCall{
						ID:   p.ToolCallID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: p.ToolName, Arguments: string(p.ToolInput)},
					})
				}
			}
			msgs = append(msgs, msg)
		case RoleTool:
			for _, p := range m.Content {
				if p.Type == ContentToolResult {
					msgs = append(msgs, openaiMessage{
						Role:       "tool",
						Content:    p.Text,
						ToolCallID: p.ToolCallID,
					})
				}
			}
		}
	}

	// Convert tools
	var tools []openaiTool
	for _, t := range req.Tools {
		tool := openaiTool{Type: "function"}
		tool.Function.Name = t.Name
		tool.Function.Description = t.Description
		tool.Function.Parameters = t.Parameters
		tools = append(tools, tool)
	}

	apiReq := openaiRequest{
		Model:    model,
		Messages: msgs,
		Tools:    tools,
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp openaiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("openai: API error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return &Response{Message: TextMessage(RoleAssistant, "")}, nil
	}

	choice := apiResp.Choices[0]
	var parts []ContentPart
	if choice.Message.Content != "" {
		parts = append(parts, ContentPart{Type: ContentText, Text: choice.Message.Content})
	}

	stopReason := StopEnd
	for _, tc := range choice.Message.ToolCalls {
		parts = append(parts, ContentPart{
			Type:       ContentToolCall,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			ToolInput:  json.RawMessage(tc.Function.Arguments),
		})
		stopReason = StopTool
	}

	return &Response{
		Message: Message{Role: RoleAssistant, Content: parts},
		Usage: Usage{
			InputTokens:  apiResp.Usage.PromptTokens,
			OutputTokens: apiResp.Usage.CompletionTokens,
		},
		StopReason: stopReason,
	}, nil
}
