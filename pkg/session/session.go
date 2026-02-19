// Package session manages conversation history persistence.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rcliao/teeny-claw/pkg/llm"
)

// Session holds conversation state.
type Session struct {
	Key      string        `json:"key"`
	Messages []llm.Message `json:"messages"`
	Summary  string        `json:"summary,omitempty"`
	Created  time.Time     `json:"created"`
	Updated  time.Time     `json:"updated"`
}

// Manager handles session CRUD and persistence.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	dir      string
}

// NewManager creates a session manager backed by a directory.
func NewManager(dir string) *Manager {
	os.MkdirAll(dir, 0755)
	m := &Manager{
		sessions: make(map[string]*Session),
		dir:      dir,
	}
	m.loadAll()
	return m
}

// GetHistory returns message history for a session.
func (m *Manager) GetHistory(key string) []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[key]
	if !ok {
		return nil
	}
	return cloneMessages(s.Messages)
}

// GetSummary returns the session's compaction summary.
func (m *Manager) GetSummary(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if s, ok := m.sessions[key]; ok {
		return s.Summary
	}
	return ""
}

// AddMessage appends a message to a session.
func (m *Manager) AddMessage(key string, msg llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.getOrCreate(key)
	s.Messages = append(s.Messages, msg)
	s.Updated = time.Now()
}

// SetSummary sets the compaction summary and truncates history.
func (m *Manager) SetSummary(key string, summary string, keepLast int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.getOrCreate(key)
	s.Summary = summary
	if keepLast > 0 && len(s.Messages) > keepLast {
		s.Messages = s.Messages[len(s.Messages)-keepLast:]
	}
	s.Updated = time.Now()
}

// MessageCount returns how many messages are in a session.
func (m *Manager) MessageCount(key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if s, ok := m.sessions[key]; ok {
		return len(s.Messages)
	}
	return 0
}

// Save persists a session to disk.
func (m *Manager) Save(key string) error {
	m.mu.RLock()
	s, ok := m.sessions[key]
	if !ok {
		m.mu.RUnlock()
		return nil
	}
	// Snapshot
	snapshot := Session{
		Key:      s.Key,
		Summary:  s.Summary,
		Created:  s.Created,
		Updated:  s.Updated,
		Messages: cloneMessages(s.Messages),
	}
	m.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	filename := sanitize(key) + ".json"
	path := filepath.Join(m.dir, filename)

	// Atomic write
	tmp, err := os.CreateTemp(m.dir, "session-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	return os.Rename(tmpPath, path)
}

func (m *Manager) getOrCreate(key string) *Session {
	s, ok := m.sessions[key]
	if !ok {
		s = &Session{
			Key:     key,
			Created: time.Now(),
			Updated: time.Now(),
		}
		m.sessions[key] = s
	}
	return s
}

func (m *Manager) loadAll() {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		m.sessions[s.Key] = &s
	}
}

func sanitize(key string) string {
	return strings.ReplaceAll(key, ":", "_")
}

func cloneMessages(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = llm.Message{Role: m.Role}
		out[i].Content = make([]llm.ContentPart, len(m.Content))
		copy(out[i].Content, m.Content)
	}
	return out
}
