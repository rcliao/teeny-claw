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

### 3. `todo-mgmt` ✅ — Execution Bridge
**What:** The bridge between planning and execution. Agent makes a plan → todos track what needs doing → agent works through them across sessions → human can see progress and reprioritize.
**Why:** Agents need a "what should I do next?" answer every session. Memory is context, todos are intent.
**Depends on:** agent-memory (persistence)
**Key problem:** Cron sessions need to pick up work, know what's done, what's blocked, what's next. Currently we use MEMORY.md + roadmap, but a structured task store would be cleaner.
**Research topics:** Task CLI tools, priority/dependency tracking, agent task decomposition patterns.

### 4. `research-helper` 🔍 — Structured Investigation
**What:** Two-tier deep research: plan what to research → execute research queries → synthesize findings. Not just "search the web" but structured investigation with a query plan phase.
**Why:** Current research is ad-hoc web_search calls. A structured approach produces better, more thorough results.
**Depends on:** agent-memory (storing findings), todo-mgmt (tracking research tasks)
**Key problem:** Research quality depends on query planning. Bad queries → shallow results. The tool should enforce plan-first research.
**Research topics:** Query decomposition, multi-source synthesis, claim verification, search API integration.

### 5. `plan-doc` 📋 — Human-Agent Collaboration (Capstone)
**What:** Collaborative planning between human and agent via a structured doc with inline comments. Like Claude Code's plan mode but TUI-friendly — agent proposes, human comments/steers, agent revises.
**Why last:** Uses everything — research feeds plans, todos track execution.
**Depends on:** All of the above.
**Key problem:** Current planning is chat-based — linear, hard to reference, easy to lose context. A doc with comments gives both sides a shared artifact to iterate on.
**Research topics:** Human-in-the-loop planning, comment-based steering, structured plan formats, TUI design patterns.

## ~~agent-artifacts~~ — DROPPED
Originally planned as file/artifact management. Decided the filesystem + git + agent-memory already covers this. Agents don't need a separate artifact store — they need good orchestration of context via memory.

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
| todo-mgmt | — | Not started — next up |
| research-helper | — | Not started |
| plan-doc | — | Not started |

## Notes

- Each tool is a separate GitHub project under `github.com/rcliao/`
- Language: Go, pure Go deps (no CGo), standalone binaries
- Keep tools tiny and composable — unix philosophy
- Text in, text out
- Agents should be able to build and use their own tools (dogfooding)
