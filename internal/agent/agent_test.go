package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/rcliao/teeny-claw/internal/memory"
	"github.com/rcliao/teeny-claw/internal/tools"
	"github.com/rcliao/teeny-claw/pkg/llm"
)

func newTestAgent(mock *llm.Mock) (*Agent, *memory.Manager, *tools.Registry) {
	store := memory.NewMemStore()
	mem := memory.NewManager(store)
	reg := tools.NewRegistry()
	reg.Register(&tools.Shell{})
	a := New(mock, mem, reg, WithMaxRetries(2))
	return a, mem, reg
}

func TestRunBasicCycle(t *testing.T) {
	mock := &llm.Mock{Response: "DONE"}
	agent, _, _ := newTestAgent(mock)

	task := Task{ID: "t1", Description: "test task"}
	result, err := agent.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Task.ID != "t1" {
		t.Errorf("got task ID %q, want t1", result.Task.ID)
	}
	// Should have 3 LLM calls: plan, act, reflect
	if len(mock.Calls) != 3 {
		t.Errorf("got %d LLM calls, want 3", len(mock.Calls))
	}
}

func TestRunWithToolCall(t *testing.T) {
	responses := []string{
		"Step 1: run echo",                    // plan
		`TOOL:shell INPUT:echo hello`,         // act (first call)
		"DONE",                                // act (second call, after tool result)
		"Learned that echo works",             // reflect
	}
	mock := &sequenceMock{responses: responses}
	store := memory.NewMemStore()
	mem := memory.NewManager(store)
	reg := tools.NewRegistry()
	reg.Register(&tools.Shell{})
	agent := New(mock, mem, reg)

	task := Task{ID: "t2", Description: "run echo hello"}
	result, err := agent.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	// Should have at least one real tool action
	found := false
	for _, a := range result.Actions {
		if a.Tool == "shell" {
			found = true
			if !a.OK {
				t.Error("shell action should be OK")
			}
		}
	}
	if !found {
		t.Error("expected a shell tool action")
	}
}

func TestRunWithSelfCorrection(t *testing.T) {
	responses := []string{
		"Step 1: run failing command",                // plan
		`TOOL:shell INPUT:false`,                     // act: 'false' exits 1
		`TOOL:shell INPUT:echo recovered`,            // act: retry with correction
		"DONE",                                       // act: done
		"Learned to handle failures",                 // reflect
	}
	mock := &sequenceMock{responses: responses}
	store := memory.NewMemStore()
	mem := memory.NewManager(store)
	reg := tools.NewRegistry()
	reg.Register(&tools.Shell{})
	agent := New(mock, mem, reg, WithMaxRetries(3))

	task := Task{ID: "t3", Description: "test self-correction"}
	result, err := agent.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success after correction")
	}
	// Should have both failing and succeeding actions
	var failCount, okCount int
	for _, a := range result.Actions {
		if a.Tool == "shell" {
			if a.OK {
				okCount++
			} else {
				failCount++
			}
		}
	}
	if failCount == 0 {
		t.Error("expected at least one failed action")
	}
	if okCount == 0 {
		t.Error("expected at least one successful action")
	}
}

func TestRunPlanError(t *testing.T) {
	mock := &llm.Mock{Err: errors.New("llm down")}
	agent, _, _ := newTestAgent(mock)

	task := Task{ID: "t4", Description: "test error"}
	_, err := agent.Run(context.Background(), task)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunMaxActSteps(t *testing.T) {
	// LLM always returns a tool call — should stop at maxActSteps
	mock := &llm.Mock{Response: "TOOL:shell INPUT:echo loop"}
	store := memory.NewMemStore()
	mem := memory.NewManager(store)
	reg := tools.NewRegistry()
	reg.Register(&tools.Shell{})
	agent := New(mock, mem, reg, WithMaxActSteps(3))

	task := Task{ID: "t5", Description: "test max steps"}
	result, err := agent.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have exactly 3 shell actions (capped)
	shellCount := 0
	for _, a := range result.Actions {
		if a.Tool == "shell" {
			shellCount++
		}
	}
	if shellCount != 3 {
		t.Errorf("got %d shell actions, want 3", shellCount)
	}
}

// sequenceMock returns responses in order.
type sequenceMock struct {
	responses []string
	idx       int
}

func (m *sequenceMock) Generate(_ context.Context, prompt string) (string, error) {
	if m.idx >= len(m.responses) {
		return "DONE", nil
	}
	r := m.responses[m.idx]
	m.idx++
	return r, nil
}

func (m *sequenceMock) GenerateWithSystem(_ context.Context, system, prompt string) (string, error) {
	return m.Generate(nil, system+"\n"+prompt)
}
