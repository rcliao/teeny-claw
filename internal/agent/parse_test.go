package agent

import "testing"

func TestParseToolCall(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantTool string
		wantIn   string
		wantDone bool
	}{
		{"done", "DONE", "", "", true},
		{"done with text", "DONE - all complete", "", "", true},
		{"no tool marker", "just some text", "", "", true},
		{"basic", "TOOL:shell INPUT:echo hi", "shell", "echo hi", false},
		{"with whitespace", "  TOOL:shell  INPUT:  ls -la  ", "shell", "ls -la", false},
		{"no input", "TOOL:shell", "shell", "", false},
		{"prefixed text", "I think we should run TOOL:shell INPUT:pwd", "shell", "pwd", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, in, done := parseToolCall(tt.input)
			if tool != tt.wantTool {
				t.Errorf("tool = %q, want %q", tool, tt.wantTool)
			}
			if in != tt.wantIn {
				t.Errorf("input = %q, want %q", in, tt.wantIn)
			}
			if done != tt.wantDone {
				t.Errorf("done = %v, want %v", done, tt.wantDone)
			}
		})
	}
}
