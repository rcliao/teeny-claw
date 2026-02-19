package toolreg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rcliao/teeny-claw/pkg/llm"
)

type todoStore struct {
	Items []todoItem `json:"items"`
}

type todoItem struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	Position  int    `json:"position"`
	Notes     string `json:"notes,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// RegisterInternalTodoTools registers todo-mgmt commands as in-process tools.
func RegisterInternalTodoTools(reg *Registry, workspace string) {
	filePath := filepath.Join(workspace, ".teeny-claw", "todos.json")

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.add",
		Description: "[todo-mgmt] Add a task",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":    map[string]any{"type": "string", "description": "Task summary"},
				"summary": map[string]any{"type": "string", "description": "Alias of text"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		summary := firstNonEmpty(args, "text", "summary")
		if summary == "" {
			return "", fmt.Errorf("todo-mgmt.add failed: missing required field: text")
		}

		store, err := loadTodoStore(filePath)
		if err != nil {
			return "", fmt.Errorf("todo-mgmt.add failed: %w", err)
		}

		now := nowRFC3339()
		item := todoItem{
			ID:        newTodoID(),
			Summary:   summary,
			Status:    "todo",
			Position:  nextTodoPosition(store.Items),
			CreatedAt: now,
			UpdatedAt: now,
		}
		store.Items = append(store.Items, item)

		if err := saveTodoStore(filePath, store); err != nil {
			return "", fmt.Errorf("todo-mgmt.add failed: %w", err)
		}

		return toJSON(item)
	})

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.list",
		Description: "[todo-mgmt] List tasks",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"all":    map[string]any{"type": "boolean", "description": "Include done/skipped tasks"},
				"status": map[string]any{"type": "string", "description": "Filter by status: todo, active, done, skipped"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		store, err := loadTodoStore(filePath)
		if err != nil {
			return "", fmt.Errorf("todo-mgmt.list failed: %w", err)
		}

		all := asBool(args["all"])
		status := firstNonEmpty(args, "status")

		filtered := filterTodos(store.Items, all, status)
		return toJSON(filtered)
	})

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.next",
		Description: "[todo-mgmt] Show the next task to work on",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		store, err := loadTodoStore(filePath)
		if err != nil {
			return "", fmt.Errorf("todo-mgmt.next failed: %w", err)
		}

		next := nextTodo(store.Items)
		if next == nil {
			return "null", nil
		}
		return toJSON(next)
	})

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.done",
		Description: "[todo-mgmt] Mark a task as done",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Task ID or prefix"},
			},
			"required": []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return setTodoStatus(filePath, args, "done", "todo-mgmt.done")
	})

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.skip",
		Description: "[todo-mgmt] Skip a task",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Task ID or prefix"},
			},
			"required": []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return setTodoStatus(filePath, args, "skipped", "todo-mgmt.skip")
	})

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.note",
		Description: "[todo-mgmt] Add a note to a task",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "string", "description": "Task ID or prefix"},
				"text": map[string]any{"type": "string", "description": "Note text"},
				"note": map[string]any{"type": "string", "description": "Alias of text"},
			},
			"required": []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(fmt.Sprintf("%v", args["id"]))
		if id == "" {
			return "", fmt.Errorf("todo-mgmt.note failed: missing required field: id")
		}
		note := firstNonEmpty(args, "text", "note")
		if note == "" {
			return "", fmt.Errorf("todo-mgmt.note failed: missing required field: text")
		}

		store, err := loadTodoStore(filePath)
		if err != nil {
			return "", fmt.Errorf("todo-mgmt.note failed: %w", err)
		}
		idx, task, err := resolveTodoByID(store.Items, id)
		if err != nil {
			return "", fmt.Errorf("todo-mgmt.note failed: %w", err)
		}
		if task.Notes != "" {
			task.Notes += "\n"
		}
		task.Notes += note
		task.UpdatedAt = nowRFC3339()
		store.Items[idx] = *task

		if err := saveTodoStore(filePath, store); err != nil {
			return "", fmt.Errorf("todo-mgmt.note failed: %w", err)
		}
		return toJSON(task)
	})

	reg.RegisterInternal(llm.ToolDef{
		Name:        "todo-mgmt.clear",
		Description: "[todo-mgmt] Remove all done/skipped tasks",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		store, err := loadTodoStore(filePath)
		if err != nil {
			return "", fmt.Errorf("todo-mgmt.clear failed: %w", err)
		}

		before := len(store.Items)
		kept := make([]todoItem, 0, len(store.Items))
		for _, item := range store.Items {
			if item.Status == "done" || item.Status == "skipped" {
				continue
			}
			kept = append(kept, item)
		}
		store.Items = kept

		if err := saveTodoStore(filePath, store); err != nil {
			return "", fmt.Errorf("todo-mgmt.clear failed: %w", err)
		}

		return fmt.Sprintf("Cleared %d tasks", before-len(kept)), nil
	})
}

func setTodoStatus(path string, args map[string]any, status, opname string) (string, error) {
	id := strings.TrimSpace(fmt.Sprintf("%v", args["id"]))
	if id == "" {
		return "", fmt.Errorf("%s failed: missing required field: id", opname)
	}

	store, err := loadTodoStore(path)
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", opname, err)
	}
	idx, task, err := resolveTodoByID(store.Items, id)
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", opname, err)
	}
	task.Status = status
	task.UpdatedAt = nowRFC3339()
	store.Items[idx] = *task

	if err := saveTodoStore(path, store); err != nil {
		return "", fmt.Errorf("%s failed: %w", opname, err)
	}
	return toJSON(task)
}

func resolveTodoByID(items []todoItem, id string) (int, *todoItem, error) {
	for i := range items {
		if items[i].ID == id {
			cp := items[i]
			return i, &cp, nil
		}
	}

	matches := make([]int, 0, 2)
	for i := range items {
		if strings.HasPrefix(items[i].ID, id) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return -1, nil, fmt.Errorf("task not found: %s", id)
	}
	if len(matches) > 1 {
		return -1, nil, fmt.Errorf("ambiguous ID prefix %q matches %d tasks", id, len(matches))
	}
	idx := matches[0]
	cp := items[idx]
	return idx, &cp, nil
}

func filterTodos(items []todoItem, all bool, status string) []todoItem {
	out := make([]todoItem, 0, len(items))
	for _, item := range items {
		if all {
			out = append(out, item)
			continue
		}
		if status != "" {
			if item.Status == status {
				out = append(out, item)
			}
			continue
		}
		if item.Status == "todo" || item.Status == "active" {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func nextTodo(items []todoItem) *todoItem {
	filtered := filterTodos(items, false, "todo")
	if len(filtered) == 0 {
		return nil
	}
	cp := filtered[0]
	return &cp
}

func nextTodoPosition(items []todoItem) int {
	maxPos := -1
	for _, item := range items {
		if item.Status == "todo" && item.Position > maxPos {
			maxPos = item.Position
		}
	}
	return maxPos + 1
}

func firstNonEmpty(args map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := args[k]
		if !ok {
			continue
		}
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func asBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newTodoID() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func loadTodoStore(path string) (*todoStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &todoStore{}, nil
		}
		return nil, err
	}

	var s todoStore
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Items == nil {
		s.Items = []todoItem{}
	}
	return &s, nil
}

func saveTodoStore(path string, s *todoStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func toJSON(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
