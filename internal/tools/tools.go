// Package tools provides a registry and execution framework for agent tools.
package tools

import (
	"context"
	"fmt"
	"sync"
)

// Result holds the outcome of a tool execution.
type Result struct {
	Output string
	Error  error
}

// OK returns true if the tool executed without error.
func (r Result) OK() bool { return r.Error == nil }

// Tool is the interface that all tools implement.
type Tool interface {
	// Name returns the tool's identifier.
	Name() string
	// Description returns a human-readable description.
	Description() string
	// Execute runs the tool with the given input.
	Execute(ctx context.Context, input string) Result
}

// Registry manages available tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	return nil
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tool names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Execute runs a tool by name with the given input.
func (r *Registry) Execute(ctx context.Context, name, input string) Result {
	t, ok := r.Get(name)
	if !ok {
		return Result{Error: fmt.Errorf("unknown tool: %s", name)}
	}
	return t.Execute(ctx, input)
}
