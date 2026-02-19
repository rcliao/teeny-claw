package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rcliao/teeny-claw/internal/agent"
	"github.com/rcliao/teeny-claw/internal/process"
	"github.com/rcliao/teeny-claw/internal/wire"
	"github.com/rcliao/teeny-claw/pkg/eval"
	"github.com/rcliao/teeny-claw/pkg/llm"
	"github.com/rcliao/teeny-claw/pkg/scheduler"

	"github.com/spf13/cobra"
)

func defaultConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".teeny-claw")
}

func loadConfig() wire.AppConfig {
	configDir := defaultConfigDir()
	cfg := wire.AppConfig{
		Workspace: filepath.Join(configDir, "workspace"),
		Provider: wire.ProviderConfig{
			Name:      "anthropic",
			Model:     "claude-sonnet-4-20250514",
			APIKeyEnv: "ANTHROPIC_API_KEY",
		},
		Tools: wire.ToolsConfig{
			Path:    []string{filepath.Join(configDir, "tools")},
			Timeout: 30,
		},
		Session: wire.SessionConfig{
			Dir: filepath.Join(configDir, "sessions"),
		},
	}

	// Try to load config file
	configPath := filepath.Join(configDir, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	return cfg
}

func isClaudeCLIProvider(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "claude-cli" || n == "claude_code" || n == "claudecode"
}

func claudeCLIAutonomousAllowed(cfg wire.AppConfig) bool {
	if cfg.Provider.AllowAutonomous {
		return true
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TEENY_ALLOW_CLAUDE_CLI_AUTONOMOUS")))
	return v == "1" || v == "true" || v == "yes"
}

func buildComponents(cfg wire.AppConfig, verbose bool, runMode string) (*wire.Components, error) {
	// Provider guardrails
	if isClaudeCLIProvider(cfg.Provider.Name) && runMode == "daemon" && !claudeCLIAutonomousAllowed(cfg) {
		return nil, fmt.Errorf("provider %q is blocked in daemon/autonomous mode by default; use an API-key provider, or set provider.allow_autonomous=true (or TEENY_ALLOW_CLAUDE_CLI_AUTONOMOUS=1)", cfg.Provider.Name)
	}
	if isClaudeCLIProvider(cfg.Provider.Name) && verbose {
		fmt.Fprintln(os.Stderr, "[warn] using claude-cli backend; verify account policy/ToS for your usage mode")
	}

	cfg.Provider.Verbose = verbose
	return wire.Build(cfg)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func main() {
	var verbose bool
	var sessionKey string

	rootCmd := &cobra.Command{
		Use:   "teeny",
		Short: "Lightweight autonomous agent runtime",
	}
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().StringVarP(&sessionKey, "session", "s", "main", "Session key")

	// init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize workspace and config",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := defaultConfigDir()
			dirs := []string{
				configDir,
				filepath.Join(configDir, "workspace"),
				filepath.Join(configDir, "sessions"),
				filepath.Join(configDir, "tools"),
			}
			for _, d := range dirs {
				os.MkdirAll(d, 0755)
				fmt.Printf("Created %s\n", d)
			}

			// Write default config if not exists
			configPath := filepath.Join(configDir, "config.json")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				cfg := loadConfig()
				data, _ := json.MarshalIndent(cfg, "", "  ")
				os.WriteFile(configPath, data, 0644)
				fmt.Printf("Created %s\n", configPath)
			}

			// Write default AGENTS.md
			agentsPath := filepath.Join(configDir, "workspace", "AGENTS.md")
			if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
				os.WriteFile(agentsPath, []byte("# AGENTS.md\n\nYou are an autonomous agent. Use your tools to accomplish tasks.\n"), 0644)
				fmt.Printf("Created %s\n", agentsPath)
			}

			fmt.Println("\nDone! Set ANTHROPIC_API_KEY and run: teeny chat")
			return nil
		},
	}

	// run command
	runCmd := &cobra.Command{
		Use:   "run [message]",
		Short: "Run a one-shot agent task",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			var message string
			if len(args) > 0 {
				message = args[0]
			} else {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				message = strings.TrimSpace(string(data))
			}

			if message == "" {
				return fmt.Errorf("no message provided")
			}

			comp, err := buildComponents(cfg, verbose, "oneshot")
			if err != nil {
				return err
			}

			task := agent.Task{
				ID:          fmt.Sprintf("run-%d", time.Now().Unix()),
				Description: message,
				CreatedAt:   time.Now(),
			}
			result, err := comp.Agent.Run(context.Background(), task)
			if err != nil {
				return err
			}

			fmt.Println(result.Reflection)
			return nil
		},
	}

	// chat command
	chatCmd := &cobra.Command{
		Use:   "chat",
		Short: "Interactive chat session",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			comp, err := buildComponents(cfg, verbose, "interactive")
			if err != nil {
				return err
			}

			fmt.Println("teeny-claw chat (type 'exit' to quit)")
			fmt.Println("---")

			scanner := bufio.NewScanner(os.Stdin)
			turnNum := 0
			for {
				fmt.Print("> ")
				if !scanner.Scan() {
					break
				}
				input := strings.TrimSpace(scanner.Text())
				if input == "" {
					continue
				}
				if input == "exit" || input == "quit" {
					break
				}

				turnNum++
				task := agent.Task{
					ID:          fmt.Sprintf("chat-%s-%d", sessionKey, turnNum),
					Description: input,
					CreatedAt:   time.Now(),
				}

				// Record user message in session
				comp.Sessions.AddMessage(sessionKey, llm.TextMessage(llm.RoleUser, input))

				result, err := comp.Agent.Run(context.Background(), task)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					continue
				}

				// Record assistant response in session
				comp.Sessions.AddMessage(sessionKey, llm.TextMessage(llm.RoleAssistant, result.Reflection))

				fmt.Println(result.Reflection)
				fmt.Println()
			}

			// Save session on exit
			if err := comp.Sessions.Save(sessionKey); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save session: %v\n", err)
			}

			return nil
		},
	}

	// daemon command
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run as background daemon with scheduled jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			daemonCfgPath := filepath.Join(defaultConfigDir(), "daemon.json")
			daemonCfg, err := scheduler.LoadDaemonConfig(daemonCfgPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no daemon config found at %s — create it with job definitions", daemonCfgPath)
				}
				return fmt.Errorf("load daemon config: %w", err)
			}

			if len(daemonCfg.Jobs) == 0 {
				return fmt.Errorf("no jobs defined in %s", daemonCfgPath)
			}

			runFn := func(ctx context.Context, sessKey, prompt string) (string, error) {
				comp, err := buildComponents(cfg, verbose, "daemon")
				if err != nil {
					return "", err
				}
				task := agent.Task{
					ID:          fmt.Sprintf("daemon-%s-%d", sessKey, time.Now().Unix()),
					Description: prompt,
					CreatedAt:   time.Now(),
				}
				result, err := comp.Agent.Run(ctx, task)
				if err != nil {
					return "", err
				}
				return result.Reflection, nil
			}

			sched := scheduler.New(daemonCfg.Jobs, runFn, verbose)

			fmt.Printf("teeny-claw daemon starting with %d job(s)\n", len(daemonCfg.Jobs))
			for _, j := range daemonCfg.Jobs {
				status := "enabled"
				if !j.Enabled {
					status = "disabled"
				}
				fmt.Printf("  - %s (%s) [%s]\n", j.Name, j.Schedule, status)
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// Task queue processing
			queue := process.NewTaskQueue(filepath.Join(defaultConfigDir(), "queue"))
			go func() {
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						task, ok := queue.ClaimNext()
						if !ok {
							continue
						}
						if verbose {
							fmt.Printf("[daemon] processing task %s: %s\n", task.ID, truncateStr(task.Prompt, 60))
						}
						go func(t process.QueuedTask) {
							comp, err := buildComponents(cfg, verbose, "daemon")
							if err != nil {
								queue.Fail(t.ID, err.Error())
								return
							}
							agentTask := agent.Task{
								ID:          t.ID,
								Description: t.Prompt,
								CreatedAt:   t.CreatedAt,
							}
							result, err := comp.Agent.Run(context.Background(), agentTask)
							if err != nil {
								queue.Fail(t.ID, err.Error())
								return
							}
							queue.Complete(t.ID, result.Reflection)
						}(task)
					}
				}
			}()

			sched.Start(ctx)
			<-ctx.Done()
			sched.Stop()
			fmt.Println("\ndaemon stopped")
			return nil
		},
	}

	// job command group
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "Manage daemon jobs",
	}

	jobListCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonCfgPath := filepath.Join(defaultConfigDir(), "daemon.json")
			daemonCfg, err := scheduler.LoadDaemonConfig(daemonCfgPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No daemon config found. Create ~/.teeny-claw/daemon.json")
					return nil
				}
				return err
			}
			data, _ := json.MarshalIndent(daemonCfg.Jobs, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	jobRunCmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Trigger a job immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobName := args[0]
			cfg := loadConfig()

			daemonCfgPath := filepath.Join(defaultConfigDir(), "daemon.json")
			daemonCfg, err := scheduler.LoadDaemonConfig(daemonCfgPath)
			if err != nil {
				return fmt.Errorf("load daemon config: %w", err)
			}

			var job *scheduler.Job
			for i := range daemonCfg.Jobs {
				if daemonCfg.Jobs[i].Name == jobName {
					job = &daemonCfg.Jobs[i]
					break
				}
			}
			if job == nil {
				return fmt.Errorf("job %q not found", jobName)
			}

			comp, err := buildComponents(cfg, verbose, "daemon")
			if err != nil {
				return err
			}

			fmt.Printf("Running job %q (session=%s)...\n", job.Name, job.Session)
			task := agent.Task{
				ID:          fmt.Sprintf("job-%s-%d", job.Name, time.Now().Unix()),
				Description: job.Prompt,
				CreatedAt:   time.Now(),
			}
			result, err := comp.Agent.Run(context.Background(), task)
			if err != nil {
				return err
			}
			fmt.Println(result.Reflection)
			return nil
		},
	}

	// heartbeat command
	heartbeatCmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Run one eval-grounded self-review cycle",
		Long: `Reviews recent LLM call data from token-eval, identifies patterns
(high cost, failures, inefficiencies), and stores learnings in agent-memory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			evalCfg := eval.DefaultConfig()
			evalClient := eval.NewClient(evalCfg)

			summary, err := evalClient.BuildReviewSummary(context.Background())
			if err != nil {
				summary = "token-eval not available — no call data to review."
				if verbose {
					fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				}
			}

			learnings, _ := evalClient.BuildLearningContext(context.Background(), "orchestrator learnings", 10)

			reviewPrompt := fmt.Sprintf(`You are performing a self-review of recent LLM orchestration calls.

