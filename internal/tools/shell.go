package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Shell executes shell commands.
type Shell struct {
	// WorkDir is the working directory for commands.
	WorkDir string
}

func (s *Shell) Name() string        { return "shell" }
func (s *Shell) Description() string { return "Execute a shell command and return its output" }

func (s *Shell) Execute(ctx context.Context, input string) Result {
	cmd := exec.CommandContext(ctx, "sh", "-c", input)
	if s.WorkDir != "" {
		cmd.Dir = s.WorkDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	if err != nil {
		return Result{Output: output, Error: fmt.Errorf("shell: %w", err)}
	}
	return Result{Output: output}
}
