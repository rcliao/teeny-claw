// Package eval provides eval-grounded self-review capabilities.
// It queries token-eval for recent LLM call data and agent-memory for stored learnings,
// enabling the agent to improve over time.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Record represents a token-eval capture record.
type Record struct {
	ID               string  `json:"id"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Intent           string  `json:"intent"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	CreatedAt        string  `json:"created_at"`
}

// Learning represents an insight stored in agent-memory.
type Learning struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Tags    string `json:"tags"`
}

// Config for the eval module.
type Config struct {
	TokenEvalBinary   string // Path to token-eval binary (default: "token-eval")
	AgentMemoryBinary string // Path to agent-memory binary (default: "agent-memory")
	LookbackHours     int    // How far back to query (default: 24)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		TokenEvalBinary:   "token-eval",
		AgentMemoryBinary: "agent-memory",
		LookbackHours:     24,
	}
}

// Client provides eval data access.
type Client struct {
	cfg Config
}

// NewClient creates an eval client.
func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg}
}

// QueryRecentCalls fetches recent token-eval records.
func (c *Client) QueryRecentCalls(ctx context.Context, limit int) ([]Record, error) {
	binary := c.cfg.TokenEvalBinary
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("token-eval not found: %w", err)
	}

	since := time.Now().Add(-time.Duration(c.cfg.LookbackHours) * time.Hour).Format(time.RFC3339)
	args := []string{"query", "--since", since, "--limit", fmt.Sprintf("%d", limit), "--json"}
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("token-eval query: %w", err)
	}

	var records []Record
	if err := json.Unmarshal(out, &records); err != nil {
		// Try line-delimited JSON
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			var r Record
			if err := json.Unmarshal([]byte(line), &r); err == nil {
				records = append(records, r)
			}
		}
	}
	return records, nil
}

// QueryLearnings fetches stored learnings from agent-memory.
func (c *Client) QueryLearnings(ctx context.Context, query string, limit int) ([]Learning, error) {
	binary := c.cfg.AgentMemoryBinary
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("agent-memory not found: %w", err)
	}

	args := []string{"search", query, "--limit", fmt.Sprintf("%d", limit), "--json"}
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agent-memory search: %w", err)
	}

	var learnings []Learning
	if err := json.Unmarshal(out, &learnings); err != nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			var l Learning
			if err := json.Unmarshal([]byte(line), &l); err == nil {
				learnings = append(learnings, l)
			}
		}
	}
	return learnings, nil
}

// StoreLearning saves a learning to agent-memory.
func (c *Client) StoreLearning(ctx context.Context, content string, tags []string) error {
	binary := c.cfg.AgentMemoryBinary
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("agent-memory not found: %w", err)
	}

	args := []string{"add", "--tags", strings.Join(tags, ",")}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("agent-memory add: %s: %w", string(out), err)
	}
	return nil
}

// BuildReviewSummary creates a text summary of recent eval data for the LLM to review.
func (c *Client) BuildReviewSummary(ctx context.Context) (string, error) {
	records, err := c.QueryRecentCalls(ctx, 50)
	if err != nil {
		return "", err
	}

	if len(records) == 0 {
		return "No recent LLM calls found in the last 24 hours.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Recent LLM Calls (last %dh)\n\n", c.cfg.LookbackHours))

	totalCost := 0.0
	totalPrompt := 0
	totalCompletion := 0
	for _, r := range records {
		totalCost += r.Cost
		totalPrompt += r.PromptTokens
		totalCompletion += r.CompletionTokens
	}

	sb.WriteString(fmt.Sprintf("- **Total calls**: %d\n", len(records)))
	sb.WriteString(fmt.Sprintf("- **Total tokens**: %d prompt + %d completion = %d\n",
		totalPrompt, totalCompletion, totalPrompt+totalCompletion))
	if totalCost > 0 {
		sb.WriteString(fmt.Sprintf("- **Total cost**: $%.4f\n", totalCost))
	}
	sb.WriteString("\n### Call Details\n\n")

	for _, r := range records {
		sb.WriteString(fmt.Sprintf("- `%s` | %s | %d+%d tokens | intent: %s\n",
			r.CreatedAt, r.Model, r.PromptTokens, r.CompletionTokens, r.Intent))
	}

	return sb.String(), nil
}

// BuildLearningContext fetches relevant learnings for injection into the system prompt.
func (c *Client) BuildLearningContext(ctx context.Context, topic string, limit int) (string, error) {
	learnings, err := c.QueryLearnings(ctx, topic, limit)
	if err != nil {
		return "", nil // Gracefully degrade — no learnings is fine
	}

	if len(learnings) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("## Learnings from Previous Sessions\n\n")
	for _, l := range learnings {
		sb.WriteString(fmt.Sprintf("- %s", l.Content))
		if l.Tags != "" {
			sb.WriteString(fmt.Sprintf(" [tags: %s]", l.Tags))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
