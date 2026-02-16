# agent-memory — Design Doc

> Foundation tool for the teeny-claw constellation.
> Status: **Plan** | Author: teenyclaw | Date: 2026-02-16

## Overview

A tiny CLI for persistent agent memory. Text in, text out. SQLite-backed, single binary, no server. Agents call it to remember things and recall them later.

## Goals

- Dead simple CLI interface — any agent can use it
- Text in, text out — no vectors or embeddings in the API
- Immutable append-only storage — updates create versions, nothing lost
- Recency-biased retrieval — latest is default
- Single binary, zero config, works out of the box
- Foundation for the rest of the teeny-claw tools

## Non-Goals (v1)

- No server/daemon mode
- No MCP protocol support
- No built-in embedding model (FTS5 only)
- No multi-user / auth
- No TTL / auto-expiry

---

## Data Model

### Two-Level Storage: Memories + Chunks

A memory is the logical unit (what the agent stores). Content can range from a single sentence to a long markdown document. When content is long, we **internally chunk it** so search can match against specific sections — but the agent always gets back the full memory. Chunking is an internal optimization, invisible in the API.

```
Agent perspective:        Internal storage:
┌──────────────┐         ┌──────────────┐
│   Memory     │         │   memories   │  (the record)
│  (ns + key)  │────────▶│   + content  │
│              │         └──────┬───────┘
│  long markdown         ┌─────▼───────┐
│  content...  │         │   chunks     │  (search index)
│              │         │  chunk 1     │
│              │         │  chunk 2     │
│              │         │  chunk N     │
└──────────────┘         └─────────────┘
```

### Schema

```sql
CREATE TABLE memories (
    id          TEXT PRIMARY KEY,  -- ulid (sortable, unique)
    ns          TEXT NOT NULL,     -- namespace e.g. "project:teeny-claw"
    key         TEXT NOT NULL,     -- logical key within namespace
    content     TEXT NOT NULL,     -- the actual memory (can be long markdown)
    kind        TEXT NOT NULL DEFAULT 'semantic',  -- semantic | episodic | procedural
    tags        TEXT,              -- JSON array: ["deploy", "infra"]
    version     INTEGER NOT NULL DEFAULT 1,
    supersedes  TEXT,              -- id of previous version (NULL for first)
    created_at  TEXT NOT NULL,     -- RFC3339
    deleted_at  TEXT,              -- soft delete
    priority    TEXT NOT NULL DEFAULT 'normal', -- low | normal | high | critical
    access_count INTEGER NOT NULL DEFAULT 0,
    last_accessed_at TEXT,              -- updated on get/search hit
    meta        TEXT               -- JSON object for future extensibility
);

CREATE INDEX idx_memories_ns_key ON memories(ns, key);
CREATE INDEX idx_memories_ns_kind ON memories(ns, kind);
CREATE INDEX idx_memories_created ON memories(created_at DESC);
CREATE INDEX idx_memories_deleted ON memories(deleted_at);
CREATE INDEX idx_memories_priority ON memories(ns, priority);

-- Relations between memories
CREATE TABLE memory_links (
    from_id TEXT NOT NULL REFERENCES memories(id),
    to_id   TEXT NOT NULL REFERENCES memories(id),
    rel     TEXT NOT NULL,  -- relates_to | contradicts | depends_on | refines
    created_at TEXT NOT NULL,
    PRIMARY KEY (from_id, to_id, rel)
);

CREATE INDEX idx_links_to ON memory_links(to_id);

-- Chunks: internal search index over memory content
-- Agent never sees these directly
CREATE TABLE chunks (
    id          TEXT PRIMARY KEY,  -- ulid
    memory_id   TEXT NOT NULL REFERENCES memories(id),
    seq         INTEGER NOT NULL,  -- chunk order within memory
    text        TEXT NOT NULL,     -- chunk text
    start_line  INTEGER,           -- line offset in original content
    end_line    INTEGER
);

CREATE INDEX idx_chunks_memory ON chunks(memory_id);

-- Full-text search over chunks
CREATE VIRTUAL TABLE chunks_fts USING fts5(
    text,
    content=chunks,
    content_rowid=rowid
);
```

### Chunking Strategy

- **Short content (<500 chars):** No chunking. Single chunk = full content.
- **Long content:** Split on markdown boundaries (headings, double newlines, list groups). Target ~300-500 chars per chunk with overlap.
- Chunking happens on `put`. When a new version is created, old chunks remain (tied to old memory version).
- Chunk boundaries are best-effort — we're not parsing a full AST, just splitting on natural markdown breaks.

