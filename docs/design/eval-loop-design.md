# Eval Loop — Design Thinking

> Forward-looking design for how token-eval's captured data feeds into prompt improvement.
> Status: **Thinking** | Date: 2026-02-16

## The Problem

Agents work in multi-step loops, not single calls. A "task" is a **trace** — a sequence of steps where each step has its own prompt, context, and output. The trace as a whole succeeds or fails.

We need eval that works at **two levels**:
1. **Step-level**: Was this individual prompt effective?
2. **Trace-level**: Did the overall loop accomplish the goal?

And it needs to support different loop architectures:

## Two Loop Styles

### ReAct: Thought → Action → Observation

```
Step 1: Thought  → "I need to find the search function"
Step 2: Action   → grep -r "func Search" .
Step 3: Observe  → found in store/search.go
Step 4: Thought  → "I should add FTS5 indexing"
Step 5: Action   → edit store/search.go
Step 6: Observe  → file updated
Step 7: Thought  → "Let me verify with tests"
Step 8: Action   → go test ./...
Step 9: Observe  → PASS
→ Trace result: PASS
```

**Characteristics:**
- Each step is reactive — decide what to do based on last observation
- No upfront planning
- Quality depends on: thought prompts, tool selection, observation parsing
- Failure modes: wrong tool choice, misinterpreting observation, looping

### GEPA: Goal → Explore → Plan → Act → Reflect → Evolve

```
Phase 1: Goal    → "Add FTS5 search to agent-memory"
Phase 2: Explore → read codebase, understand schema, check existing search
Phase 3: Plan    → "1. Add FTS5 table, 2. Add triggers, 3. Update search.go, 4. Test"
Phase 4: Act     → execute plan steps
Phase 5: Reflect → "FTS5 works but key-based search regressed"
Phase 6: Evolve  → "Need to supplement FTS5 with LIKE fallback"
→ Loop back to Act with evolved plan
→ Trace result: PASS
```

**Characteristics:**
- Phases, not steps — each phase can have multiple LLM calls
- Planning happens upfront, then gets refined
- Quality depends on: exploration depth, plan quality, reflection accuracy
- Failure modes: bad plan, shallow exploration, not reflecting on failures

## What Needs Capturing

### Current token-eval record (single call)

```
record:
  prompt, context, intent, output, result, quality, tokens
```

This is fine for individual calls but doesn't capture the **structure** of a trace.

### What we need: Traces + Steps

```
trace:
  id, project, task, goal,
  loop_type (react | gepa | custom),
  steps: [step1, step2, ...],
  result (pass | fail),
  quality (0-100),
  created_at

step:
  id, trace_id, seq,
  phase (thought | action | observe | goal | explore | plan | act | reflect | evolve),
  prompt, context, output,
  input_tokens, output_tokens,
  duration_ms,
  created_at
```

### Schema Addition to token-eval

```sql
-- Traces group steps into a single agent task
CREATE TABLE traces (
    id          TEXT PRIMARY KEY,
    project     TEXT NOT NULL,
    task        TEXT,
    goal        TEXT,              -- what the agent was trying to accomplish
    loop_type   TEXT,              -- react | gepa | custom
    model       TEXT,              -- primary model used
    total_steps INTEGER DEFAULT 0,
    total_input_tokens  INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    total_cost_usd REAL DEFAULT 0,
    result      TEXT,              -- pass | fail
    quality     INTEGER,           -- 0-100
    created_at  TEXT NOT NULL,
    finished_at TEXT,
    meta        TEXT
);

CREATE INDEX idx_traces_project ON traces(project);
CREATE INDEX idx_traces_task ON traces(project, task);
CREATE INDEX idx_traces_result ON traces(result);

-- Steps are individual LLM calls within a trace
CREATE TABLE steps (
    id          TEXT PRIMARY KEY,
    trace_id    TEXT NOT NULL REFERENCES traces(id),
    seq         INTEGER NOT NULL,
    phase       TEXT NOT NULL,     -- free-form; conventions: thought|action|observe (ReAct), goal|explore|plan|act|reflect|evolve (GEPA)
    prompt      TEXT,
    context     TEXT,
    intent      TEXT,
    output      TEXT,
    model       TEXT,
    input_tokens  INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cost_usd    REAL,
    duration_ms INTEGER,
    created_at  TEXT NOT NULL,
    meta        TEXT
);

CREATE INDEX idx_steps_trace ON steps(trace_id, seq);

-- FTS over steps
CREATE VIRTUAL TABLE steps_fts USING fts5(
    prompt, context, intent, output,
    content=steps,
    content_rowid=rowid
);
```

## CLI Flow for Traces

