# todo-mgmt Research: CLI Task Management for AI Agents

> Research date: 2026-02-16

## 1. Landscape Survey: Existing CLI Task Tools

### Taskwarrior
- **What:** The gold standard CLI task manager. C++, massive feature set.
- **Data model:** UUID-identified tasks with ~25 built-in fields (status, description, project, tags, priority H/M/L, due, scheduled, wait, depends, urgency score, UDAs). Statuses: pending, completed, deleted, waiting, recurring.
- **Storage:** v3 uses TaskChampion (SQLite-like local DB with sync protocol). v2 used flat JSON-per-line files.
- **CLI pattern:** `task add`, `task 5 done`, `task 5 modify priority:H`, `task next`. Filter expressions: `task project:home +urgent list`.
- **What works:** Urgency algorithm auto-sorts "what's next". Rich filtering. UDAs for extensibility. `task next` is exactly the query pattern agents need.
- **What's overbuilt:** Recurrence, holidays, color themes, 100+ config options, complex report system. The urgency formula has ~15 coefficients. Way too much for our use case.
- **Key takeaway:** The urgency-based `next` command is the killer feature. The data model has good bones but is bloated.

### Ultralist
- **What:** Fast, simple CLI task manager. Written in Go.
- **Data model:** Tasks with subject, project (+tag), context (@tag), due date, status (pending/completed/archived), notes, priority (boolean).
- **Storage:** `.todos.json` — single JSON file per project directory.
- **CLI pattern:** `ultralist add "do thing +project @context"`, `ultralist complete 1`, `ultralist list`. Natural language in subject line.
- **What works:** Go binary, fast, simple JSON storage, project-local. The inline `+project @context` parsing is slick.
- **What's overbuilt:** Web app sync, Slack integration (abandoned). Has a webapp nobody uses.
- **Key takeaway:** Right language (Go), right simplicity level. JSON file is fine for humans, bad for querying at scale.

### dstask
- **What:** Git-backed task manager, Go, single binary. Inspired by taskwarrior.
- **Data model:** Tasks with summary, tags, project, priority (P0-P3, default P2), status, notes (markdown file per task).
- **Storage:** YAML files in a git repo (~/.dstask/). One file per task, organized by status directories.
- **CLI pattern:** `dstask add P1 +project do the thing`, `dstask done 5`, `dstask next`. Context-aware filtering.
- **What works:** Git sync for free. Markdown notes per task. P0-P3 priority is clean. Single binary Go.
- **What's overbuilt:** Git as storage adds complexity for our case (agent doesn't need merge conflict resolution).
- **Key takeaway:** P0-P3 priority and per-task notes are good patterns. File-per-task is interesting but SQLite is better for querying.

### TaskLite
- **What:** Haskell CLI task manager with **SQLite backend**. Inspired by Taskwarrior.
- **Data model:** Similar to Taskwarrior but cleaner. ULID-based IDs. Tasks, tags, notes as separate tables.
- **Storage:** SQLite database. This is the closest prior art to what we want.
- **CLI pattern:** `tasklite add "do thing"`, `tasklite head` (show top tasks).
- **What works:** SQLite storage means real queries. ULID for sortable unique IDs. Clean relational model.
- **What's overbuilt:** Haskell dependency is brutal. GraphQL server and webapp are scope creep.
- **Key takeaway:** **This is the closest existing tool to what we want.** SQLite backend is the right call. But Haskell is a non-starter for us.

### doing (Brett Terpstra)
- **What:** Ruby CLI for logging what you're doing. Activity logger, not task manager.
- **Data model:** Timestamped entries in sections (Currently, Done, Later). Plain text.
- **Storage:** Single plain text file (~/.doing).
- **CLI pattern:** `doing now "working on X"`, `doing done`, `doing last`, `doing later "check this"`.
- **What works:** The `now/done/last` pattern is exactly how an agent thinks. Dead simple.
- **What's overbuilt:** Ruby dependency. Plugin system. Not really a task manager.
- **Key takeaway:** The activity log pattern (`now`/`done`/`last`) is valuable for agent session tracking. Consider incorporating this pattern.

### t (minimal todo)
- **What:** Ultra-minimal Python todo manager.
- **Storage:** Plain text files, one task per line with hash ID prefix.
- **Key takeaway:** Too minimal. No structure for agent use.

## 2. Agent Task Tracking Patterns

### Claude Code Tasks (just released)
- Tasks are **in-memory only** — disappear when session ends
- `~/.claude/tasks/` has lock files for coordination, not task data
- Sub-agents can create/complete tasks for coordination within a session
- **Gap:** No cross-session persistence. No way for cron sessions to know what's pending.
- This is literally the problem we're solving.

### Common Agent Patterns
- **TODO.md / PLAN.md files:** Most agents use markdown files for task tracking. Works but unstructured — hard to query "what's next?"
- **Devin:** Uses internal planner that creates step-by-step plans, tracks progress. Opaque, not externalizable.
- **Copilot Workspace:** Breaks issues into specification → plan → implementation. Plan is a structured list of file changes.
- **Microsoft Magentic-One:** "Task ledger" pattern — orchestrator maintains a structured ledger of goals, subgoals, and progress.

