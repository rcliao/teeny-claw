package toolreg

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rcliao/teeny-claw/pkg/llm"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry(0)
	if r == nil {
		t.Fatal("nil registry")
	}
	if r.timeout.Seconds() != 30 {
		t.Fatalf("expected 30s timeout, got %v", r.timeout)
	}
}

func TestRegisterAndToToolDefs(t *testing.T) {
	r := NewRegistry(0)
	r.Register(&ToolManifest{
		Name:        "test-tool",
		Binary:      "echo",
		Description: "A test",
		Commands: map[string]CommandDef{
			"hello": {Description: "Say hello", Parameters: map[string]ParameterDef{}},
			"bye":   {Description: "Say bye", Parameters: map[string]ParameterDef{}},
		},
	})

	defs := r.ToToolDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["test-tool.hello"] || !names["test-tool.bye"] {
		t.Fatalf("missing expected tool defs: %v", names)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	toolDir := filepath.Join(dir, "my-tool")
	os.MkdirAll(toolDir, 0755)
	os.WriteFile(filepath.Join(toolDir, "tool.json"), []byte(`{
		"name": "my-tool",
		"binary": "echo",
		"description": "test",
		"commands": {
			"run": {"description": "run it", "parameters": {}}
		}
	}`), 0644)

	r := NewRegistry(0)
	if err := r.Discover([]string{dir}); err != nil {
		t.Fatalf("discover: %v", err)
	}

	defs := r.ToToolDefs()
	if len(defs) != 1 || defs[0].Name != "my-tool.run" {
		t.Fatalf("unexpected defs: %+v", defs)
	}
}

func TestDiscoverMissingDir(t *testing.T) {
	r := NewRegistry(0)
	if err := r.Discover([]string{"/nonexistent"}); err != nil {
		t.Fatalf("should skip missing dirs, got: %v", err)
	}
}

func TestExecuteEcho(t *testing.T) {
	r := NewRegistry(0)
	r.Register(&ToolManifest{
		Name:   "test",
		Binary: "echo",
		Commands: map[string]CommandDef{
			"greet": {
				Description: "greet",
				Args:        "hello world",
				Parameters:  map[string]ParameterDef{},
			},
		},
	})

	out, err := r.Execute(context.Background(), "test.greet", `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "greet hello world\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	r := NewRegistry(0)
	_, err := r.Execute(context.Background(), "nope.cmd", `{}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteInvalidName(t *testing.T) {
	r := NewRegistry(0)
	_, err := r.Execute(context.Background(), "nope", `{}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildJSONSchema(t *testing.T) {
	schema := buildJSONSchema(map[string]ParameterDef{
		"name": {Type: "string", Description: "Name", Required: true},
		"age":  {Type: "integer", Description: "Age", Required: false, Default: 25},
	})

	if schema["type"] != "object" {
		t.Fatalf("expected object type")
	}
	props := schema["properties"].(map[string]any)
	if len(props) != 2 {
		t.Fatalf("expected 2 properties")
	}
	req := schema["required"].([]string)
	if len(req) != 1 || req[0] != "name" {
		t.Fatalf("unexpected required: %v", req)
	}
}

func TestExecuteInternalTool(t *testing.T) {
	r := NewRegistry(0)
	r.RegisterInternal(llm.ToolDef{
		Name:        "internal.echo",
		Description: "echo",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "ok:" + args["text"].(string), nil
	})

	out, err := r.Execute(context.Background(), "internal.echo", `{"text":"hello"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "ok:hello" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestInternalToolOverridesExternalBinary(t *testing.T) {
	r := NewRegistry(0)
	r.Register(&ToolManifest{
		Name:   "todo-mgmt",
		Binary: "definitely-not-installed-binary",
		Commands: map[string]CommandDef{
			"list": {Description: "list", Parameters: map[string]ParameterDef{}},
		},
	})
	r.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.list",
		Description: "internal list",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "internal-list", nil
	})

	out, err := r.Execute(context.Background(), "todo-mgmt.list", `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "internal-list" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRegisterInternalTodoTools(t *testing.T) {
	r := NewRegistry(0)
	workspace := t.TempDir()
	RegisterInternalTodoTools(r, workspace)

	add1, err := r.Execute(context.Background(), "todo-mgmt.add", `{"text":"claude-cli tool loop validation"}`)
	if err != nil {
		t.Fatalf("add todo 1: %v", err)
	}
	if !strings.Contains(add1, "\"summary\": \"claude-cli tool loop validation\"") {
		t.Fatalf("unexpected add output: %q", add1)
	}

	add2, err := r.Execute(context.Background(), "todo-mgmt.add", `{"text":"second task"}`)
	if err != nil {
		t.Fatalf("add todo 2: %v", err)
	}

	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(add2), &item); err != nil {
		t.Fatalf("unmarshal add2: %v", err)
	}
	if item.ID == "" {
		t.Fatal("empty id")
	}

	listOut, err := r.Execute(context.Background(), "todo-mgmt.list", `{}`)
	if err != nil {
		t.Fatalf("list todos: %v", err)
	}
	if !strings.Contains(listOut, "claude-cli tool loop validation") || !strings.Contains(listOut, "second task") {
		t.Fatalf("expected todos in output, got: %q", listOut)
	}

	nextOut, err := r.Execute(context.Background(), "todo-mgmt.next", `{}`)
	if err != nil {
		t.Fatalf("next todo: %v", err)
	}
	if !strings.Contains(nextOut, "claude-cli tool loop validation") {
		t.Fatalf("unexpected next output: %q", nextOut)
	}

	noteOut, err := r.Execute(context.Background(), "todo-mgmt.note", `{"id":"`+item.ID[:8]+`","text":"remember edge cases"}`)
	if err != nil {
		t.Fatalf("note todo: %v", err)
	}
	if !strings.Contains(noteOut, "remember edge cases") {
		t.Fatalf("unexpected note output: %q", noteOut)
	}

	doneOut, err := r.Execute(context.Background(), "todo-mgmt.done", `{"id":"`+item.ID[:8]+`"}`)
	if err != nil {
		t.Fatalf("done todo: %v", err)
	}
	if !strings.Contains(doneOut, `"status": "done"`) {
		t.Fatalf("unexpected done output: %q", doneOut)
	}

	allOut, err := r.Execute(context.Background(), "todo-mgmt.list", `{"all":true}`)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if !strings.Contains(allOut, `"status": "done"`) {
		t.Fatalf("expected done task in list all, got: %q", allOut)
	}

	clearOut, err := r.Execute(context.Background(), "todo-mgmt.clear", `{}`)
	if err != nil {
		t.Fatalf("clear todos: %v", err)
	}
	if !strings.Contains(clearOut, "Cleared 1 tasks") {
		t.Fatalf("unexpected clear output: %q", clearOut)
	}
}
