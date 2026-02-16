// Package agent implements the GEPA-style core loop:
// Goal → Explore → Plan → Act → Reflect → Evolve.
package agent

import (
	"context"
	"fmt"
	"log/slog"
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
	llm        llm.Client
	memory     *memory.Manager
	tools      *tools.Registry
	maxRetries int
	logger     *slog.Logger
}

// New creates an Agent with the given dependencies.
func New(client llm.Client, mem *memory.Manager, reg *tools.Registry, opts ...Option) *Agent {
	a := &Agent{
		llm:        client,
		memory:     mem,
		tools:      reg,
		maxRetries: 3,
		logger:     slog.Default(),
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

	// 4. Act — ask LLM what tool calls to make, then execute them.
	actPrompt := fmt.Sprintf(
		"Plan:\n%s\n\nAvailable tools: %v\n\nWhat is the first tool call to execute? Reply with TOOL:<name> INPUT:<input> or DONE if complete.",
		plan, a.tools.List(),
	)
	response, err := a.llm.Generate(ctx, actPrompt)
	if err != nil {
		return nil, fmt.Errorf("acting: %w", err)
	}

	// For now, store the raw response as an action. Tool parsing comes next iteration.
	result.Actions = append(result.Actions, ActionResult{
		Tool:   "llm",
		Input:  actPrompt,
		Output: response,
		OK:     true,
	})

	// 5. Reflect — analyze what happened.
	reflectPrompt := fmt.Sprintf(
		"Task: %s\nPlan: %s\nOutcome: %s\n\nReflect: What worked? What didn't? What lesson should be remembered?",
		task.Description, plan, response,
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
