# token-eval — Design Doc

> Measurement tool for the teeny-claw constellation.
> Status: **Plan** | Author: teenyclaw | Date: 2026-02-16

## Overview

A tiny CLI that records LLM token usage, computes cost, and lets you query spend over time. Agents and harnesses pipe structured data in; token-eval tracks it, prices it, and reports on it.

## Goals

- Record token usage per call with project/task/model context
- Compute cost automatically from a pricing table
- Query: summary, trends, comparisons, budgets
- Optional quality signals so you can answer "was that spend worth it?"
- Dogfood agent-memory for daily summaries

## Non-Goals (v1)

- No proxy/middleware mode (not intercepting API calls)
- No real-time streaming dashboard
- No budget enforcement (reporting only)
- No built-in tokenizer (pre-flight counting is a maybe)

---

## Data Model

### Schema

```sql
CREATE TABLE records (
    id              TEXT PRIMARY KEY,  -- ulid
    project         TEXT NOT NULL,     -- e.g. "teeny-claw", "strategy-game"
    task            TEXT,              -- e.g. "implement-put-cmd", "code-review"
    session_id      TEXT,              -- link to agent session (optional)
    model           TEXT NOT NULL,     -- e.g. "claude-sonnet-4", "gpt-4o"
    provider        TEXT,              -- e.g. "anthropic", "openai", "google" (auto-detected from model if omitted)
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    cache_creation  INTEGER NOT NULL DEFAULT 0,
    cache_read      INTEGER NOT NULL DEFAULT 0,
    cost_usd        REAL,             -- computed from pricing table
    duration_ms     INTEGER,          -- latency (optional)
    result          TEXT,             -- pass | fail | null
    quality         INTEGER,          -- 0-100 (optional)
    created_at      TEXT NOT NULL,    -- RFC3339
    meta            TEXT              -- JSON for extensibility
);

CREATE INDEX idx_records_project ON records(project);
CREATE INDEX idx_records_model ON records(model);
CREATE INDEX idx_records_task ON records(project, task);
CREATE INDEX idx_records_created ON records(created_at DESC);

-- Pricing table
CREATE TABLE pricing (
    model           TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    input_per_mtok  REAL NOT NULL,    -- $ per million input tokens
    output_per_mtok REAL NOT NULL,    -- $ per million output tokens
    cache_create_multiplier REAL DEFAULT 1.25,  -- multiplier on input price
    cache_read_multiplier   REAL DEFAULT 0.1,   -- multiplier on input price
    updated_at      TEXT NOT NULL
);

-- Budget table
CREATE TABLE budgets (
    project         TEXT NOT NULL,
    period          TEXT NOT NULL,    -- daily | weekly | monthly
    amount_usd      REAL NOT NULL,
    PRIMARY KEY (project, period)
);
```

### Cost Computation

```
cost = (input_tokens * input_per_mtok / 1_000_000)
     + (output_tokens * output_per_mtok / 1_000_000)
     + (cache_creation * input_per_mtok * cache_create_multiplier / 1_000_000)
     + (cache_read * input_per_mtok * cache_read_multiplier / 1_000_000)
```

### Bundled Pricing (ships with binary)

```json
[
  {"model": "claude-opus-4", "provider": "anthropic", "input": 15.0, "output": 75.0},
  {"model": "claude-sonnet-4", "provider": "anthropic", "input": 3.0, "output": 15.0},
  {"model": "claude-haiku-3.5", "provider": "anthropic", "input": 0.80, "output": 4.0},
  {"model": "gpt-4o", "provider": "openai", "input": 5.0, "output": 15.0},
  {"model": "gpt-4o-mini", "provider": "openai", "input": 0.15, "output": 0.60},
  {"model": "gemini-2.5-pro", "provider": "google", "input": 1.25, "output": 10.0},
  {"model": "deepseek-r1", "provider": "deepseek", "input": 0.55, "output": 2.19}
]
```

Bundled as a Go embed. User overrides via `token-eval price set`.

### Provider Auto-Detection

