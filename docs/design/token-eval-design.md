# token-eval — Design Doc

> Measurement & capture tool for the teeny-claw constellation.
> Status: **Plan** | Author: teenyclaw | Date: 2026-02-16

## Overview

A tiny CLI that **captures the full context of every LLM call** — prompt, context, intent, and output — so you can measure **prompt effectiveness** and build eval datasets over time.

The core question: **does this prompt produce what we intend?** Everything else (cost, model choice) is secondary metadata.

## Goals

- **Capture the prompt-to-outcome chain**: prompt → context → intent → output → result
- Measure prompt effectiveness: which prompts consistently produce good results?
- Build eval datasets: passing records become golden test cases
- Enable prompt improvement: when a prompt fails, the captured data shows what was missing
- Track prompt changes over time: same task, different prompts, which works better?

## Non-Goals (v1)

- No budget tracking or enforcement (capture first, analyze later)
- No model comparison (that's an eval loop concern)
- No proxy/middleware mode
- No built-in tokenizer
- No eval loop runner (that's a future tool — token-eval just captures and retrieves)

---

## Data Model

The core insight: **every LLM call is a potential eval case**. We capture the full picture — what went in, what came out, what was intended, and the token economics.

### Schema

```sql
CREATE TABLE records (
    id              TEXT PRIMARY KEY,  -- ulid
    project         TEXT NOT NULL,     -- e.g. "teeny-claw", "strategy-game"
    task            TEXT,              -- e.g. "implement-put-cmd", "code-review"
    session_id      TEXT,              -- link to agent session (optional)

    -- What went in
    model           TEXT NOT NULL,     -- e.g. "claude-sonnet-4", "gpt-4o"
    provider        TEXT,              -- auto-detected from model if omitted
    prompt          TEXT,              -- the prompt/instruction sent
    context         TEXT,              -- system prompt, injected context, memory
    intent          TEXT,              -- what was this call supposed to accomplish?

    -- What came out
    output          TEXT,              -- the model's response
    result          TEXT,              -- pass | fail | null (did it work?)
    quality         INTEGER,          -- 0-100 (optional granular score)

    -- Token economics
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    cache_creation  INTEGER NOT NULL DEFAULT 0,
    cache_read      INTEGER NOT NULL DEFAULT 0,
    cost_usd        REAL,             -- computed from pricing table
    duration_ms     INTEGER,          -- latency (optional)

    -- Metadata
    created_at      TEXT NOT NULL,    -- RFC3339
    meta            TEXT              -- JSON for extensibility
);

CREATE INDEX idx_records_project ON records(project);
CREATE INDEX idx_records_model ON records(model);
CREATE INDEX idx_records_task ON records(project, task);
CREATE INDEX idx_records_created ON records(created_at DESC);
CREATE INDEX idx_records_result ON records(result);

-- FTS over prompt/context/output for searching records
CREATE VIRTUAL TABLE records_fts USING fts5(
    prompt, context, intent, output,
    content=records,
    content_rowid=rowid
);

-- Pricing table (secondary — cost is derived, not the point)
CREATE TABLE pricing (
    model           TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    input_per_mtok  REAL NOT NULL,    -- $ per million input tokens
    output_per_mtok REAL NOT NULL,    -- $ per million output tokens
    cache_create_multiplier REAL DEFAULT 1.25,
    cache_read_multiplier   REAL DEFAULT 0.1,
    updated_at      TEXT NOT NULL
);
```

### What Gets Captured (per record)

| Field | What | Why |
|-------|------|-----|
| `prompt` | The actual prompt/instruction | Replay: can re-run this against a different model |
| `context` | System prompt, injected memories, tool context | Understand what the model had to work with |
| `intent` | What this call was supposed to accomplish | Eval: does the output match the intent? |
| `output` | The model's full response | Eval: compare against golden output |
| `result` | pass/fail | Binary signal: did it work? |
| `quality` | 0-100 score | Granular signal for scoring |
| `input/output_tokens` | Token counts | Economics + efficiency analysis |
| `cost_usd` | Computed cost | Derived metric |

### The Eval Loop (future, not v1)

```
Prompt A for task X → pass (quality 90)
Prompt B for task X → fail
Prompt C for task X → pass (quality 95)
                         │
                         ▼
              "Prompt C is most effective for task X"
              "Prompt B failed because it lacked schema context"
              "Adding intent field improved pass rate from 70% → 95%"
```

The dataset answers **prompt effectiveness** questions:
- Which prompt patterns work best for which task types?
- What context is necessary vs noise?
- When prompts fail, what's the common thread?
- How do prompt iterations improve results over time?

Model choice and cost are useful metadata but not the primary signal. A well-crafted prompt on a cheap model beats a bad prompt on an expensive one.

We're building the capture layer now. The eval runner comes later.

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

#### `record` — Capture an LLM call

The core command. Called after every LLM call. Captures the full picture.

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
| `--prompt` | | no | The prompt/instruction sent |
| `--context` | | no | System prompt / injected context |
| `--intent` | | no | What this call was supposed to accomplish |
| `--response` | | no | The model's output |
| `--cache-create` | | no | Cache creation tokens |
| `--cache-read` | | no | Cache read tokens |
| `--duration` | | no | Latency in ms |
| `--result` | | no | `pass` or `fail` |
| `--quality` | `-q` | no | Quality score 0-100 |
| `--meta` | | no | JSON metadata |

Text fields (prompt, context, intent, response) can also come via stdin as JSON:
```bash
cat <<EOF | token-eval record -p "teeny-claw" -m claude-sonnet-4
{
  "task": "implement-search",
  "prompt": "Add FTS5 search to the store layer",
  "context": "Project uses SQLite via modernc.org/sqlite...",
  "intent": "Search should match chunks and return full memories",
  "output": "Here's the implementation...",
  "input_tokens": 2500,
  "output_tokens": 800,
  "result": "pass",
  "quality": 90
}
EOF
```

**Output:** JSON of the captured record with computed cost.

#### `query` — Retrieve records

The retrieval side. Find records for analysis, replay, or eval.

```
token-eval query [flags] [search text]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--project` | `-p` | no | Filter by project |
| `--task` | `-t` | no | Filter by task |
| `--model` | `-m` | no | Filter by model |
| `--result` | | no | Filter: `pass`, `fail` |
| `--last` | `-l` | no | Time window: `24h`, `7d`, `30d` |
| `--limit` | | no | Max results (default 20) |
| `--full` | | no | Include prompt/context/output in results (default: summary only) |

Without search text: returns records matching filters. With search text: FTS5 search over prompt/context/intent/output.

```bash
# Get all passing code review records
token-eval query -p "teeny-claw" -t "code-review" --result pass --full

# Search for records about FTS5
token-eval query "FTS5 search"

# Get last 10 records with full content (for building golden dataset)
token-eval query -p "teeny-claw" --last 7d --result pass --full --limit 10
```

**Output (default):**
```json
[
  {
    "id": "01HZ...",
    "project": "teeny-claw",
    "task": "implement-search",
    "model": "claude-sonnet-4",
    "intent": "Search should match chunks and return full memories",
    "result": "pass",
    "quality": 90,
    "input_tokens": 2500,
    "output_tokens": 800,
    "cost_usd": 0.0195,
    "created_at": "2026-02-16T20:00:00Z"
  }
]
```

**Output (--full):** Same but includes `prompt`, `context`, and `output` fields — everything needed to build an eval case.

#### `export` — Export records for eval datasets

```
token-eval export [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--project` | `-p` | no | Filter by project |
| `--task` | `-t` | no | Filter by task |
| `--result` | | no | Filter: `pass` (for golden dataset) |
| `--last` | `-l` | no | Time window |
| `--format` | `-f` | no | `json` (default) or `jsonl` (one record per line) |

```bash
# Export passing records as golden dataset
token-eval export -p "teeny-claw" --result pass -f jsonl > golden.jsonl
```

Each exported record is a complete eval case: prompt + context + intent + expected output.

#### `summary` — Aggregate view (nice-to-have)

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
  "total_cost_usd": 0.87,
  "pass_rate": 0.87,
  "avg_quality": 85,
  "breakdown": [
    {"model": "claude-sonnet-4", "calls": 12, "cost": 0.72, "pass_rate": 0.83},
    {"model": "claude-opus-4", "calls": 3, "cost": 0.15, "pass_rate": 1.00}
  ]
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

Writes a daily summary to agent-memory as an episodic memory so agents can recall spend context.

---

## Project Structure

```
token-eval/
├── cmd/
│   └── token-eval/
│       └── main.go
├── internal/
│   ├── store/
│   │   ├── store.go          # SQLite store + schema
│   │   ├── record.go         # Insert records
│   │   ├── query.go          # Query + FTS5 search
│   │   └── store_test.go
│   ├── pricing/
│   │   ├── pricing.go        # Cost computation
│   │   ├── bundled.go        # Embedded default prices (go:embed)
│   │   └── pricing_test.go
│   └── cli/
│       ├── root.go
│       ├── record.go
│       ├── query.go
│       ├── export.go
│       ├── summary.go
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
# After every LLM call — capture the full picture
cat <<EOF | token-eval record -p "teeny-claw" -m claude-sonnet-4
{
  "task": "implement-search",
  "prompt": "Add FTS5 full-text search to the store layer...",
  "context": "Schema has chunks table, using modernc.org/sqlite...",
  "intent": "Search chunks via FTS5, return full memories ranked by relevance",
  "output": "func (s *SQLiteStore) Search(...) { ... }",
  "input_tokens": 2500,
  "output_tokens": 800,
  "cache_read": 15000,
  "result": "pass",
  "quality": 90
}
EOF

# Query what we've captured
token-eval query -p "teeny-claw" --task "implement-search" --full

# Build golden dataset from passing records
token-eval export -p "teeny-claw" --result pass -f jsonl > golden.jsonl

# Quick summary
token-eval summary -p "teeny-claw" --today
```

### The Eval Loop (future)

```
1. Capture:  agent works → token-eval record (every call)
2. Curate:   token-eval export --result pass → golden.jsonl
3. Analyze:  which prompts work? which fail? what's the pattern?
4. Improve:  refine prompts based on data
5. Validate: re-run improved prompts, measure pass rate change
```

token-eval handles steps 1-2. Steps 3-5 can be done manually at first, automated later.

### How OpenClaw could integrate

OpenClaw already has token counts in `/status`. Future integration:
1. OpenClaw calls `token-eval record` after each agent turn (with prompt + context + output)
2. Agent can query its own prompt history: `token-eval query --task X --result fail` → "why did these fail?"
3. Quality keeper cron records pass/fail for each dev session, building the dataset automatically

---

## Build Plan

### Phase 1 — Capture (v0.1.0)
- [ ] Project scaffold (go mod, cobra, Makefile)
- [ ] SQLite store with schema + FTS5
- [ ] `record` command (flags + stdin JSON, full capture)
- [ ] Cost computation + bundled pricing table (Go embed)
- [ ] Provider auto-detection
- [ ] `query` command with filters + FTS5 search
- [ ] `price list` / `price set`
- [ ] Unit tests
- [ ] Acceptance test suite
- [ ] README

### Phase 2 — Retrieval + Export (v0.2.0)
- [ ] `export` command (JSON/JSONL, filter by result/task/project)
- [ ] `summary` command with grouping (by model, task, day)
- [ ] `sync` command (write to agent-memory)
- [ ] `--format text` for human-readable output
- [ ] JSONL batch recording via stdin

### Phase 3 — Polish (v0.3.0)
- [ ] goreleaser + CI
- [ ] Acceptance test hardening
- [ ] Document eval loop workflow

---

## Open Items

- **Token counting (pre-flight):** Worth adding `token-eval count "text"` using tiktoken-go? Useful but adds ~4MB to binary. Defer.
- **Eval runner:** Separate tool that reads exported golden datasets, runs prompt variations, and scores effectiveness. Not token-eval's job, but token-eval feeds it.
- **Prompt diff tracking:** When the same task gets different prompts over time, could we auto-diff them and correlate with quality changes? e.g. "Adding schema context to code-review prompts improved pass rate 70% → 95%"
- **Auto-capture from OpenClaw:** Could OpenClaw call `token-eval record` after each turn? Full capture without agent cooperation.
- **Prompt size:** Prompts and outputs can be large. Start with no cap, revisit if DB bloats.