### Key Insight: The "Task Ledger" Pattern
From Microsoft's research and real-world agent frameworks, the pattern that works:
1. **Goal decomposition:** Break high-level goal into ordered tasks
2. **Task state tracking:** Each task has a clear status (todo/active/done/blocked/skipped)
3. **Session continuity:** Agent reads ledger at session start, picks up where it left off
4. **Progress notes:** Each task accumulates notes about what was tried/learned

## 3. Task Decomposition Analysis

### Hierarchical vs Flat
- **Flat lists** are simpler and work for most agent use cases. Agents don't naturally think in deep hierarchies.
- **One level of nesting** (goal → tasks) is the sweet spot. A "parent_id" field handles this without recursive complexity.
- Deep hierarchies (3+ levels) add query complexity with minimal benefit.

### Dependencies
- Full DAG dependencies (taskwarrior's `depends:`) are overbuilt for agent use.
- **Sequential ordering** (priority + position) handles 90% of cases.
- Simple `blocked_by` field (single task ID) covers the remaining 10%.

### "What's Next?" Algorithm
The simplest effective algorithm:
1. Filter to status=todo
2. Sort by priority (P0 > P1 > P2 > P3)
3. Within same priority, sort by position/creation order
4. Return first result

No urgency coefficients, no due-date decay functions. Just priority + order.

## 4. Synthesized Recommendations for todo-mgmt

### Minimal Viable Task Structure

```sql
CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,  -- ULID (sortable, unique)
    summary     TEXT NOT NULL,     -- one-line description
    status      TEXT NOT NULL DEFAULT 'todo',  -- todo|active|done|skipped|blocked
    priority    INTEGER NOT NULL DEFAULT 2,     -- 0-3 (P0=critical, P3=someday)
    position    INTEGER NOT NULL DEFAULT 0,     -- ordering within same priority
    parent_id   TEXT,              -- optional, for goal→task grouping
    project     TEXT,              -- optional grouping label
    tags        TEXT,              -- comma-separated or JSON array
    notes       TEXT,              -- accumulated markdown notes
    created_at  TEXT NOT NULL,     -- RFC3339
    updated_at  TEXT NOT NULL,     -- RFC3339
    started_at  TEXT,              -- when status changed to active
    completed_at TEXT              -- when status changed to done
);
```

**That's 13 fields.** Taskwarrior has 25+. This is the minimum that an agent needs.

### CLI Interface

```bash
# Core CRUD
todo add "implement auth middleware" -p 1 --project api
todo add "write tests for auth" -p 2 --parent <task-id>
todo list                          # all non-done tasks
todo list --project api            # filtered
todo next                          # THE key command: what should I do?

# Status transitions
todo start <id>                    # todo → active
todo done <id>                     # → done
todo skip <id> --note "not needed" # → skipped
todo block <id> --note "waiting on X"  # → blocked
todo reset <id>                    # back to todo

# Notes (append-only log)
todo note <id> "tried approach X, didn't work because Y"

# Bulk operations
todo import                        # from PLAN.md or JSON stdin
todo export                        # JSON to stdout

# Session helpers
todo active                        # what's currently in-progress?
todo summary                       # quick stats: 3 done, 5 todo, 1 blocked
```

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage | SQLite | Queryable, single file, concurrent-safe, zero config |
| IDs | ULID | Sortable by creation time, globally unique, CLI-friendly (short prefix matching) |
| Priority | P0-P3 integers | Borrowed from dstask. Simple, clear, sortable |
| Hierarchy | Single parent_id | One level of nesting. Goals have tasks. That's it. |
| Dependencies | None (use priority/position) | YAGNI. If needed later, add `blocked_by` field |
| Notes | Append-only text field | Not markdown files. Keep it in the DB for queryability |
| Config | None | DB location via env var `TODO_DB` or default `~/.local/share/todo-mgmt/tasks.db` |
| Output | JSON + human table | `--json` flag for agent consumption, pretty table for humans |

### What NOT to Build
- ❌ Recurrence / repeating tasks
- ❌ Due dates / scheduling (agents work continuously, not on deadlines)
- ❌ Urgency algorithms (P0-P3 + position is enough)
- ❌ Sync / server / web UI
- ❌ Config files
- ❌ Color themes
- ❌ Undo/history

### Integration Points
- **plan-doc → todo-mgmt:** `todo import` reads structured plan and creates tasks
- **Agent cron session:** Starts with `todo next --json`, works on it, `todo done <id> && todo note <id> "completed: ..."`, then `todo next --json` again
- **Human oversight:** `todo list`, `todo add`, reprioritize with `todo edit <id> -p 0`

### Answer to the Key Question

> What's the minimal viable task structure that an agent needs to be productive across sessions?

**A task needs exactly 5 things:**
1. **Identity** — unique ID the agent can reference
2. **What** — summary text describing the work
3. **Status** — is it todo, in-progress, or done?
4. **Priority** — what order to work in
5. **Notes** — accumulated context from previous sessions

Everything else (project, tags, parent, timestamps) is nice-to-have for organization but not strictly necessary. The schema above includes them because they're cheap and useful, but the core loop is:

```
next → start → (work) → note → done → next
```

That's the whole agent workflow. Build for that loop.