If `--provider` is omitted, detect from model name:
- `claude-*` → anthropic
- `gpt-*` → openai
- `gemini-*` → google
- `deepseek-*` → deepseek
- else → "unknown"

---

## CLI Specification

### Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--db` | `-d` | `$TOKEN_EVAL_DB` or `~/.token-eval/eval.db` | Database path |
| `--format` | `-f` | `json` | Output format: `json` or `text` |

### Commands

#### `record` — Record a usage event

The core command. Called after every LLM call.

```
token-eval record [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--project` | `-p` | yes | Project name |
| `--model` | `-m` | yes | Model name |
| `--input` | `-i` | yes | Input token count |
| `--output` | `-o` | yes | Output token count |
| `--task` | `-t` | no | Task name |
| `--session` | | no | Session ID |
| `--provider` | | no | Provider (auto-detected if omitted) |
| `--cache-create` | | no | Cache creation tokens |
| `--cache-read` | | no | Cache read tokens |
| `--duration` | | no | Latency in ms |
| `--result` | | no | `pass` or `fail` |
| `--quality` | `-q` | no | Quality score 0-100 |
| `--meta` | | no | JSON metadata |

Also accepts JSON on stdin for batch recording:
```bash
echo '{"project":"tc","model":"claude-sonnet-4","input":1500,"output":300}' | token-eval record
```

**Output:** JSON of the recorded event with computed cost.

#### `summary` — Usage summary

```
token-eval summary [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--project` | `-p` | no | Filter by project |
| `--model` | `-m` | no | Filter by model |
| `--today` | | no | Today only |
| `--last` | `-l` | no | Time window: `24h`, `7d`, `30d` |
| `--by` | | no | Group by: `model`, `task`, `project`, `day` |

**Output:**
```json
{
  "period": "2026-02-16",
  "total_calls": 15,
  "input_tokens": 45000,
  "output_tokens": 12000,
  "cache_read_tokens": 80000,
  "total_cost_usd": 0.87,
  "avg_cost_per_call": 0.058,
  "breakdown": [
    {"model": "claude-sonnet-4", "calls": 12, "cost": 0.72},
    {"model": "claude-opus-4", "calls": 3, "cost": 0.15}
  ]
}
```

#### `trend` — Spend over time

```
token-eval trend --project "teeny-claw" --last 7d
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--project` | `-p` | no | Filter by project |
| `--last` | `-l` | no | Time window (default `7d`) |
| `--by` | | no | Group by: `day` (default), `hour`, `week` |

**Output:** JSON array of daily totals.

#### `compare` — Compare models or tasks

```
token-eval compare --task "code-review" --last 30d
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--task` | `-t` | no | Compare models for this task |
| `--model` | `-m` | no | Compare tasks for this model |
| `--project` | `-p` | no | Filter by project |
| `--last` | `-l` | no | Time window |

**Output:** Side-by-side comparison with avg cost, avg quality, pass rate.

```json
{
  "task": "code-review",
  "models": [
    {"model": "claude-sonnet-4", "calls": 20, "avg_cost": 0.12, "avg_quality": 82, "pass_rate": 0.90},
    {"model": "claude-opus-4", "calls": 5, "avg_cost": 0.45, "avg_quality": 91, "pass_rate": 1.00}
  ]
}
```

This is where quality signals pay off — you can see that Opus costs 4× more but has higher quality and 100% pass rate.

#### `budget` — Budget tracking

```
token-eval budget [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--project` | `-p` | yes | Project |
| `--set` | | no | Set budget: `--set 5.00 --period daily` |
| `--period` | | no | `daily`, `weekly`, `monthly` |

Without `--set`: reports current spend vs budget.

```json
{
  "project": "teeny-claw",
  "period": "daily",
  "budget_usd": 5.00,
  "spent_usd": 1.23,
  "remaining_usd": 3.77,
  "pct_used": 24.6
}
```

#### `price` — Manage pricing table

```
token-eval price list                                           # show all known prices
token-eval price set claude-opus-4.6 --input 15 --output 75    # add/update a model
token-eval price rm old-model                                   # remove
```