%s

%s

## Your Task

1. **Analyze** the recent call data for patterns:
   - Are any calls using excessive tokens? Why?
   - Are there repeated failures or retries?
   - Could any prompts be more efficient?

2. **Synthesize** 1-3 actionable learnings (short, specific, useful).

3. **Report** a brief summary of what you found and stored.

Be concise. Focus on actionable improvements.`, summary, learnings)

			comp, err := buildComponents(cfg, verbose, "daemon")
			if err != nil {
				return err
			}

			task := agent.Task{
				ID:          fmt.Sprintf("heartbeat-%d", time.Now().Unix()),
				Description: reviewPrompt,
				CreatedAt:   time.Now(),
			}
			result, err := comp.Agent.Run(context.Background(), task)
			if err != nil {
				return err
			}

			fmt.Println(result.Reflection)
			return nil
		},
	}

	// task command group — background task queue
	taskCmd := &cobra.Command{
		Use:   "task",
		Short: "Manage background tasks",
	}

	taskAddCmd := &cobra.Command{
		Use:   "add <prompt>",
		Short: "Queue a background task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, _ := cmd.Flags().GetString("workdir")
			queue := process.NewTaskQueue(filepath.Join(defaultConfigDir(), "queue"))
			id := queue.Add(args[0], workDir)
			fmt.Printf("Queued task %s: %s\n", id, args[0])
			return nil
		},
	}
	taskAddCmd.Flags().String("workdir", "", "Working directory for the task")

	taskListCmd := &cobra.Command{
		Use:   "list",
		Short: "List queued tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			queue := process.NewTaskQueue(filepath.Join(defaultConfigDir(), "queue"))
			tasks := queue.List("")
			if len(tasks) == 0 {
				fmt.Println("No tasks.")
				return nil
			}
			for _, t := range tasks {
				elapsed := ""
				if !t.EndedAt.IsZero() {
					elapsed = fmt.Sprintf(" (%s)", t.EndedAt.Sub(t.StartedAt).Truncate(time.Second))
				}
				fmt.Printf("  %s  [%s]  %s%s\n", t.ID, t.Status, truncateStr(t.Prompt, 60), elapsed)
			}
			return nil
		},
	}

	taskLogsCmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "Show task output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			queue := process.NewTaskQueue(filepath.Join(defaultConfigDir(), "queue"))
			logs, err := queue.Logs(args[0])
			if err != nil {
				return err
			}
			fmt.Println(logs)
			return nil
		},
	}

	taskCancelCmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			queue := process.NewTaskQueue(filepath.Join(defaultConfigDir(), "queue"))
			if err := queue.Cancel(args[0]); err != nil {
				return err
			}
			fmt.Printf("Canceled task %s\n", args[0])
			return nil
		},
	}

	taskCmd.AddCommand(taskAddCmd, taskListCmd, taskLogsCmd, taskCancelCmd)

	jobCmd.AddCommand(jobListCmd, jobRunCmd)
	rootCmd.AddCommand(initCmd, runCmd, chatCmd, daemonCmd, jobCmd, heartbeatCmd, taskCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