### ID Strategy

ULIDs — lexicographically sortable by time, globally unique, no coordination needed. Go lib: `github.com/oklog/ulid/v2`.

### Versioning Model

```
put(ns="p:tc", key="deploy", content="v1 text")
  → creates memory id=01ABC, version=1, supersedes=NULL

put(ns="p:tc", key="deploy", content="v2 text")
  → creates memory id=01DEF, version=2, supersedes="01ABC"

get(ns="p:tc", key="deploy")
  → returns id=01DEF (latest non-deleted version)

get(ns="p:tc", key="deploy", --history)
  → returns [01DEF, 01ABC] (newest first)
```

Finding the latest version: query by `(ns, key)` where `deleted_at IS NULL`, order by `version DESC`, limit 1.

---

## CLI Specification

### Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--db` | `-d` | `$AGENT_MEMORY_DB` or `~/.agent-memory/memory.db` | Database path |
| `--format` | `-f` | `json` | Output format: `json` or `text` |

### Commands

#### `put` — Store a memory

```
agent-memory put [flags] [content]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--ns` | `-n` | yes | Namespace |
| `--key` | `-k` | yes | Key (unique within namespace) |
| `--kind` | | no | `semantic` (default), `episodic`, `procedural` |
| `--tags` | `-t` | no | Comma-separated tags |
| `--priority` | `-p` | no | `low`, `normal` (default), `high`, `critical` |
| `--meta` | | no | JSON string for metadata |

Content can be positional arg, or piped via stdin (stdin takes priority if both).

**Output:** JSON of the created memory.

```json
{"id":"01ABC...","ns":"project:tc","key":"deploy","version":1,"created_at":"2026-02-16T15:30:00Z"}
```

If key already exists in namespace: creates new version, links `supersedes` to previous.

#### `get` — Retrieve a memory

```
agent-memory get [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--ns` | `-n` | yes | Namespace |
| `--key` | `-k` | yes | Key |
| `--history` | | no | Return all versions (newest first) |
| `--version` | `-v` | no | Specific version number |

**Output:** JSON object (or array with `--history`).

#### `search` — Find memories

```
agent-memory search [flags] <query>
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--ns` | `-n` | no | Filter by namespace (omit = search all) |
| `--kind` | | no | Filter by kind |
| `--tags` | `-t` | no | Filter by tags (comma-separated, AND logic) |
| `--limit` | `-l` | no | Max results (default 10) |

Uses FTS5 for ranking. Returns only latest version of each key. Results ordered by relevance × recency (FTS5 rank + created_at bias).

**Output:** JSON array of matching memories.

#### `list` — List memories

```
agent-memory list [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--ns` | `-n` | no | Filter by namespace |
| `--kind` | | no | Filter by kind |
| `--tags` | `-t` | no | Filter by tags |
| `--limit` | `-l` | no | Max results (default 20) |
| `--keys-only` | | no | Only output ns/key pairs |

Returns latest version of each key, newest first.

#### `rm` — Soft-delete a memory

```
agent-memory rm [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--ns` | `-n` | yes | Namespace |
| `--key` | `-k` | yes | Key |
| `--all-versions` | | no | Delete all versions (default: only latest) |
| `--hard` | | no | Permanent delete (irreversible) |

Default: sets `deleted_at` on latest version. With `--all-versions`, marks all versions.

#### `context` — Assemble relevant memories for a task

The power command. Agent describes what it's about to do, gets back a curated bundle of memories that fits within a token budget.

```
agent-memory context [flags] <description>
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--ns` | `-n` | no | Filter by namespace (omit = all) |
| `--budget` | `-b` | no | Max tokens in output (default 4000) |
| `--kind` | | no | Filter by kind |
| `--tags` | `-t` | no | Filter by tags |

**How it works:**
1. Search for memories matching the description (FTS5 / future embeddings)
2. Score each memory: `relevance × 0.4 + recency × 0.2 + importance × 0.2 + access_frequency × 0.2`
3. Greedily pack top-scored memories into the token budget
4. For long memories, include the most relevant chunks (not necessarily the full content) to maximize coverage within budget
5. Return assembled context as a structured JSON array, ordered by score

