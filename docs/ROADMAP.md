# Teeny-Claw Roadmap 🔧⚡

## Vision

A collection of tiny, agent-specific CLI tools — each its own GitHub project. Agents build and use their own tools. Each tool goes through the **Research → Plan → Steer → Build → Evaluate** cycle before building.

## Tool Constellation & Build Order

Build order follows the dependency graph — bottom-up from foundational to capstone.

```
                    ┌─────────────┐
                    │  plan-doc   │  ← 5. Capstone
                    │ (steering)  │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  todo-mgmt  │  ← 4. Management
                    │   (tasks)   │
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
  ┌──────▼──────┐  ┌───────▼──────┐         │
  │ token-eval  │  │   research   │         │  ← 2-3. Middle layer
  │  (capture)  │  │   helper     │         │
  └──────┬──────┘  └───────┬──────┘         │
         │                 │                 │
         └─────────────────┼─────────────────┘
                           │
                    ┌──────▼──────┐
                    │agent-memory │  ← 1. Foundation
                    │ (storage)   │
                    └─────────────┘
```

## Build Order (Bottom-Up)

### 1. `agent-memory` 🧱 — Foundation ✅
**What:** Persistent structured storage for agent data. Simple read/write/query CLI.
**Why first:** Everything else needs somewhere to persist state.
**Status:** Done — CRUD, FTS5 search, context assembly, links, vector embeddings. 39/39 tests.

### 2. `token-eval` 📊 — Capture ✅ (Phase 1)
**What:** Captures the full context of every LLM call — prompt, context, intent, output — for measuring prompt effectiveness and building eval datasets.
**Why second:** Foundation for eval loops. Every subsequent tool we build gets captured for evaluation.
**Depends on:** agent-memory (for syncing summaries)
**Status:** Phase 1 done — record, query, price. 16/16 tests. Phase 2 (export, summary, sync) deferred.

### 3. `todo-mgmt` ✅ — Focus Tool
**What:** Session-scoped ordered task list that keeps agents on track. Agent decomposes a goal → creates todos → works through them in order. The guardrail against drift.
**Why:** Agents wander without structure. A simple ordered list keeps execution focused.
**Key insight:** Not a persistent task database — it's a focus mechanism. Session-scoped, caller manages lifecycle.
**Depends on:** Nothing (standalone)
**Status:** Building now

### ~~4. `research-helper`~~ — DROPPED
Without an LLM, it's just a thin wrapper around web_search + web_fetch (which agents already use). With an LLM, it duplicates the agent itself. The agent *is* the research helper.

### 4. `plan-doc` 📋 — Human-Agent Collaboration
**What:** Collaborative planning between human and agent via a structured doc with inline comments. Like Claude Code's plan mode but TUI-friendly — agent proposes, human comments/steers, agent revises.
**Why last:** Uses everything — research feeds plans, todos track execution.
**Depends on:** All of the above.
**Key problem:** Current planning is chat-based — linear, hard to reference, easy to lose context. A doc with comments gives both sides a shared artifact to iterate on.
**Research topics:** Human-in-the-loop planning, comment-based steering, structured plan formats, TUI design patterns.

## ~~agent-artifacts~~ — DROPPED
Filesystem + git + agent-memory covers it. Didn't pass the tool filter:
- **Scale:** Files don't scale differently in a tool vs filesystem
- **Structure:** No enforced schema needed beyond filenames + directories
- **UX:** `ls`/`cat`/`git` is already the UX

### When to build a tool vs use markdown + grep + git
A dedicated tool earns its existence when it provides:
1. **Scale** — Data outgrows a single file or needs indexed queries
2. **Forced structure** — Consistent schema enables cross-record analysis
3. **Better UX** — CLI gives human/agent something grep can't

## Development Cycle (per tool)

Each tool follows this loop:

1. 🔍 **Research** — Survey prior art, patterns, avoid reinventing wheels
2. 📋 **Plan** — Distill findings into a design doc
3. 🎯 **Steer** — Eric reviews, adjusts direction
4. 🔨 **Build** — Dev sessions implement (Go, each tool = own GitHub repo)
5. ✅ **Evaluate** — Test, measure quality, track prompt effectiveness
6. 🔄 **Guide next cycle** — Learnings feed back into research for next tool

## Status

| Tool | Phase | Status |
|------|-------|--------|
| agent-memory | Done ✅ | All phases complete |
| token-eval | Build ✅ | Phase 1 done (capture). Phase 2 (export/summary) deferred |
| teeny-orchestrator | Done ✅ | Core loop, daemon, eval self-review, cron, multi-provider, base_url — 77 tests |
| todo-mgmt | Done ✅ | Session-scoped focus tool — 15/15 tests |
| research-helper | Dropped | Agent + web_search is already the research helper |
| plan-doc | — | Not started |

## Notes

- Each tool is a separate GitHub project under `github.com/rcliao/`
- Language: Go, pure Go deps (no CGo), standalone binaries
- Keep tools tiny and composable — unix philosophy
- Text in, text out
- Agents should be able to build and use their own tools (dogfooding)
