// Package llm provides the LLM client abstraction layer.
// The primary implementation wraps Claude Code CLI.
package llm

import (
	"context"
)

// Client is the interface for LLM interactions.
type Client interface {
	// Generate produces a text response for the given prompt.
	Generate(ctx context.Context, prompt string) (string, error)

	// GenerateWithSystem produces a response using a system prompt.
	GenerateWithSystem(ctx context.Context, system, prompt string) (string, error)
}

// Message represents a conversation message.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Role identifies who sent a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)
