package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeCode wraps the `claude` CLI as an LLM backend.
type ClaudeCode struct {
	// Binary is the path to the claude CLI. Defaults to "claude".
	Binary string
	// Model overrides the default model (optional).
	Model string
	// WorkDir sets the working directory for claude sessions.
	WorkDir string
}

// NewClaudeCode creates a ClaudeCode client with defaults.
func NewClaudeCode() *ClaudeCode {
	return &ClaudeCode{Binary: "claude"}
}

// Generate sends a prompt to claude CLI and returns the response.
func (c *ClaudeCode) Generate(ctx context.Context, prompt string) (string, error) {
	return c.run(ctx, "", prompt)
}

// GenerateWithSystem sends a prompt with a system instruction.
func (c *ClaudeCode) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	return c.run(ctx, system, prompt)
}

func (c *ClaudeCode) run(ctx context.Context, system, prompt string) (string, error) {
	binary := c.Binary
	if binary == "" {
		binary = "claude"
	}

	args := []string{
		"--print",        // output only the response
		"--output-format", "json",
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if system != "" {
		args = append(args, "--system-prompt", system)
	}
	args = append(args, "--prompt", prompt)

	cmd := exec.CommandContext(ctx, binary, args...)
	if c.WorkDir != "" {
		cmd.Dir = c.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude: %w: %s", err, stderr.String())
	}

	// Try to parse JSON response, fall back to raw text.
	raw := stdout.String()
	var resp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err == nil && resp.Result != "" {
		return resp.Result, nil
	}

	return strings.TrimSpace(raw), nil
}
