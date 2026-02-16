package llm

import "context"

// Mock is a test double for Client.
type Mock struct {
	// Response is returned by Generate calls.
	Response string
	// Err is returned if non-nil.
	Err error
	// Calls records all prompts received.
	Calls []string
}

func (m *Mock) Generate(_ context.Context, prompt string) (string, error) {
	m.Calls = append(m.Calls, prompt)
	return m.Response, m.Err
}

func (m *Mock) GenerateWithSystem(_ context.Context, system, prompt string) (string, error) {
	m.Calls = append(m.Calls, system+"\n"+prompt)
	return m.Response, m.Err
}
