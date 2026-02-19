package process

import (
	"testing"
)

func TestTaskQueueAddAndList(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	id1 := q.Add("implement search", "/tmp")
	id2 := q.Add("write tests", "/tmp")

	if id1 == id2 {
		t.Fatal("IDs should be unique")
	}

	all := q.List("")
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}

	pending := q.List(TaskPending)
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
}

func TestTaskQueueGet(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	id := q.Add("test task", "")

	task, ok := q.Get(id)
	if !ok {
		t.Fatal("task not found")
	}
	if task.Prompt != "test task" {
		t.Errorf("prompt = %q", task.Prompt)
	}
	if task.Status != TaskPending {
		t.Errorf("status = %q", task.Status)
	}
}

func TestTaskQueueClaimNext(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	q.Add("first", "")
	q.Add("second", "")

	task, ok := q.ClaimNext()
	if !ok {
		t.Fatal("expected to claim a task")
	}
	if task.Prompt != "first" {
		t.Errorf("claimed wrong task: %q", task.Prompt)
	}
	if task.Status != TaskRunning {
		t.Errorf("claimed task status = %q", task.Status)
	}

	// Next claim should return second
	task2, ok := q.ClaimNext()
	if !ok {
		t.Fatal("expected to claim second task")
	}
	if task2.Prompt != "second" {
		t.Errorf("claimed wrong task: %q", task2.Prompt)
	}

	// No more pending
	_, ok = q.ClaimNext()
	if ok {
		t.Fatal("should have no more pending tasks")
	}
}

func TestTaskQueueComplete(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	id := q.Add("complete me", "")

	task, _ := q.ClaimNext()
	q.Complete(task.ID, "done output")

	got, ok := q.Get(id)
	if !ok {
		t.Fatal("task not found")
	}
	if got.Status != TaskDone {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.Output != "done output" {
		t.Errorf("output = %q", got.Output)
	}
}

func TestTaskQueueFail(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	id := q.Add("fail me", "")

	task, _ := q.ClaimNext()
	q.Fail(task.ID, "something broke")

	got, _ := q.Get(id)
	if got.Status != TaskFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.Error != "something broke" {
		t.Errorf("error = %q", got.Error)
	}
}

func TestTaskQueueCancel(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	id := q.Add("cancel me", "")

	if err := q.Cancel(id); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	got, _ := q.Get(id)
	if got.Status != TaskCanceled {
		t.Errorf("status = %q, want canceled", got.Status)
	}

	// Double cancel should error
	if err := q.Cancel(id); err == nil {
		t.Fatal("expected error on double cancel")
	}
}

func TestTaskQueueCancelNotFound(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	if err := q.Cancel("nonexistent"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskQueueLogs(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	id := q.Add("log task", "")
	task, _ := q.ClaimNext()
	q.Complete(task.ID, "hello output")

	logs, err := q.Logs(id)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if logs != "hello output" {
		t.Errorf("logs = %q", logs)
	}
}

func TestTaskQueueLogsNotFound(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	_, err := q.Logs("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskQueuePersistence(t *testing.T) {
	dir := t.TempDir()
	q := NewTaskQueue(dir)
	q.Add("persisted task", "/tmp")

	// Reload
	q2 := NewTaskQueue(dir)
	all := q2.List("")
	if len(all) != 1 {
		t.Fatalf("expected 1 task after reload, got %d", len(all))
	}
	if all[0].Prompt != "persisted task" {
		t.Errorf("prompt = %q", all[0].Prompt)
	}
}

func TestTaskQueueFilterByStatus(t *testing.T) {
	q := NewTaskQueue(t.TempDir())
	q.Add("a", "")
	q.Add("b", "")
	q.ClaimNext() // claim "a" → running

	pending := q.List(TaskPending)
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}

	running := q.List(TaskRunning)
	if len(running) != 1 {
		t.Errorf("expected 1 running, got %d", len(running))
	}
}
