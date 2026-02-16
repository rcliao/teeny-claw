# CLAUDE.md

## Project Overview

**teeny-claw** — A minimal, self-improving autonomous agent framework in Go.

### Prior Art & Inspiration
- **OpenClaw** (TypeScript, 430K+ lines) — full-featured but massive. We want the core ideas, not the complexity.
- **NanoBot** (Python, ~4K lines, github.com/HKUDS/nanobot) — proved you can do core OpenClaw in 4K lines. Good memory system reference.
- **PicoClaw** (Go, <10MB RAM, github.com/sipeed/picoclaw) — Go port of NanoBot, runs on $10 hardware. Good Go architecture reference.
- **NanoClaw** (Python, github.com/gavrielc/nanoclaw) — wraps Claude Code as the LLM harness, container isolation, "skills over features" philosophy.

### What Makes teeny-claw Different
- **Self-improving memory** — the agent reflects on outcomes and distills lessons into long-term memory
- **Self-correction loop** — detects failures, retries with context, learns patterns
- **Claude Code as backend** — wraps `claude` CLI for LLM, not raw API calls
- **Go for performance** — single binary, fast startup, easy process management
- **Autonomous-first** — designed for background operation, not just chat

Wraps Claude Code (or other coding CLIs) as the LLM backend and adds:
- **Process management** — spawn, monitor, and orchestrate coding agent sessions
- **Tool execution** — shell commands, file I/O, structured tool calls
- **Memory system** — persistent context, self-improving knowledge base, semantic recall
- **Cron/heartbeat** — scheduled autonomous work sessions
- **Self-correction loop** — detect failures, retry with context, learn from mistakes

### Core Philosophy
- **Small and sharp** — do fewer things well, not everything poorly
- **Go for performance** — fast startup, low memory, easy to deploy as a single binary
- **Memory-first** — great memory is the foundation of great autonomy
- **Self-improving** — the agent should get better at its job over time by learning from successes and failures

## Architecture

```
teeny-claw/
├── cmd/
│   └── teeny/          # CLI entrypoint
│       └── main.go
├── internal/
│   ├── agent/          # Core agent loop (observe → think → act → reflect)
│   ├── memory/         # Memory system (short-term, long-term, semantic search)
│   ├── tools/          # Tool registry and execution (shell, file, etc.)
│   ├── process/        # Process manager (spawn/monitor Claude Code sessions)
│   ├── scheduler/      # Cron + heartbeat scheduler
│   ├── context/        # Context window management and pruning
│   └── config/         # Configuration loading
├── pkg/
│   └── llm/            # LLM client abstraction (Claude Code CLI, API, etc.)
├── memory/             # Runtime memory files (gitignored)
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Commands

```bash
# Build
go build -o bin/teeny ./cmd/teeny

# Run
./bin/teeny run                    # Start agent loop
./bin/teeny run --once             # Single execution cycle
./bin/teeny cron                   # Start with scheduler
./bin/teeny memory search "query"  # Search memory

# Test
go test ./...
go test -v ./internal/memory/...   # Test specific package
go test -race ./...                # Race detection

# Lint
golangci-lint run
```

## Key Design Decisions

### Agent Loop (GEPA-style, NOT ReAct)
The core loop follows: **Goal → Explore → Plan → Act → Reflect → Evolve**

Unlike ReAct (think→act→observe), GEPA-style enables genuine self-improvement:

1. **Goal**: Define what we're trying to achieve (from task queue, cron, or human input)
2. **Explore**: Gather context — search memory for related past attempts, analyze failure patterns, review evolved strategies
3. **Plan**: Generate an improved approach informed by past reflections (not just raw context)
4. **Act**: Execute the plan using tools (shell, file ops, Claude Code sessions)
5. **Reflect**: Analyze outcome — what worked, what failed, and critically, WHY. Use a separate reflection pass (can be a lighter/cheaper LLM call) to generate textual insights
6. **Evolve**: Update semantic memory with distilled lessons. Evolve strategies and prompts based on reflection. Build a knowledge graph of improvement patterns.

Key insight from GEPA research (arxiv.org/abs/2507.19457): reflective prompt evolution outperforms reinforcement learning. The agent improves by analyzing its own mistakes and generating better instructions, not by trial-and-error reward signals.

**What gets evolved:**
- Task-specific strategies (how to approach certain types of problems)
- Tool usage patterns (which tools work best for what)
- Error recovery playbooks (known failure modes → fixes)
- Self-generated prompt improvements

### Memory System
Three layers:
- **Working memory**: Current session context (in-memory, pruned by token budget)
- **Episodic memory**: Session logs with outcomes (append-only files)
- **Semantic memory**: Indexed knowledge base with vector search (self-maintained)

The agent periodically reflects on episodic memory to distill lessons into semantic memory (self-improvement).

### Self-Correction
- Track tool execution outcomes (success/failure/partial)
- On failure: add error context and retry with adjusted approach
- After N failures: escalate or pause and log for human review
- Successful corrections get recorded as patterns in semantic memory

### Process Manager
- Wraps Claude Code CLI (`claude` command) as primary LLM backend
- Manages multiple concurrent sessions
- Captures stdout/stderr, detects hangs, enforces timeouts
- Can also wrap other CLI agents (codex, gemini, etc.)

## Code Patterns

- Standard Go project layout (`cmd/`, `internal/`, `pkg/`)
- Interfaces for all major components (testable, swappable)
- Context propagation via `context.Context`
- Structured logging via `slog`
- Errors wrapped with `fmt.Errorf("...: %w", err)`
- Table-driven tests
- No global state

## Dependencies (keep minimal)

- Standard library as much as possible
- `sqlite3` for memory persistence (CGo or modernc.org/sqlite for pure Go)
- Consider `chromem-go` or similar for in-process vector search
- `cobra` or just `flag` for CLI
- `cron` library for scheduling
