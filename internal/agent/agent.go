// Package agent implements the GEPA-style core loop:
// Goal → Explore → Plan → Act → Reflect → Evolve.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rcliao/teeny-claw/internal/memory"
	"github.com/rcliao/teeny-claw/internal/tools"
	"github.com/rcliao/teeny-claw/pkg/llm"
)

// Phase represents a step in the GEPA loop.
type Phase string

const (
	PhaseGoal    Phase = "goal"
	PhaseExplore Phase = "explore"
	PhasePlan    Phase = "plan"
	PhaseAct     Phase = "act"
	PhaseReflect Phase = "reflect"
	PhaseEvolve  Phase = "evolve"
)

const defaultMaxActSteps = 10

// Task represents a unit of work for the agent.
type Task struct {
	ID          string
	Description string
	Context     string // additional context
	CreatedAt   time.Time
}

// StepResult captures the outcome of a single GEPA cycle.
type StepResult struct {
	Task       Task
	Plan       string
	Actions    []ActionResult
	Reflection string
	Success    bool
	Duration   time.Duration
}

// ActionResult captures one tool execution within a cycle.
type ActionResult struct {
	Tool   string
	Input  string
	Output string
	OK     bool
}

// Agent is the core autonomous agent.
type Agent struct {
	llm         llm.Client
	memory      *memory.Manager
	tools       *tools.Registry
	maxRetries  int
	maxActSteps int
	logger      *slog.Logger
}

// New creates an Agent with the given dependencies.
func New(client llm.Client, mem *memory.Manager, reg *tools.Registry, opts ...Option) *Agent {
	a := &Agent{
		llm:         client,
		memory:      mem,
		tools:       reg,
		maxRetries:  3,
		maxActSteps: defaultMaxActSteps,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Option configures an Agent.
type Option func(*Agent)

// WithMaxRetries sets the retry limit for self-correction.
func WithMaxRetries(n int) Option {
	return func(a *Agent) { a.maxRetries = n }
}

// WithMaxActSteps sets the maximum tool calls per cycle.
func WithMaxActSteps(n int) Option {
	return func(a *Agent) { a.maxActSteps = n }
}

// WithLogger sets the agent's logger.
func WithLogger(l *slog.Logger) Option {
	return func(a *Agent) { a.logger = l }
}

// Run executes a single GEPA cycle for the given task.
func (a *Agent) Run(ctx context.Context, task Task) (*StepResult, error) {
	start := time.Now()
	result := &StepResult{Task: task}

	a.logger.Info("agent: starting cycle", "task", task.ID, "description", task.Description)

	// 1. Goal — already defined by task.

	// 2. Explore — search memory for related context.
	memories, err := a.memory.Recall(ctx, task.Description, 5)
	if err != nil {
		a.logger.Warn("agent: memory recall failed", "error", err)
	}
	var memContext string
	for _, m := range memories {
		memContext += fmt.Sprintf("- [%s] %s\n", m.Kind, m.Content)
	}

	// 3. Plan — ask LLM to create a plan.
	planPrompt := fmt.Sprintf(
		"Task: %s\n\nContext: %s\n\nRelevant memories:\n%s\n\nCreate a concise plan to accomplish this task. List specific steps.",
		task.Description, task.Context, memContext,
	)
	plan, err := a.llm.Generate(ctx, planPrompt)
	if err != nil {
		return nil, fmt.Errorf("planning: %w", err)
	}
	result.Plan = plan

	// 4. Act — multi-step tool execution with self-correction.
	a.act(ctx, result)

	// 5. Reflect — analyze what happened.
	actSummary := a.summarizeActions(result.Actions)
	reflectPrompt := fmt.Sprintf(
		"Task: %s\nPlan: %s\nActions taken:\n%s\n\nReflect: What worked? What didn't? What lesson should be remembered?",
		task.Description, plan, actSummary,
	)
	reflection, err := a.llm.Generate(ctx, reflectPrompt)
	if err != nil {
		a.logger.Warn("agent: reflection failed", "error", err)
		reflection = "reflection unavailable"
	}
	result.Reflection = reflection

	// 6. Evolve — save lessons to memory.
	if err := a.memory.Remember(ctx, reflection, memory.KindLesson, "auto-reflection"); err != nil {
		a.logger.Warn("agent: failed to save lesson", "error", err)
	}

	result.Success = true
	result.Duration = time.Since(start)

	a.logger.Info("agent: cycle complete", "task", task.ID, "duration", result.Duration)
	return result, nil
}

// act runs the multi-step tool execution loop with self-correction.
func (a *Agent) act(ctx context.Context, result *StepResult) {
	var history string
	toolNames := a.tools.List()

	for step := 0; step < a.maxActSteps; step++ {
		prompt := fmt.Sprintf(
			"Plan:\n%s\n\nAvailable tools: %v\n\n%sWhat is the next tool call? Reply with TOOL:<name> INPUT:<input> or DONE if the plan is complete.",
			result.Plan, toolNames, history,
		)
		response, err := a.llm.Generate(ctx, prompt)
		if err != nil {
			a.logger.Warn("agent: act LLM call failed", "error", err)
			break
		}

		toolName, input, done := parseToolCall(response)
		if done {
			break
		}

		// Execute the tool.
		tr := a.tools.Execute(ctx, toolName, input)
		ar := ActionResult{
			Tool:   toolName,
			Input:  input,
			Output: tr.Output,
			OK:     tr.OK(),
		}
		result.Actions = append(result.Actions, ar)

		if tr.OK() {
			history += fmt.Sprintf("Step %d: TOOL:%s → OK: %s\n", step+1, toolName, truncate(tr.Output, 200))
		} else {
			// Self-correction: feed error back to LLM.
			history += fmt.Sprintf("Step %d: TOOL:%s → ERROR: %s (output: %s)\n", step+1, toolName, tr.Error, truncate(tr.Output, 200))
			a.logger.Info("agent: tool failed, will self-correct", "tool", toolName, "error", tr.Error)
		}
	}
}

// parseToolCall extracts tool name and input from LLM response.
// Expected format: "TOOL:<name> INPUT:<input>" or "DONE".
func parseToolCall(response string) (tool, input string, done bool) {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "DONE") || !strings.Contains(response, "TOOL:") {
		return "", "", true
	}

	// Extract TOOL:<name>
	toolIdx := strings.Index(response, "TOOL:")
	if toolIdx < 0 {
		return "", "", true
	}
	rest := response[toolIdx+5:]

	// Find INPUT: marker
	inputIdx := strings.Index(rest, "INPUT:")
	if inputIdx < 0 {
		// Tool name only, no input
		tool = strings.TrimSpace(rest)
		return tool, "", false
	}

	tool = strings.TrimSpace(rest[:inputIdx])
	input = strings.TrimSpace(rest[inputIdx+6:])
	return tool, input, false
}

func (a *Agent) summarizeActions(actions []ActionResult) string {
	var sb strings.Builder
	for i, ar := range actions {
		status := "OK"
		if !ar.OK {
			status = "FAILED"
		}
		fmt.Fprintf(&sb, "%d. [%s] %s(%s) → %s\n", i+1, status, ar.Tool, truncate(ar.Input, 80), truncate(ar.Output, 100))
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
