package llm

import (
	"context"
	"sync"
)

// Mock is a test double for Client.
type Mock struct {
	mu sync.Mutex

	// Response is returned by Send calls.
	Response *Response
	// Err is returned if non-nil.
	Err error
	// Requests records all requests received.
	Requests []*Request
}

// NewMock creates a Mock that returns the given text.
func NewMock(text string) *Mock {
	return &Mock{
		Response: &Response{
			Message:    TextMessage(RoleAssistant, text),
			StopReason: StopEnd,
		},
	}
}

// NewMockToolCall creates a Mock that returns a tool call.
func NewMockToolCall(toolCallID, toolName string, input []byte) *Mock {
	return &Mock{
		Response: &Response{
			Message: Message{
				Role: RoleAssistant,
				Content: []ContentPart{{
					Type:       ContentToolCall,
					ToolCallID: toolCallID,
					ToolName:   toolName,
					ToolInput:  input,
				}},
			},
			StopReason: StopTool,
		},
	}
}

func (m *Mock) Send(_ context.Context, req *Request) (*Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = append(m.Requests, req)
	return m.Response, m.Err
}

var _ Client = (*Mock)(nil)
