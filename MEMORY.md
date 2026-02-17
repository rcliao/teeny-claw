# MEMORY.md — teeny-claw Long-Term Memory

## What Is This Project
A constellation of tiny, agent-specific CLI tools in Go. Unix philosophy: small, composable, text in/text out. Each tool is its own GitHub repo under `github.com/rcliao/`.

## Human
- Name: Eric (rcliao on GitHub)
- Cares about: clean design, bottom-up building, eval loops, prompt effectiveness
- Style: thinks before building, research → plan → steer → build → evaluate cycle
- Prefers: Go, pure Go deps (no CGo), standalone binaries

## Key Decisions
- **SQLite via modernc.org/sqlite** — pure Go, no CGo, easy cross-compile
- **Three deps per tool**: cobra, modernc.org/sqlite, oklog/ulid
- **Immutable versioning** in agent-memory — updates create new versions
- **token-eval is a capture tool, not a cost tracker** — captures prompt/context/intent/output for eval datasets
- **Eval loop supports ReAct and GEPA** — free-form phase naming with conventions
- **Prompt effectiveness > model selection** — the core question is "does this prompt produce what we intend?"
- **No budget/compare in token-eval v1** — capture first, analyze later

## Tool Status

### agent-memory (v0.4.0) — DONE through Phase 4
- Repo: github.com/rcliao/agent-memory
- CRUD, versioning, markdown chunking, priority, access tracking
- FTS5 full-text search, context assembly with composite scoring
- link command, export/import
- Pluggable embeddings (Ollama/OpenAI), hybrid FTS5+LIKE+vector search
- 39/39 acceptance tests passing

### token-eval (v0.1.0) — Phase 1 DONE
- Repo: github.com/rcliao/token-eval
- record command (flags + stdin JSON) — captures prompt, context, intent, output
- query command with filters + FTS5 search + --full flag
- price list/set/rm with bundled pricing (7 models)
- Provider auto-detection, cost computation
- 16/16 acceptance tests passing
- **Next**: Phase 2 (export, summary, sync) OR move to next tool

### agent-artifacts — DROPPED
- Filesystem + git + agent-memory covers it

### todo-mgmt (v0.1.0) — DONE
- Repo: github.com/rcliao/todo-mgmt
- Session-scoped focus tool — keeps agents on track, not a persistent task DB
- Commands: add, list, next, done, skip, note, clear
- Flat ordered list, ULID prefix matching, JSON output
- 15/15 acceptance tests passing
- Key insight: persistence confuses agents; this is a guardrail, not a database

### research-helper — DROPPED
- Without LLM: thin wrapper around web_search + web_fetch (agent already does this)
- With LLM: duplicates the agent itself
- The agent *is* the research helper

### plan-doc — NOT STARTED
- Human-agent collaborative planning via TUI-friendly comment docs
- Like Claude Code plan mode but persistent and commentable

## Design Docs
- `docs/design/agent-memory-design.md` — full schema and CLI spec
- `docs/design/token-eval-design.md` — capture-first design, prompt effectiveness focus
- `docs/design/eval-loop-design.md` — trace/step model for ReAct + GEPA eval loops
- `docs/research/agent-memory-research.md` — landscape survey
- `docs/research/token-eval-research.md` — landscape survey

## Cron Setup
- Dev session: 10am, 2pm, 6pm PT — reads roadmap, picks up where we left off
- Quality check: 10:30am, 2:30pm, 6:30pm PT — tests all tools, fixes trivial issues, writes reports

## Lessons Learned
- Dev session cron works best when it figures out state itself (not a fixed task list)
- Quality keeper should be separate from dev — different concerns
- Acceptance test scripts need `$((PASS + 1))` not `((PASS++))` with `set -e`
- **Don't build tools for what markdown + grep + git already handles.** agent-artifacts was dropped because files are files. Only build a tool when the problem actually needs structure beyond what the filesystem provides.
