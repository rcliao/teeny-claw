# Teeny-Claw Roadmap 🔧⚡

## Vision

A collection of tiny, agent-specific CLI tools — each its own GitHub project. Agents build and use their own tools. Each tool goes through the **Research → Plan → Steer → Build → Evaluate** cycle before building.

## Tool Constellation & Build Order

Build order follows the dependency graph — bottom-up from foundational to capstone.

```
                    ┌─────────────┐
                    │  plan-doc   │  ← 6. Capstone
                    │ (steering)  │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  todo-mgmt  │  ← 5. Management
                    │   (tasks)   │
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
  ┌──────▼──────┐  ┌───────▼──────┐  ┌──────▼──────┐
  │ token-eval  │  │   research   │  │   agent-    │  ← 2-4. Middle layer
  │  (metrics)  │  │   helper     │  │  artifacts  │
  └──────┬──────┘  └───────┬──────┘  └──────┬──────┘
         │                 │                 │
         └─────────────────┼─────────────────┘
                           │
                    ┌──────▼──────┐
                    │agent-memory │  ← 1. Foundation
                    │ (storage)   │
                    └─────────────┘
```

## Build Order (Bottom-Up)

### 1. `agent-memory` 🧱 — Foundation
**What:** Persistent structured storage for agent data. Simple read/write/query CLI.
**Why first:** Everything else needs somewhere to persist state.
**Research topics:** Existing agent memory systems, structured vs vector storage, embedding approaches, sqlite vs file-based.

### 2. `token-eval` 📊 — Measurement
**What:** Token/cost tracking & evaluation per session/workflow. CLI that wraps agent runs and records metrics.
**Why second:** Once this exists, every subsequent tool we build gets cost tracking for free. Also validates our own dev cycle spend.
**Depends on:** agent-memory (for storing metrics over time)
**Research topics:** Token counting approaches, cost estimation APIs, quality signal heuristics.

### 3. `agent-artifacts` 📦 — Structure
**What:** Shared structured artifact store for multi-agent handoffs. Common read/write format (not free-form text blobs).
**Why third:** Enables real multi-agent workflows with context continuity.
**Depends on:** agent-memory (for storage backend)
**Research topics:** Anthropic Memory object pattern, shared-repo approaches from autonomous research papers, artifact schemas.

### 4. `research-helper` 🔍 — Intelligence
**What:** Standalone research CLI. Complement to OpenClaw's deep-research skill.
**Why fourth:** Can use artifacts for structured output, token-eval for cost awareness.
**Depends on:** agent-artifacts (structured output), token-eval (cost tracking)
**Research topics:** Search API integration, query decomposition, result ranking, dedup strategies.

### 5. `todo-mgmt` ✅ — Management
**What:** Task management for agents. Track work items, priorities, dependencies across projects.
**Why fifth:** Agents can now track their own work with persistence + cross-agent sharing.
**Depends on:** agent-memory (persistence), agent-artifacts (cross-agent task sharing)
**Research topics:** Task management CLIs, priority algorithms, dependency graphs.

### 6. `plan-doc` 📋 — Steering (Capstone)
**What:** Planning docs with inline comments for human steering. Agents write plans, humans steer via comments.
**Why last:** Uses everything — research feeds plans, todos track execution, artifacts carry context, token-eval watches costs.
**Depends on:** All of the above.
**Research topics:** Human-in-the-loop patterns, comment-based steering, plan formats.

## Development Cycle (per tool)

Each tool follows this loop:

1. 🔍 **Research** — Use deep-research skill to survey prior art, patterns, avoid reinventing wheels
2. 📋 **Plan** — Distill findings into a design doc
3. 🎯 **Steer** — EV reviews, adjusts direction
4. 🔨 **Build** — Dev agents implement (Go, each tool = own GitHub repo)
5. ✅ **Evaluate/Validate** — Test, measure quality, track costs
6. 🔄 **Guide next cycle** — Learnings feed back into research for next tool

## Status

| Tool | Phase | Status |
|------|-------|--------|
| agent-memory | — | Not started |
| token-eval | — | Not started |
| agent-artifacts | — | Not started |
| research-helper | — | Not started |
| todo-mgmt | — | Not started |
| plan-doc | — | Not started |

## Notes

- Each tool is a separate GitHub project under `github.com/rcliao/`
- Language: Go
- Keep tools tiny and composable — unix philosophy
- Agents should be able to build and use their own tools (dogfooding)
