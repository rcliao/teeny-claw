// Package config handles loading and validating teeny-claw configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config is the top-level configuration for teeny-claw.
type Config struct {
	// Agent configuration.
	Agent AgentConfig `json:"agent"`
	// Memory configuration.
	Memory MemoryConfig `json:"memory"`
	// Process manager configuration.
	Process ProcessConfig `json:"process"`
	// Scheduler configuration.
	Scheduler SchedulerConfig `json:"scheduler"`
}

// AgentConfig controls agent loop behavior.
type AgentConfig struct {
	// MaxRetries for self-correction before escalating.
	MaxRetries int `json:"max_retries"`
	// ReflectionEnabled turns on the reflect+evolve steps.
	ReflectionEnabled bool `json:"reflection_enabled"`
}

// MemoryConfig controls the memory subsystem.
type MemoryConfig struct {
	// Dir is the directory for memory files.
	Dir string `json:"dir"`
	// MaxWorkingTokens is the token budget for working memory.
	MaxWorkingTokens int `json:"max_working_tokens"`
}

// ProcessConfig controls process management.
type ProcessConfig struct {
	// DefaultTimeout for spawned processes.
	DefaultTimeout time.Duration `json:"default_timeout"`
	// MaxConcurrent sessions.
	MaxConcurrent int `json:"max_concurrent"`
}

// SchedulerConfig controls cron and heartbeat scheduling.
type SchedulerConfig struct {
	// HeartbeatInterval between heartbeat checks.
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

// Default returns a Config with sensible defaults.
func Default() Config {
	return Config{
		Agent: AgentConfig{
			MaxRetries:        3,
			ReflectionEnabled: true,
		},
		Memory: MemoryConfig{
			Dir:              "memory",
			MaxWorkingTokens: 8000,
		},
		Process: ProcessConfig{
			DefaultTimeout: 5 * time.Minute,
			MaxConcurrent:  4,
		},
		Scheduler: SchedulerConfig{
			HeartbeatInterval: 30 * time.Minute,
		},
	}
}

// Load reads configuration from a JSON file, falling back to defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return cfg, fmt.Errorf("resolving config path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config %s: %w", absPath, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config %s: %w", absPath, err)
	}

	return cfg, nil
}
