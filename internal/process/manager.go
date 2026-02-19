// Package process manages long-lived claude CLI processes for background tasks.
package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Status represents the state of a managed process.
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
	StatusKilled   Status = "killed"
	StatusTimedOut Status = "timed_out"
)

// SpawnOpts configures a new process.
type SpawnOpts struct {
	ID      string
	Model   string
	Prompt  string
	WorkDir string
	Timeout time.Duration
}

// Session represents a managed claude CLI process.
type Session struct {
	ID        string
	Status    Status
	Output    strings.Builder
	StartedAt time.Time
	EndedAt   time.Time
	Error     error

	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
}

// Manager spawns and manages claude CLI processes.
type Manager struct {
	sessions      map[string]*Session
	mu            sync.RWMutex
	maxConcurrent int
	binary        string
}

// NewManager creates a process manager.
func NewManager(maxConcurrent int) *Manager {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &Manager{
		sessions:      make(map[string]*Session),
		maxConcurrent: maxConcurrent,
		binary:        "claude",
	}
}

// Spawn starts a new claude CLI process.
func (m *Manager) Spawn(ctx context.Context, opts SpawnOpts) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check concurrency limit
	running := 0
	for _, s := range m.sessions {
		if s.Status == StatusRunning {
			running++
		}
	}
	if running >= m.maxConcurrent {
		return nil, fmt.Errorf("max concurrent processes (%d) reached", m.maxConcurrent)
	}

	if _, exists := m.sessions[opts.ID]; exists {
		return nil, fmt.Errorf("session %q already exists", opts.ID)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	procCtx, cancel := context.WithTimeout(ctx, timeout)

	model := opts.Model
	if model == "" {
		model = "sonnet"
	}

	args := []string{"-p", opts.Prompt, "--model", model, "--output-format", "text"}
	cmd := exec.CommandContext(procCtx, m.binary, args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	sess := &Session{
		ID:        opts.ID,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
	}
	m.sessions[opts.ID] = sess

	// Capture output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		sess.Status = StatusFailed
		sess.Error = err
		sess.EndedAt = time.Now()
		return sess, fmt.Errorf("start process: %w", err)
	}

	// Read output in background
	go func() {
		defer cancel()
		m.captureOutput(sess, stdout, stderr)
		err := cmd.Wait()

		sess.mu.Lock()
		defer sess.mu.Unlock()
		sess.EndedAt = time.Now()

		if procCtx.Err() == context.DeadlineExceeded {
			sess.Status = StatusTimedOut
			sess.Error = fmt.Errorf("process timed out after %s", timeout)
		} else if err != nil {
			sess.Status = StatusFailed
			sess.Error = err
		} else {
			sess.Status = StatusDone
		}
	}()

	return sess, nil
}

func (m *Manager) captureOutput(sess *Session, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	readInto := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			sess.mu.Lock()
			sess.Output.WriteString(scanner.Text())
			sess.Output.WriteString("\n")
			sess.mu.Unlock()
		}
	}

	go readInto(stdout)
	go readInto(stderr)
	wg.Wait()
}

// Kill terminates a running process.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.Status != StatusRunning {
		return fmt.Errorf("session %q is not running (status: %s)", id, sess.Status)
	}

	sess.cancel()
	sess.Status = StatusKilled
	sess.EndedAt = time.Now()
	return nil
}

// ReadOutput returns the current output of a session.
func (m *Manager) ReadOutput(id string) (string, error) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("session %q not found", id)
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.Output.String(), nil
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List returns all session IDs and statuses.
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var infos []SessionInfo
	for _, s := range m.sessions {
		s.mu.Lock()
		infos = append(infos, SessionInfo{
			ID:        s.ID,
			Status:    s.Status,
			StartedAt: s.StartedAt,
			EndedAt:   s.EndedAt,
		})
		s.mu.Unlock()
	}
	return infos
}

// SessionInfo is a snapshot of session metadata.
type SessionInfo struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// Wait blocks until a session completes.
func (m *Manager) Wait(id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}

	// Poll until done
	for {
		sess.mu.Lock()
		st := sess.Status
		sess.mu.Unlock()

		if st != StatusRunning {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}
