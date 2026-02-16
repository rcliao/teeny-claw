# token-eval — Research Notes

> Research phase for `token-eval`, the measurement tool in the teeny-claw constellation.
> Date: 2026-02-16

## What We're Building

A tiny CLI that wraps agent runs and records token usage, cost, and quality metrics. Lets agents (and humans) understand what they're spending and whether the spend is worth it.

## The Problem

Right now when an agent does work, you get a vague sense of "that was expensive" or "that was cheap." There's no structured way to:
- Track token usage per task/session/project over time
- Compare cost between different approaches to the same task
- Know your actual spend across models and providers
- Evaluate whether a cheaper model would've been good enough
- Set budgets and get alerts

## Landscape Survey

### Existing Tools

| Tool | Approach | Notes |
|------|----------|-------|
| **Langfuse** | Full observability platform. Tracing, token tracking, cost, evals. | Heavy — server, DB, UI. Way more than we need. |
| **LiteLLM** | Proxy that tracks spend across providers. | Proxy approach — requires routing all traffic through it. |
| **Traceloop/OpenLLMetry** | OpenTelemetry-based LLM tracing. | SDK-first, Python-heavy. |
| **OpenClaw `/status`** | Shows tokens in/out, context usage per session. | Already useful! But ephemeral — resets per session, no history. |
| **Claude Code** | Shows session cost in status bar. | Per-session only, no export, no history. |

**Gap:** No simple CLI tool that records and queries token usage history across sessions, projects, and models. Everything is either a full platform (Langfuse) or ephemeral (OpenClaw status).

### Token Types to Track

From the Anthropic API (and similar across providers):

| Token Type | What | Cost Factor |
|------------|------|-------------|
| `input_tokens` | Prompt tokens (non-cached) | Full input price |
| `output_tokens` | Completion tokens | Full output price (usually higher) |
| `cache_creation_input_tokens` | First-time cached prompt tokens | 1.25× input price |
| `cache_read_input_tokens` | Cached prompt tokens on subsequent calls | 0.1× input price |

### Where Token Data Comes From

Two main approaches:

**A) Parse from API responses** — Agent makes LLM call, response includes `usage` object. Agent pipes it to `token-eval record`.

**B) Wrap the agent** — `token-eval wrap -- claude-code "do something"` captures stdout/stderr, extracts token info.

**C) Accept structured input** — Agent or harness posts a JSON event: `{"model": "claude-sonnet-4", "input_tokens": 1500, "output_tokens": 300, ...}`

Option C is cleanest — the harness (OpenClaw, Claude Code, custom script) already knows the token counts. Just pass them to us.

### Pricing Data

Model pricing changes frequently. Options:
1. **Bundled pricing table** — ship a JSON file with known model prices, update with releases
2. **Fetch from API** — LiteLLM maintains a live pricing list
3. **User-provided** — `token-eval config set-price claude-sonnet-4 --input 3.0 --output 15.0`
4. **Hybrid** — bundle defaults, let user override, optionally fetch updates

## Design Thinking

### What makes this useful for agents?

An agent should be able to:

```bash
# Before starting work — check budget
token-eval budget --project "teeny-claw" --remaining
# → "Budget: $5.00/day, spent: $1.23, remaining: $3.77"

# After an LLM call — record it
token-eval record --project "teeny-claw" --task "implement-put-cmd" \
  --model "claude-sonnet-4" \
  --input 1500 --output 300 --cache-read 8000

# After a session — see summary
token-eval summary --project "teeny-claw" --today
# → Today: 45k input, 12k output, $0.87 across 15 calls

# Compare approaches
token-eval compare --task "code-review" --last 10
# → Sonnet avg $0.12/review, Opus avg $0.45/review

# Trend over time
token-eval trend --project "teeny-claw" --last 7d
# → Daily spend: $0.80, $1.20, $0.65, ...
```

### Core Data Model

```
record (one per LLM call):
  id, project, task, session_id,
  model, provider,
  input_tokens, output_tokens,
  cache_creation_tokens, cache_read_tokens,
  cost_usd (computed),
  duration_ms (optional — latency),
  quality_signal (optional — did the output satisfy the task?),
  created_at,
  meta (JSON)
```

### What's a "quality signal"?

This is the hard/interesting part. Token counting is easy. Knowing whether the tokens were *well spent* is hard. Options:

1. **Binary pass/fail** — agent reports whether the task succeeded: `token-eval record ... --result pass`
2. **Score 0-100** — agent self-evaluates: `--quality 85`
3. **Downstream signal** — tests passed? Build succeeded? PR merged? Link to an outcome.
4. **None for v1** — just track tokens and cost, add quality later

I'd say: **v1 ships with optional `--result pass|fail` and `--quality 0-100`**. Simple, doesn't require infra, agent can self-report.

### Relationship to agent-memory

Token-eval stores its data in **agent-memory**. This is why memory comes first in the build order:

```bash
# Under the hood, token-eval does:
agent-memory put --ns "token-eval:teeny-claw" --key "call-01ABC" --kind episodic \
  '{"model":"claude-sonnet-4","input":1500,"output":300,"cost":0.0195,...}'
```

Or does it? This raises a question: **should token-eval use agent-memory, or its own SQLite DB?**

Arguments for agent-memory:
- Dogfooding our own tool
- Cross-tool queryability (search memories and token data together)
- One less DB to manage

Arguments for own DB:
- Token data is high-volume, structured, and numeric — better suited for SQL aggregations than text search
- Don't want token logs cluttering memory search results
- Different query patterns (SUM, AVG, GROUP BY vs text search)

**Recommendation:** Own SQLite DB for operational data, but write daily/weekly summaries to agent-memory as episodic memories. Best of both worlds.

## Decisions

### Settled
1. **CLI tool** — `token-eval record`, `summary`, `trend`, `budget`, `compare`
2. **Structured JSON input** — harness passes token counts, we record and compute cost
3. **Own SQLite DB** — optimized for numeric aggregations
4. **Bundled pricing + user overrides** — ship known model prices, let users add/update
5. **Optional quality signals** — `--result pass|fail`, `--quality 0-100` from v1
6. **Daily summaries to agent-memory** — bridge between the two tools

### Open Questions
1. **Budget enforcement** — should `token-eval budget` just report, or actually block calls?
2. **Stdin streaming** — should we support piping a stream of JSON events? (batch recording)
3. **Token counting** — should we include a `token-eval count "some text"` command for pre-flight estimation? (using tiktoken-go)
4. **Multi-provider pricing** — how to handle Anthropic vs OpenAI vs Google vs local models?

## Next Steps

1. ✅ Research (this doc)
2. 📋 **Plan** — Design doc with schema, CLI spec, pricing table format
3. 🎯 **Steer** — Review with EV
4. 🔨 **Build** — after agent-memory v0.1 ships