```bash
# Start a trace
TRACE_ID=$(token-eval trace start -p "teeny-claw" -t "add-fts5" \
  --goal "Add FTS5 full-text search" --loop gepa)

# Record steps within the trace
token-eval step -T $TRACE_ID --phase goal \
  --prompt "Add FTS5 search to agent-memory store" \
  --output "Goal understood. Need to: add FTS5 table, triggers, update search." \
  -i 500 -o 200

token-eval step -T $TRACE_ID --phase explore \
  --prompt "Read current search implementation" \
  --context "store/search.go contents..." \
  --output "Current search uses LIKE. No FTS5. Chunks table exists." \
  -i 2000 -o 300

token-eval step -T $TRACE_ID --phase plan \
  --prompt "Create implementation plan" \
  --output "1. Add FTS5 virtual table 2. Add sync triggers 3. Update Search() 4. Test" \
  -i 1500 -o 400

token-eval step -T $TRACE_ID --phase act \
  --prompt "Implement step 1: Add FTS5 table to schema" \
  --output "Added CREATE VIRTUAL TABLE chunks_fts..." \
  -i 2000 -o 800

token-eval step -T $TRACE_ID --phase reflect \
  --prompt "Tests passing? Any regressions?" \
  --output "FTS5 works but key-based search broke. Need LIKE fallback." \
  -i 1500 -o 200

token-eval step -T $TRACE_ID --phase evolve \
  --prompt "Fix: add LIKE supplement after FTS5" \
  --output "Added searchLike fallback. All tests pass." \
  -i 1800 -o 600

# Finish the trace
token-eval trace finish $TRACE_ID --result pass --quality 90
```

Or the whole trace as a single JSON:

```bash
cat trace.json | token-eval trace import
```

## Eval Queries

### Step-level (prompt effectiveness)

```bash
# Which explore prompts produce the best plan quality?
token-eval query --phase explore --result pass --full

# What do failing reflect steps look like?
token-eval query --phase reflect --result fail

# Compare: do prompts with explicit schema context in explore phase
# lead to better act phases?
token-eval query --phase explore --task "add-*" --full
```

### Trace-level (loop effectiveness)

```bash
# How many steps do successful GEPA traces take vs ReAct?
token-eval trace list --loop gepa --result pass
token-eval trace list --loop react --result pass

# Which tasks fail most often?
token-eval trace list --result fail --by task

# Export passing traces as golden datasets
token-eval trace export --result pass --loop gepa -f jsonl > golden-gepa.jsonl
```

### Cross-level (connecting steps to outcomes)

```bash
# Do traces with better explore phases succeed more?
# (This is where the future eval runner adds value)
token-eval trace list --result pass  → check avg explore step quality
token-eval trace list --result fail  → check avg explore step quality
→ "Passing traces have 40% more context in explore phase"
```

## Eval Patterns by Loop Type

### ReAct Eval

**What to measure:**
- **Thought quality**: Does the thought correctly identify the next action?
- **Action selection**: Did it pick the right tool?
- **Observation parsing**: Did it correctly interpret the result?
- **Loop efficiency**: How many steps to complete? (fewer = better prompts)
- **Recovery**: When an action fails, does the next thought recover?

**Golden dataset structure:**
```jsonl
{"goal":"add search","step":"thought","prompt":"...","expected_output":"should identify need to read existing code"}
{"goal":"add search","step":"action","prompt":"...","expected_output":"grep or read the relevant file"}
```

### GEPA Eval

**What to measure:**
- **Exploration depth**: Did it look at enough of the codebase?
- **Plan quality**: Is the plan complete, ordered correctly, feasible?
- **Act precision**: Does each act step correctly implement the plan?
- **Reflection honesty**: Does reflect catch real issues or just say "looks good"?
- **Evolution quality**: Does evolve actually fix what reflect found?
- **Phase transitions**: Does it know when to move from explore → plan → act?

**Golden dataset structure:**
```jsonl
{"goal":"add FTS5","phase":"explore","context":"...","expected":"should find chunks table, no existing FTS"}
{"goal":"add FTS5","phase":"plan","context":"...","expected":"should include FTS table, triggers, search update, tests"}
{"goal":"add FTS5","phase":"reflect","context":"...","expected":"should catch LIKE regression"}
```

## How This Changes token-eval

token-eval's `record` command stays for single-call capture (backward compatible). We add:

- `trace start` / `trace finish` — bracket a multi-step agent run
- `step` — record a step within a trace (replaces `record` for traced calls)
- `trace list` / `trace export` — query and export at trace level
- Existing `query` gains `--phase` and `--trace` filters

The key insight: **records are for single calls, traces are for agent loops.** Both feed the same eval pipeline.

## Build Sequence

This doesn't all need to land in token-eval v0.1. Proposed phasing:

**token-eval v0.1** — Single-call `record` + `query` (what we designed today)
**token-eval v0.2** — Add `trace` + `step` commands, trace-level queries
**token-eval v0.3** — `trace export` for golden datasets, phase-based analysis

**eval-runner (future separate tool)** — Reads golden datasets, replays, scores

---

## Open Questions

1. **Who creates traces?** The agent harness (OpenClaw) or the agent itself? If the harness, it needs to know about loop structure. If the agent, it needs to call `trace start/step/finish`.

2. **Phase naming**: Free-form strings with documented conventions. ReAct: `thought|action|observe`. GEPA: `goal|explore|plan|act|reflect|evolve`. Agents can use any string. ✅ Decided.

3. **Nested traces**: Can a trace contain sub-traces? e.g., a GEPA act phase that internally uses ReAct. Start simple (flat), add nesting later if needed.

4. **Automatic step detection**: Could the system infer phase from prompt patterns? e.g., "Let me think about..." → thought. Nice but fragile. Defer.
