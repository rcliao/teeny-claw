package process

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskStatus represents the state of a queued task.
type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskRunning  TaskStatus = "running"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
)

// QueuedTask represents a task in the queue.
type QueuedTask struct {
	ID        string     `json:"id"`
	Prompt    string     `json:"prompt"`
	Status    TaskStatus `json:"status"`
	Output    string     `json:"output,omitempty"`
	WorkDir   string     `json:"working_dir,omitempty"`
	Error     string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt time.Time  `json:"started_at,omitempty"`
	EndedAt   time.Time  `json:"ended_at,omitempty"`
}

// TaskQueue manages a persistent queue of tasks.
type TaskQueue struct {
	tasks []QueuedTask
	mu    sync.RWMutex
	path  string
	nextID int
}

// NewTaskQueue loads or creates a task queue backed by a JSON file.
func NewTaskQueue(dir string) *TaskQueue {
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "tasks.json")
	q := &TaskQueue{path: path, nextID: 1}
	q.load()
	return q
}

// Add queues a new task and returns its ID.
func (q *TaskQueue) Add(prompt, workDir string) string {
	q.mu.Lock()
	defer q.mu.Unlock()

	id := fmt.Sprintf("task-%d", q.nextID)
	q.nextID++

	task := QueuedTask{
		ID:        id,
		Prompt:    prompt,
		Status:    TaskPending,
		WorkDir:   workDir,
		CreatedAt: time.Now(),
	}
	q.tasks = append(q.tasks, task)
	q.save()
	return id
}

// List returns all tasks, optionally filtered by status.
func (q *TaskQueue) List(status TaskStatus) []QueuedTask {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var out []QueuedTask
	for _, t := range q.tasks {
		if status == "" || t.Status == status {
			out = append(out, t)
		}
	}
	return out
}

// Get returns a task by ID.
func (q *TaskQueue) Get(id string) (QueuedTask, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, t := range q.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return QueuedTask{}, false
}

// ClaimNext atomically claims the next pending task for execution.
func (q *TaskQueue) ClaimNext() (QueuedTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.tasks {
		if q.tasks[i].Status == TaskPending {
			q.tasks[i].Status = TaskRunning
			q.tasks[i].StartedAt = time.Now()
			q.save()
			return q.tasks[i], true
		}
	}
	return QueuedTask{}, false
}

// Complete marks a task as done with output.
func (q *TaskQueue) Complete(id, output string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.tasks {
		if q.tasks[i].ID == id {
			q.tasks[i].Status = TaskDone
			q.tasks[i].Output = output
			q.tasks[i].EndedAt = time.Now()
			q.save()
			return
		}
	}
}

// Fail marks a task as failed with an error.
func (q *TaskQueue) Fail(id, errMsg string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.tasks {
		if q.tasks[i].ID == id {
			q.tasks[i].Status = TaskFailed
			q.tasks[i].Error = errMsg
			q.tasks[i].EndedAt = time.Now()
			q.save()
			return
		}
	}
}

// Cancel marks a pending or running task as canceled.
func (q *TaskQueue) Cancel(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.tasks {
		if q.tasks[i].ID == id {
			if q.tasks[i].Status == TaskDone || q.tasks[i].Status == TaskCanceled {
				return fmt.Errorf("task %q is already %s", id, q.tasks[i].Status)
			}
			q.tasks[i].Status = TaskCanceled
			q.tasks[i].EndedAt = time.Now()
			q.save()
			return nil
		}
	}
	return fmt.Errorf("task %q not found", id)
}

// Logs returns the output of a task.
func (q *TaskQueue) Logs(id string) (string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, t := range q.tasks {
		if t.ID == id {
			if t.Output != "" {
				return t.Output, nil
			}
			if t.Error != "" {
				return "ERROR: " + t.Error, nil
			}
			return fmt.Sprintf("Task %s is %s (no output yet)", id, t.Status), nil
		}
	}
	return "", fmt.Errorf("task %q not found", id)
}

type taskStore struct {
	Tasks  []QueuedTask `json:"tasks"`
	NextID int          `json:"next_id"`
}

func (q *TaskQueue) load() {
	data, err := os.ReadFile(q.path)
	if err != nil {
		return
	}
	var store taskStore
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}
	q.tasks = store.Tasks
	q.nextID = store.NextID
	if q.nextID == 0 {
		q.nextID = len(q.tasks) + 1
	}
}

func (q *TaskQueue) save() {
	store := taskStore{Tasks: q.tasks, NextID: q.nextID}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(q.path, data, 0644)
}