**Output:**
```json
{
  "budget": 4000,
  "used": 3847,
  "memories": [
    {"ns": "project:tc", "key": "language", "kind": "semantic", "content": "...", "score": 0.95},
    {"ns": "project:tc", "key": "deploy", "kind": "procedural", "content": "...", "score": 0.82, "excerpt": true}
  ]
}
```

When `excerpt: true`, the content was trimmed to the most relevant chunks to fit the budget. The agent can always `get` the full memory by key if needed.

#### `link` — Create relations between memories

```
agent-memory link [flags]
```

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--from` | | yes | Source memory (ns:key or id) |
| `--to` | | yes | Target memory (ns:key or id) |
| `--rel` | `-r` | yes | Relation: `relates_to`, `contradicts`, `depends_on`, `refines` |
| `--rm` | | no | Remove the link instead |

Related memories are surfaced in `get` output and boosted in `search`/`context` when neighbors match.

#### `ns` — Namespace management

```
agent-memory ns list                          # list all namespaces
agent-memory ns stats --ns "project:tc"       # count, size, last updated, kinds breakdown
```

#### `compact` — Prepare memories for summarization *(deferred, maybe-never)*

> With good scoring in `context`, compact is mostly unnecessary — irrelevant memories
> naturally fade in ranking. Only needed if DB size becomes a real concern.

```
agent-memory compact --ns "project:tc" --kind episodic [--before 2026-01-01]
```

Outputs all matching memories as a single document for agent summarization.

#### `export` / `import` — Backup and restore

```
agent-memory export [--ns <namespace>] > backup.json
agent-memory import < backup.json
```

Export: JSONL (one memory per line). Import: upserts, preserves IDs and versions.

---

## Project Structure

```
agent-memory/
├── cmd/
│   └── agent-memory/
│       └── main.go          # CLI entrypoint (cobra)
├── internal/
│   ├── store/
│   │   ├── store.go         # Store interface
│   │   ├── sqlite.go        # SQLite implementation
│   │   └── sqlite_test.go
│   ├── model/
│   │   └── memory.go        # Memory struct, validation
│   ├── search/
│   │   └── fts.go           # FTS5 search + ranking
│   ├── chunker/
│   │   ├── chunker.go       # Markdown-aware chunking
│   │   └── chunker_test.go
│   ├── context/
│   │   └── assembler.go     # Budget-aware context assembly
│   └── cli/
│       ├── put.go
│       ├── get.go
│       ├── search.go
│       ├── context.go
│       ├── list.go
│       ├── link.go
│       ├── ns.go
│       ├── compact.go
│       ├── rm.go
│       └── export.go
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── .goreleaser.yml          # cross-platform builds
```

## Dependencies (minimal)

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `modernc.org/sqlite` | Pure Go SQLite (no CGo) |
| `github.com/oklog/ulid/v2` | ULID generation |

That's it. Three deps.

## Search Ranking (v1)

Search matches against **chunks** but returns **full memories**. If multiple chunks from the same memory match, we take the best chunk score for that memory.

```sql
SELECT m.*, MAX(fts.rank) AS best_rank
FROM chunks c
JOIN chunks_fts fts ON c.rowid = fts.rowid
JOIN memories m ON c.memory_id = m.id
WHERE chunks_fts MATCH ?
  AND m.deleted_at IS NULL
