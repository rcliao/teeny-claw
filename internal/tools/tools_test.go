package tools

import (
	"context"
	"strings"
	"testing"
)

// mockTool is a simple tool for testing.
type mockTool struct {
	name   string
	output string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "mock" }
func (m *mockTool) Execute(_ context.Context, input string) Result {
	return Result{Output: m.output + ": " + input}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	tool := &mockTool{name: "test", output: "ok"}

	if err := reg.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := reg.Get("test")
	if !ok {
		t.Fatal("expected tool to be found")
	}
	if got.Name() != "test" {
		t.Errorf("expected name=test, got %s", got.Name())
	}
}

func TestRegistryDuplicate(t *testing.T) {
	reg := NewRegistry()
	tool := &mockTool{name: "dup"}

	reg.Register(tool)
	err := reg.Register(tool)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistryExecute(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "echo", output: "echoed"})

	result := reg.Execute(context.Background(), "echo", "hello")
	if !result.OK() {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output != "echoed: hello" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestRegistryExecuteUnknown(t *testing.T) {
	reg := NewRegistry()
	result := reg.Execute(context.Background(), "nope", "")
	if result.OK() {
		t.Error("expected error for unknown tool")
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "a"})
	reg.Register(&mockTool{name: "b"})

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(names))
	}
}

func TestShellExecute(t *testing.T) {
	shell := &Shell{}
	result := shell.Execute(context.Background(), "echo hello")
	if !result.OK() {
		t.Fatalf("shell error: %v", result.Error)
	}
	if strings.TrimSpace(result.Output) != "hello" {
		t.Errorf("expected 'hello', got %q", result.Output)
	}
}

func TestShellError(t *testing.T) {
	shell := &Shell{}
	result := shell.Execute(context.Background(), "exit 1")
	if result.OK() {
		t.Error("expected error for exit 1")
	}
}
