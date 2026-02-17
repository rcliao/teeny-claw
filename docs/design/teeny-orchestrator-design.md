# teeny-orchestrator Design Doc

## Overview

A lightweight autonomous agent runtime in Go. Constructs context, runs tool loops against any LLM provider, and improves over time via an integrated eval feedback loop.

**Not** a framework or SDK. A single binary that uses teeny-claw CLI tools as its capabilities.

## Why

Context construction is the product. The same LLM behaves wildly differently based on what's in its context window. OpenClaw's adoption proves this — customization via workspace files (AGENTS.md, SOUL.md, etc.) directly shapes agent behavior.

teeny-claw already has the tools (agent-memory, token-eval, todo-mgmt). What's missing is the runtime that:
1. Composes them with proper context
2. Runs autonomously in the background
3. Uses eval data to ground and improve its own behavior

## Architecture

```
                    ┌──────────────┐
                    │   Scheduler  │
                    │  cron/heart  │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
     workspace ───→ │   Context    │
     files          │   Builder    │
     tool manifests └──────┬───────┘
     eval data             │
     memory                │
                    ┌──────▼───────┐
                    │   Provider   │
                    │   Adapter    │
                    │  (any LLM)  │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  Tool Loop   │ ←→ agent-memory
                    │  exec tools  │ ←→ token-eval
                    │  feed results│ ←→ todo-mgmt
                    └──────┬───────┘ ←→ exec/read/write
                           │
                    ┌──────▼───────┐
                    │   Session    │
                    │   Manager    │
                    └──────────────┘
```

## Components

### 1. Daemon / Scheduler

Background process with job scheduling.

```json
{
  "jobs": [
    {
      "name": "heartbeat",
      "schedule": "*/30 * * * *",
      "prompt": "Check eval data for recent failures. Review memory for patterns. Take corrective action if needed.",
      "session": "heartbeat"
    },
    {
      "name": "daily-review",
      "schedule": "0 9 * * *",
      "prompt": "Review yesterday's work. Update long-term memory with learnings.",
      "session": "daily-review"
    }
  ]
}
```

Modes:
- `teeny-orchestrator daemon` — run as background service
- `teeny-orchestrator run "message"` — one-shot
- `teeny-orchestrator chat` — interactive REPL
- `teeny-orchestrator job run <name>` — trigger a job manually

### 2. Context Builder

Assembles the LLM context window from multiple sources:

**Static context** (loaded once per run):
- System identity (runtime, workspace path, time)
- Bootstrap files: AGENTS.md, SOUL.md, USER.md, IDENTITY.md, TOOLS.md
- Tool schemas (from tool manifests)
- Skills summary (if any)

**Dynamic context** (per message):
- Session history
- Session summary (from compaction)
- Eval-grounded data (recent token-eval entries)
- Memory context (recent agent-memory entries)

**Budget management**:
- Per-file cap (default 20K chars)
- Total bootstrap cap (default 24K chars)
- History truncation when approaching context window
- Compaction trigger threshold

Priority order when budget is tight:
1. System identity + tools (required)
2. Current message + recent history (required)
3. Bootstrap files (AGENTS.md > SOUL.md > others)
4. Eval/memory context
5. Older history

### 3. Provider Adapter

Interface:

```go
type Provider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Name() string
}

type ChatRequest struct {
    Model    string
    Messages []Message
    Tools    []ToolDef
    MaxTokens int
}

type ChatResponse struct {
    Content   string
    ToolCalls []ToolCall
    Usage     Usage
}
```

Implementations:
- `anthropic` — Claude models via Messages API
- `openai` — GPT/o-series via Chat Completions API  
- `ollama` — local models via OpenAI-compatible API
- `openrouter` — any model via OpenRouter

Tool call format translation is handled internally per provider.

### 4. Tool Loop

The core agent loop:

```
receive message
build context (system + history + user message)
loop:
    call LLM with tools
    if no tool calls: break
    for each tool call:
        resolve tool from registry
        execute (subprocess)
        collect result
    append assistant message + tool results to context
    if iterations >= max: break
save session
return final response
```

Guards:
- Max iterations (default 20)
- Per-tool timeout (default 30s)
- Total run timeout (default 5min)

### 5. Tool Registry

Tools are external binaries discovered via manifests.

**Discovery**: scan `$TEENY_TOOL_PATH` (default: `~/.teeny-claw/tools`) for `tool.json` files.

**Manifest format** (`tool.json`):