#### `sync` — Write summary to agent-memory

```
token-eval sync --project "teeny-claw" --last 24h
```

Computes a summary and writes it to agent-memory as an episodic memory:
```bash
agent-memory put --ns "token-eval:teeny-claw" --key "daily-2026-02-16" --kind episodic \
  "Spent $0.87 across 15 calls. Sonnet: $0.72 (12 calls), Opus: $0.15 (3 calls). Avg quality: 85."
```

This bridges token-eval → agent-memory so agents can search their own spend history in context.

---

## Project Structure

```
token-eval/
├── cmd/
│   └── token-eval/
│       └── main.go
├── internal/
│   ├── store/
│   │   ├── store.go          # Store interface + SQLite
│   │   ├── record.go         # Insert/query records
│   │   └── record_test.go
│   ├── pricing/
│   │   ├── pricing.go        # Pricing table + cost computation
│   │   ├── bundled.go        # Embedded default prices
│   │   └── pricing_test.go
│   ├── report/
│   │   ├── summary.go        # Summary aggregations
│   │   ├── trend.go          # Trend over time
│   │   ├── compare.go        # Model/task comparison
│   │   └── budget.go         # Budget tracking
│   └── cli/
│       ├── root.go
│       ├── record.go
│       ├── summary.go
│       ├── trend.go
│       ├── compare.go
│       ├── budget.go
│       ├── price.go
│       └── sync.go
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── test/
    └── acceptance.sh
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `modernc.org/sqlite` | Pure Go SQLite |
| `github.com/oklog/ulid/v2` | ULID generation |

Same three deps as agent-memory. The `sync` command shells out to `agent-memory` binary (no library dependency).

---

## Integration Points

### How agents use it

```bash
# After every LLM call in a dev session:
token-eval record -p "teeny-claw" -t "implement-search" \
  -m claude-sonnet-4 -i 2500 -o 800 --cache-read 15000 \
  --result pass --quality 90

# At end of session:
token-eval summary -p "teeny-claw" --today

# Daily cron job:
token-eval sync -p "teeny-claw" --last 24h
```

### How OpenClaw could integrate

OpenClaw already has token counts in `/status`. A future integration:
1. OpenClaw calls `token-eval record` after each agent turn
2. Agent can `token-eval summary --today` to see its own spend
3. `token-eval budget --project X` could gate expensive operations

### How quality signals flow

```
Agent does task → records result (pass/fail + quality 0-100)
                         │
                         ▼
              token-eval compare --task X
                         │
                         ▼
         "Sonnet is 90% as good as Opus for code reviews
          but costs 4× less. Switch to Sonnet."
```

This is the long game: data-driven model selection per task type.

---

## Build Plan

### Phase 1 — Core (v0.1.0)
- [ ] Project scaffold (go mod, cobra, Makefile)
- [ ] SQLite store with schema migration
- [ ] Bundled pricing table (Go embed)
- [ ] `record` command (flags + stdin JSON)
- [ ] Cost computation
- [ ] Provider auto-detection
- [ ] `price list` / `price set` / `price rm`
- [ ] Unit tests
- [ ] Acceptance test suite
- [ ] README

### Phase 2 — Reporting (v0.2.0)
- [ ] `summary` command with grouping
- [ ] `trend` command with time windows
- [ ] `compare` command with quality analysis
- [ ] `budget` set/check

### Phase 3 — Integration (v0.3.0)
- [ ] `sync` command (write to agent-memory)
- [ ] `--format text` for human-readable output
- [ ] Stdin batch recording (JSONL)
- [ ] goreleaser + CI

---

## Open Items

- **Token counting (pre-flight):** Worth adding `token-eval count "text"` using tiktoken-go? Useful but adds ~4MB to binary size from vocab embeddings. Defer to v1.0?
- **Budget enforcement mode:** Future flag `--enforce` that returns exit code 1 when over budget. Agents check exit code before making calls.
- **Real-time tracking:** Could token-eval accept a streaming pipe from agent stdout? Nice but complex. Defer.
