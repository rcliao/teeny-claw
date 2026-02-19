package llm

import "fmt"

// ClientConfig holds provider configuration for the factory.
type ClientConfig struct {
	Name    string
	APIKey  string
	Model   string
	BaseURL string // Optional: custom endpoint for OpenAI-compatible APIs
	Verbose bool
}

// NewClient creates a Client by provider name.
func NewClient(name, apiKey, model string) (Client, error) {
	return NewClientFromConfig(ClientConfig{Name: name, APIKey: apiKey, Model: model})
}

// NewClientFromConfig creates a Client from a full config.
func NewClientFromConfig(cfg ClientConfig) (Client, error) {
	switch cfg.Name {
	case "anthropic", "claude":
		return NewAnthropic(AnthropicConfig{
			APIKey:       cfg.APIKey,
			DefaultModel: cfg.Model,
		}), nil
	case "openai", "gpt":
		opts := []OpenAIOption{}
		if cfg.BaseURL != "" {
			opts = append(opts, WithBaseURL(cfg.BaseURL))
		}
		return NewOpenAI(cfg.APIKey, cfg.Model, opts...), nil
	case "claude-cli", "claude_code", "claudecode":
		return NewClaudeCLI(cfg.Model, WithClaudeVerbose(cfg.Verbose)), nil
	default:
		return nil, fmt.Errorf("unknown provider: %q (supported: anthropic, openai, claude-cli)", cfg.Name)
	}
}
