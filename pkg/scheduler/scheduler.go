// Package scheduler provides cron-based job scheduling for the daemon.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Job defines a scheduled task.
type Job struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"` // cron expression or "@every 30m"
	Prompt   string `json:"prompt"`
	Session  string `json:"session"`
	Enabled  bool   `json:"enabled"`
}

// RunFunc is called when a job fires. It receives the job's prompt and session key.
type RunFunc func(ctx context.Context, sessionKey, prompt string) (string, error)

// Scheduler manages and runs scheduled jobs.
type Scheduler struct {
	jobs    []Job
	runFn   RunFunc
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	verbose bool
}

// New creates a scheduler with the given jobs and run function.
func New(jobs []Job, runFn RunFunc, verbose bool) *Scheduler {
	return &Scheduler{
		jobs:    jobs,
		runFn:   runFn,
		verbose: verbose,
	}
}

// Start begins the scheduler loop. It checks jobs every 30 seconds.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.running = true
	s.mu.Unlock()

	go s.loop(ctx)
}

// Stop halts the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
}

// Running returns whether the scheduler is active.
func (s *Scheduler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) loop(ctx context.Context) {
	// Track last run time per job to avoid double-firing
	lastRun := make(map[string]time.Time)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Check immediately on start
	s.checkJobs(ctx, lastRun)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkJobs(ctx, lastRun)
		}
	}
}

func (s *Scheduler) checkJobs(ctx context.Context, lastRun map[string]time.Time) {
	now := time.Now()
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		last, ok := lastRun[job.Name]
		if ok && !shouldRun(job.Schedule, last, now) {
			continue
		}
		if !ok && !shouldRunInitial(job.Schedule, now) {
			// For interval-based, run on first check; for cron, check alignment
			lastRun[job.Name] = now
			continue
		}

		lastRun[job.Name] = now
		go s.runJob(ctx, job)
	}
}

func (s *Scheduler) runJob(ctx context.Context, job Job) {
	if s.verbose {
		log.Printf("[scheduler] running job %q session=%s", job.Name, job.Session)
	}

	result, err := s.runFn(ctx, job.Session, job.Prompt)
	if err != nil {
		log.Printf("[scheduler] job %q error: %v", job.Name, err)
		return
	}

	if s.verbose {
		log.Printf("[scheduler] job %q done: %s", job.Name, truncate(result, 200))
	}
}

// shouldRun checks if a job should run based on schedule and last run time.
func shouldRun(schedule string, last, now time.Time) bool {
	// Try interval first
	if interval, err := parseInterval(schedule); err == nil {
		return now.Sub(last) >= interval
	}

	// Try cron expression
	if cron, err := ParseCron(schedule); err == nil {
		// Only fire if current minute matches AND we haven't run this minute
		truncNow := now.Truncate(time.Minute)
		truncLast := last.Truncate(time.Minute)
		return cron.Matches(now) && truncNow.After(truncLast)
	}

	return false
}

// shouldRunInitial returns true for first-time check.
func shouldRunInitial(schedule string, now time.Time) bool {
	if _, err := parseInterval(schedule); err == nil {
		return true
	}
	if cron, err := ParseCron(schedule); err == nil {
		return cron.Matches(now)
	}
	return false
}

// parseInterval parses "@every 30m" style schedules.
func parseInterval(schedule string) (time.Duration, error) {
	if len(schedule) > 7 && schedule[:7] == "@every " {
		return time.ParseDuration(schedule[7:])
	}
	return 0, fmt.Errorf("unsupported schedule format: %s (use @every <duration>)", schedule)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// DaemonConfig holds daemon configuration.
type DaemonConfig struct {
	Jobs    []Job  `json:"jobs"`
	PidFile string `json:"pid_file,omitempty"`
}

// LoadDaemonConfig loads daemon config from a JSON file.
func LoadDaemonConfig(path string) (*DaemonConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg DaemonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
