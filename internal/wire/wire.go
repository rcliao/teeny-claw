// Package wire builds the full dependency graph from configuration.
package wire

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rcliao/teeny-claw/internal/agent"
	"github.com/rcliao/teeny-claw/internal/memory"
	"github.com/rcliao/teeny-claw/internal/tools"
	ctxpkg "github.com/rcliao/teeny-claw/pkg/context"
	"github.com/rcliao/teeny-claw/pkg/eval"
	"github.com/rcliao/teeny-claw/pkg/llm"
	"github.com/rcliao/teeny-claw/pkg/session"
	"github.com/rcliao/teeny-claw/pkg/toolreg"
)

// AppConfig holds the full application configuration.
type AppConfig struct {
	Workspace string
	Provider  ProviderConfig
	Tools     ToolsConfig
	Session   SessionConfig
}

// ProviderConfig holds LLM provider settings.
type ProviderConfig struct {
	Name            string
	Model           string
	APIKeyEnv       string
	BaseURL         string
	AllowAutonomous bool
	Verbose         bool
}

// ToolsConfig holds tool discovery settings.
type ToolsConfig struct {
	Path    []string
	Timeout int // seconds
}

// SessionConfig holds session persistence settings.
type SessionConfig struct {
	Dir string
}

// Components holds the wired-up application components.
type Components struct {
	Agent      *agent.Agent
	Sessions   *session.Manager
	ToolReg    *toolreg.Registry
	CtxBuilder *ctxpkg.Builder
	EvalClient *eval.Client
	LLMClient  llm.Client
}

// Build constructs all components from configuration.
func Build(cfg AppConfig) (*Components, error) {
	// LLM client
	apiKey := os.Getenv(cfg.Provider.APIKeyEnv)
	client, err := llm.NewClientFromConfig(llm.ClientConfig{
		Name:    cfg.Provider.Name,
		APIKey:  apiKey,
		Model:   cfg.Provider.Model,
		BaseURL: cfg.Provider.BaseURL,
		Verbose: cfg.Provider.Verbose,
	})
	if err != nil {
		return nil, fmt.Errorf("create LLM client: %w", err)
	}

	// Tool registries
	timeout := time.Duration(cfg.Tools.Timeout) * time.Second
	manifestReg := toolreg.NewRegistry(timeout)
	if err := manifestReg.Discover(cfg.Tools.Path); err != nil {
		return nil, fmt.Errorf("discover tools: %w", err)
	}

	// Register builtins
	registerBuiltins(manifestReg, cfg.Workspace)
	toolreg.RegisterInternalTodoTools(manifestReg, cfg.Workspace)

	// Bridge manifest tools into the agent's tool registry
	agentTools := tools.NewRegistry()
	agentTools.Register(&tools.Shell{WorkDir: cfg.Workspace})
	manifestReg.RegisterInto(agentTools)

	// Context builder
	ctxCfg := ctxpkg.DefaultConfig()
	ctxBuilder := ctxpkg.NewBuilder(cfg.Workspace, ctxCfg, manifestReg)

	// Inject learnings (best-effort)
	evalClient := eval.NewClient(eval.DefaultConfig())
	if learnings, err := evalClient.BuildLearningContext(context.Background(), ctxCfg.LearningsTopic, 5); err == nil && learnings != "" {
		ctxBuilder.SetLearnings(learnings)
	}

	// Session manager
	sessions := session.NewManager(cfg.Session.Dir)

	// Memory
	store := memory.NewMemStore()
	mem := memory.NewManager(store)

	// Agent
	a := agent.New(client, mem, agentTools,
		agent.WithModel(cfg.Provider.Model),
	)

	return &Components{
		Agent:      a,
		Sessions:   sessions,
		ToolReg:    manifestReg,
		CtxBuilder: ctxBuilder,
		EvalClient: evalClient,
		LLMClient:  client,
	}, nil
}

// registerBuiltins adds read_file, list_dir as built-in manifest tools.
func registerBuiltins(reg *toolreg.Registry, workspace string) {
	_ = workspace
	reg.Register(&toolreg.ToolManifest{
		Name:        "read_file",
		Binary:      "cat",
		Description: "Read a file's contents",
		Commands: map[string]toolreg.CommandDef{
			"run": {
				Description: "Read a file",
				Args:        "{path}",
				Parameters: map[string]toolreg.ParameterDef{
					"path": {Type: "string", Description: "File path to read", Required: true},
				},
			},
		},
	})

	reg.Register(&toolreg.ToolManifest{
		Name:        "list_dir",
		Binary:      "ls",
		Description: "List directory contents",
		Commands: map[string]toolreg.CommandDef{
			"run": {
				Description: "List files in a directory",
				Args:        "-la {path}",
				Parameters: map[string]toolreg.ParameterDef{
					"path": {Type: "string", Description: "Directory path", Required: true},
				},
			},
		},
	})
}
