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
	model       string
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

// WithModel sets the model ID for LLM requests.
func WithModel(model string) Option {
	return func(a *Agent) { a.model = model }
}

// generate sends a simple text prompt to the LLM and returns the text response.
func (a *Agent) generate(ctx context.Context, prompt string) (string, error) {
	return llm.Generate(ctx, a.llm, a.model, prompt)
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
	plan, err := a.generate(ctx, planPrompt)
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
	reflection, err := a.generate(ctx, reflectPrompt)
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
// It uses native tool calling when the LLM supports it (returns ContentToolCall parts),
// falling back to text-based TOOL:/INPUT: parsing for providers that don't.
func (a *Agent) act(ctx context.Context, result *StepResult) {
	toolDefs := a.tools.ToToolDefs()

	// Build conversation messages for the act loop
	var messages []llm.Message
	messages = append(messages, llm.TextMessage(llm.RoleUser, fmt.Sprintf(
		"Execute the following plan using the available tools. When you're done, reply with just the word DONE.\n\nPlan:\n%s",
		result.Plan,
	)))

	for step := 0; step < a.maxActSteps; step++ {
		resp, err := a.llm.Send(ctx, &llm.Request{
			Model:    a.model,
			Messages: messages,
			Tools:    toolDefs,
		})
		if err != nil {
			a.logger.Warn("agent: act LLM call failed", "error", err)
			break
		}

		// Check for native tool calls
		calls := resp.Message.ToolCalls()
		if len(calls) > 0 {
			// Append assistant message to conversation
			messages = append(messages, resp.Message)

			for _, call := range calls {
				tr := a.tools.Execute(ctx, call.ToolName, string(call.ToolInput))
				ar := ActionResult{
					Tool:   call.ToolName,
					Input:  string(call.ToolInput),
					Output: tr.Output,
					OK:     tr.OK(),
				}
				result.Actions = append(result.Actions, ar)

				// Feed tool result back
				resultText := tr.Output
				isError := false
				if !tr.OK() {
					resultText = fmt.Sprintf("ERROR: %v\nOutput: %s", tr.Error, tr.Output)
					isError = true
					a.logger.Info("agent: tool failed, will self-correct", "tool", call.ToolName, "error", tr.Error)
				}
				messages = append(messages, llm.ToolResultMessage(call.ToolCallID, resultText, isError))
			}
			continue
		}

		// Fallback: text-based parsing for providers without native tool calls
		text := resp.Message.Text()
		toolName, input, done := parseToolCall(text)
		if done {
			break
		}

		tr := a.tools.Execute(ctx, toolName, input)
		ar := ActionResult{
			Tool:   toolName,
			Input:  input,
			Output: tr.Output,
			OK:     tr.OK(),
		}
		result.Actions = append(result.Actions, ar)

		// Append assistant message and tool result to conversation
		messages = append(messages, resp.Message)
		if tr.OK() {
			messages = append(messages, llm.TextMessage(llm.RoleUser,
				fmt.Sprintf("Tool %s returned: %s\n\nContinue with the plan or reply DONE.", toolName, truncate(tr.Output, 500))))
		} else {
			messages = append(messages, llm.TextMessage(llm.RoleUser,
				fmt.Sprintf("Tool %s failed: %v (output: %s)\n\nAdjust and continue or reply DONE.", toolName, tr.Error, truncate(tr.Output, 500))))
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