GROUP BY m.id
ORDER BY (MAX(fts.rank) * 0.7) + (julianday(m.created_at) / julianday('now') * 0.3) DESC
LIMIT ?
```

This blends chunk-level text relevance (70%) with memory recency (30%). The agent gets back full memory content, not individual chunks. For long memories, we could optionally include a `matched_excerpt` field showing which section matched.

## Search Ranking (v1.1 — future)

Add internal embedding via a small local model. The interface doesn't change — `search` just gets smarter. Options to evaluate:
- Bundle a small GGUF embedding model (~50MB)
- Use `chromem-go` (pure Go, has built-in embedding)
- Hybrid: FTS5 for candidate retrieval, re-rank with cosine similarity

## Build Plan

### Phase 1 — Core (v0.1.0)
- [ ] Project scaffold (go mod, cobra, Makefile)
- [ ] SQLite store with schema migration
- [ ] Markdown chunker (split on headings/paragraphs, ~300-500 char targets)
- [ ] `put` command (with stdin support, versioning, auto-chunking, priority)
- [ ] `get` command (latest, --history, --version) with access tracking
- [ ] `list` command
- [ ] `rm` command (soft-delete)
- [ ] JSON output formatting
- [ ] Unit tests for store + chunker
- [ ] README with usage examples

### Phase 2 — Search + Context (v0.2.0)
- [ ] FTS5 index over chunks
- [ ] `search` command — match chunks, return full memories, composite scoring (relevance + recency + importance + access frequency)
- [ ] Optional `matched_excerpt` in search results
- [ ] Namespace/kind/tag filtering on search
- [ ] `context` command — budget-aware assembly with greedy packing
- [ ] `ns list` and `ns stats` commands
- [ ] Integration tests

### Phase 3 — Relations + Polish (v0.3.0)
- [ ] `link` command (create/remove relations)
- [ ] Related memory surfacing in `get` output
- [ ] Relation-aware boosting in `search`/`context`
- [ ] `export` / `import` commands
- [ ] `--format text` human-readable output
- [ ] `--db` flag and env var support
- [ ] Auto-create DB directory
- [ ] goreleaser config for cross-platform binaries
- [ ] CI (GitHub Actions)

### Phase 4 — Smart Search (v1.0.0)
- [ ] Local embedding model integration
- [ ] Hybrid FTS5 + vector search
- [ ] Embedding-aware `context` assembly
- [ ] Benchmark: search quality and latency
- [ ] Conflict detection (surface contradicting memories)

---

## Example Session

```bash
# Simple fact — stored as-is, no chunking needed
$ agent-memory put -n "user:prefs" -k "editor" "Prefers Neovim with Lazy plugin manager"
{"id":"01HX...","ns":"user:prefs","key":"editor","version":1}

# Critical fact — priority ensures it ranks high forever
$ agent-memory put -n "user:prefs" -k "allergies" -p critical "Allergic to peanuts"
{"id":"01HW...","ns":"user:prefs","key":"allergies","version":1,"priority":"critical"}

# Long session recap — gets auto-chunked internally
$ cat <<'EOF' | agent-memory put -n "project:teeny-claw" -k "session-2026-02-16" --kind episodic
# Session: 2026-02-16

## Research Phase
Surveyed agent memory landscape. Key players: MemGPT/Letta (OS paradigm),
Mem0 (cloud), Memori (SQL-native), MCP Memory Server (JSON flat file).

## Design Decisions
- SQLite + pure Go (modernc.org/sqlite) for zero-CGo cross-compile
- Immutable versioning — updates create new versions
- Text in, text out — embeddings handled internally
- FTS5 for v1 search, local embedding model for v1.1

## Open Items
- Token budget for context assembly
- Chunking strategy for long markdown
EOF
{"id":"01HY...","ns":"project:teeny-claw","key":"session-2026-02-16","version":1,"chunks":3}

# Search matches against chunks, returns full memories
$ agent-memory search -n "project:teeny-claw" "what storage backend"
[{"id":"01HY...","key":"session-2026-02-16","score":0.87,"matched_excerpt":"SQLite + pure Go (modernc.org/sqlite) for zero-CGo cross-compile",...}]

# Context assembly — the power move
# "I'm about to build the agent-memory CLI, give me everything relevant"
$ agent-memory context -n "project:teeny-claw" --budget 2000 "building the CLI tool"
{
  "budget": 2000,
  "used": 1847,
  "memories": [
    {"key":"session-2026-02-16","score":0.95,"excerpt":true,"content":"## Design Decisions\n- SQLite + pure Go..."},
    {"key":"language","score":0.82,"content":"Go 1.23+. All tools are standalone Go binaries."}
  ]
}

# Link related memories
$ agent-memory link --from "project:teeny-claw:language" --to "project:teeny-claw:session-2026-02-16" --rel relates_to

# Namespace stats
$ agent-memory ns stats -n "project:teeny-claw"
{"ns":"project:teeny-claw","memories":5,"versions":7,"kinds":{"semantic":3,"episodic":2},"last_updated":"2026-02-16T16:00:00Z"}

# After many sessions, compact episodic memories for summarization
$ agent-memory compact -n "project:teeny-claw" --kind episodic --before 2026-02-01
# Outputs all matching memories as one doc → agent summarizes → stores as new semantic memory
```

---

## Open Items

- **Content size limit:** No hard cap for v1. Log a warning if >100KB.
- **Namespace conventions:** Document recommended patterns (e.g., `user:*`, `project:*`, `agent:*`) but don't enforce.
- **Concurrency:** SQLite WAL mode for safe concurrent reads. Single-writer is fine for CLI usage.