```json
{
  "name": "agent-memory",
  "binary": "agent-memory",
  "description": "Store and retrieve versioned memory entries with search",
  "commands": {
    "store": {
      "description": "Store a memory entry",
      "args": "--namespace {namespace} --priority {priority}",
      "stdin": true,
      "parameters": {
        "namespace": { "type": "string", "description": "Memory namespace", "required": true },
        "priority": { "type": "integer", "description": "Priority 0-10", "default": 5 },
        "content": { "type": "string", "description": "Content to store (via stdin)", "required": true }
      }
    },
    "query": {
      "description": "Search memory entries",
      "args": "{query}",
      "parameters": {
        "query": { "type": "string", "description": "Search query", "required": true },
        "limit": { "type": "integer", "description": "Max results", "default": 10 }
      }
    }
  }
}
```

Each command becomes an LLM tool. `agent-memory.store`, `agent-memory.query`, etc.

**Execution**: tool calls are translated to subprocess invocations:
```bash
echo "content" | agent-memory store --namespace decisions --priority 7
```

**Builtins**: `exec`, `read_file`, `write_file`, `list_dir` are built into the orchestrator (not external).

### 6. Session Manager

JSON files in `~/.teeny-claw/sessions/`.

```json
{
  "key": "main",
  "messages": [...],
  "summary": "Previous conversation summary...",
  "created": "2026-02-17T10:00:00Z",
  "updated": "2026-02-17T15:30:00Z"
}
```

Compaction: when message count exceeds threshold (default 50 messages or ~80% context window), summarize older messages via LLM and replace with summary.

Sessions are keyed by name: `main`, `heartbeat`, `daily-review`, custom job names.

### 7. Eval Integration (The Differentiator)

**Auto-capture**: Every LLM call is recorded via `token-eval record`:
```bash
token-eval record \
  --model "claude-sonnet-4-20250514" \
  --provider anthropic \
  --prompt-tokens 1500 \
  --completion-tokens 400 \
  --intent "heartbeat-eval-review" \
  --session "heartbeat" \
  < prompt.json
```

**Eval-grounded context**: Before each run, the context builder can pull:
```bash
# Recent failures or low-quality outputs
token-eval query --intent "%" --limit 5 --full

# Patterns from memory
agent-memory query "lessons learned" --limit 5
```

This data is injected into the context so the LLM is grounded in its own history.

**Self-improvement loop**:
```
heartbeat fires
  → token-eval query: recent runs, costs, patterns
  → agent-memory query: known issues, preferences
  → LLM analyzes: "what should I do differently?"
  → agent-memory store: new learnings
  → optionally: update workspace files (AGENTS.md, etc.)
```

## Config

`~/.teeny-claw/config.json`:

```json
{
  "workspace": "~/.teeny-claw/workspace",
  "provider": {
    "name": "anthropic",
    "model": "claude-sonnet-4-20250514",
    "api_key_env": "ANTHROPIC_API_KEY"
  },
  "tools": {
    "path": ["~/.teeny-claw/tools", "/usr/local/share/teeny-tools"],
    "timeout": 30
  },
  "session": {
    "dir": "~/.teeny-claw/sessions",
    "compaction_threshold": 50,
    "max_history": 100
  },
  "daemon": {
    "heartbeat_interval": "30m",
    "jobs": []
  },
  "context": {
    "bootstrap_max_chars": 20000,
    "bootstrap_total_max_chars": 24000
  },
  "eval": {
    "auto_capture": true,
    "db": "~/.teeny-claw/eval.db"
  }
}
```

## Deps

Same philosophy: minimal, pure Go.
- `cobra` — CLI
- `modernc.org/sqlite` — session/eval storage (optional, can use JSON files)
- One HTTP client for provider APIs (stdlib `net/http`)

No CGo. Single binary. Cross-compile friendly.

## Phases

### Phase 1: Core Loop (this week)
- [ ] Provider adapter (Anthropic first)
- [ ] Tool manifest discovery + registry
- [ ] Tool loop (LLM → exec → results → loop)
- [ ] Context builder (system prompt + bootstrap files)
- [ ] Session manager (JSON files)
- [ ] `teeny-orchestrator run "message"` works end-to-end
- [ ] Auto-capture to token-eval

### Phase 2: Interactive + Daemon
- [ ] `teeny-orchestrator chat` (REPL)
- [ ] `teeny-orchestrator daemon` (background)
- [ ] Cron/heartbeat scheduler
- [ ] Job definitions

### Phase 3: Eval Loop
- [ ] Eval-grounded context injection
- [ ] Heartbeat self-review job
- [ ] Memory-based improvement patterns
- [ ] Prompt/workspace file self-modification

### Phase 4: Multi-Provider
- [ ] OpenAI provider
- [ ] Ollama provider
- [ ] OpenRouter provider
- [ ] Provider fallback chain

### Future
- Compaction (LLM summarization)
- Streaming output
- Channel integration (optional — webhook/stdio)
- MCP tool discovery (alongside manifests)
