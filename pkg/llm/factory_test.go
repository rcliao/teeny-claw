package llm

import "testing"

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"anthropic", false},
		{"claude", false},
		{"openai", false},
		{"gpt", false},
		{"claude-cli", false},
		{"unknown", true},
	}
	for _, tt := range tests {
		c, err := NewClient(tt.name, "key", "model")
		if tt.wantErr {
			if err == nil {
				t.Errorf("NewClient(%q) expected error", tt.name)
			}
		} else {
			if err != nil {
				t.Errorf("NewClient(%q) error: %v", tt.name, err)
			}
			if c == nil {
				t.Errorf("NewClient(%q) returned nil", tt.name)
			}
		}
	}
}

func TestNewClientFromConfig(t *testing.T) {
	// OpenAI with base URL
	c, err := NewClientFromConfig(ClientConfig{Name: "openai", APIKey: "key", Model: "gpt-4", BaseURL: "http://localhost:11434/v1"})
	if err != nil {
		t.Fatalf("NewClientFromConfig openai: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}

	// Claude CLI
	c2, err := NewClientFromConfig(ClientConfig{Name: "claude-cli", Model: "sonnet"})
	if err != nil {
		t.Fatalf("NewClientFromConfig claude-cli: %v", err)
	}
	if c2 == nil {
		t.Fatal("nil client")
	}

	// Unknown
	_, err = NewClientFromConfig(ClientConfig{Name: "badprovider"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
