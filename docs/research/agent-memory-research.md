# agent-memory — Research Notes

> Research phase for `agent-memory`, the foundation tool in the teeny-claw constellation.
> Date: 2026-02-16

## What We're Building

A tiny, standalone CLI for persistent structured storage that agents can use to read/write/query memories. Written in Go, single binary, no server needed.

## Landscape Survey

### Existing Systems

| System | Approach | Notes |
|--------|----------|-------|
| **MemGPT / Letta** | OS paradigm — treats context as RAM, external storage as disk. LLM manages its own paging. | Heavyweight, Python, full framework. Interesting conceptual model but way more than we need. |
| **Mem0** | Cloud memory service with API. Auto-extracts facts from conversations. | SaaS dependency, not self-hosted-first. |
| **Memori (GibsonAI)** | SQL-native memory layer. Stores structured memories in SQLite/Postgres. | Closest to our philosophy — SQL-backed, open source. But Python, library not CLI. |
| **MCP Memory Server** | Anthropic's reference — knowledge graph stored as JSON file. Entities + relations + observations. | Very simple. JSON flat file. Good starting point conceptually but no search beyond exact match. |
| **LangMem (LangChain)** | Memory extraction + management via LangGraph store. | Framework-locked, Python. |
| **Google ADK Memory** | Go-based memory in Google's Agent Dev Kit. Vertex AI Search backend. | Go, but cloud-dependent. |
| **Basic Memory (MCP)** | Markdown files → semantic graph. Local-first. | Interesting local-first approach. |

### Storage Approaches

1. **Flat files (JSON/Markdown)** — Simplest. MCP memory server does this. No query capability beyond load-all.
2. **SQLite** — Sweet spot for us. Single file, no server, rich querying, Go has great support (`modernc.org/sqlite` for pure Go, or CGo `mattn/go-sqlite3`).
3. **SQLite + vector search** — `sqlite-vec` (pure C extension) or `sqvect` (pure Go). Enables semantic search without external services.
4. **Knowledge graph** — Entities/relations/observations. Good for structured facts. Can layer on SQLite.

### Memory Types (from literature)

| Type | What | Example |
|------|------|---------|
| **Episodic** | Events that happened | "User deployed v2.1 on 2026-01-15" |
| **Semantic** | Facts/knowledge | "User prefers dark mode, uses Go" |
| **Procedural** | How to do things | "To deploy: run `make deploy`" |
| **Working** | Current session context | (handled by the LLM context window, not us) |

### Key Design Patterns

1. **Write-back cycle (MemGPT)** — LLM decides when to persist. Memory pressure triggers save.
2. **Auto-extraction (Mem0/OpenAI)** — System extracts facts from conversation automatically.
3. **Explicit tools (MCP)** — Agent explicitly calls `add_memory`, `search_memory`, etc.
4. **Hybrid fact + semantic (OpenAI)** — Structured facts prepended to prompt + semantic search for deeper recall.

## Design Decisions

### What makes sense for us

We're building a **CLI tool that agents call explicitly**. Not a framework, not a library, not a server. The agent (OpenClaw, Claude Code, Codex, whatever) calls our binary when it wants to remember or recall something.

### Recommended Architecture

```
agent-memory
├── Storage: SQLite (single file, portable)
├── Memory model: Namespaced key-value with metadata
│   ├── namespace (e.g., "project:teeny-claw", "user:preferences")
│   ├── key (unique within namespace)
│   ├── content (text blob — the actual memory)
│   ├── kind (semantic | episodic | procedural)
│   ├── tags (comma-separated or JSON array)
│   ├── created_at, updated_at
│   └── embedding (optional, for semantic search)
├── Search: FTS5 (full-text) + optional vector search
└── Output: JSON (for agent consumption)
```

### CLI Interface (draft)

Pure text in, text out. No vectors, no embeddings in the interface.

```bash
# Store a memory (text in)
agent-memory put --ns "project:teeny-claw" --key "deploy-process" \
  --kind procedural --tags "deploy,infra" \
  "Run make deploy from the project root. Requires AWS credentials."

# Can also pipe content in
echo "Long procedural doc..." | agent-memory put --ns "project:teeny-claw" --key "deploy-process"

# Get by key (returns latest version, JSON out)
agent-memory get --ns "project:teeny-claw" --key "deploy-process"

# Get with full version history
agent-memory get --ns "project:teeny-claw" --key "deploy-process" --history

# Search (text in → ranked results out, recency-biased)
agent-memory search --ns "project:teeny-claw" "how to deploy"

# List memories in a namespace
agent-memory list --ns "project:teeny-claw" --kind procedural

# Delete (soft-delete, recoverable)
agent-memory rm --ns "project:teeny-claw" --key "deploy-process"

# Import/export (for backup, migration)
agent-memory export --format json > memories.json
agent-memory import < memories.json
```

### Key Design Choices

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Storage** | SQLite via `modernc.org/sqlite` (pure Go) | No CGo = easy cross-compile, single binary. |
| **Text search** | SQLite FTS5 | Built-in, fast, good enough for most queries. |
| **Vector search** | Optional, phase 2 | Start simple. Add `sqlite-vec` or pure-Go cosine similarity later. |
| **Embeddings** | Optional, via external call | Don't bake in an embedding model. Let agent provide embeddings or call out to an API. |
| **Output format** | JSON to stdout | Agents parse JSON easily. Human-readable flags optional. |
| **Namespacing** | Required | Isolation between projects/agents/contexts. |
| **Schema** | Fixed but extensible | Core fields + JSON metadata column for future extensibility. |
| **DB location** | `$AGENT_MEMORY_DB` env var, default `~/.agent-memory/memory.db` | Standard, overridable. |

### What We're NOT Building

- ❌ A server/daemon (just a CLI)
- ❌ An embedding model (use external APIs)
- ❌ Automatic memory extraction (agent decides what to store)
- ❌ A framework or library (standalone binary)
- ❌ Multi-user auth (single-user, local-first)

## Decisions (Steered 2026-02-16)

### 1. Text in, text out — we handle embeddings internally
The CLI interface is pure text. Agent sends text, gets text back. No embedding vectors exposed in the API at all. We embed internally using a small local model (e.g., a GGUF embedding model or a pure-Go embedding approach). This keeps the tool dead simple to use from any agent.

**Implementation options for internal embeddings:**
- Bundle a small GGUF model and use a Go inference lib (e.g., `go-llama.cpp` bindings or pure-Go ONNX runtime)
- Use a lightweight Go embedding lib like `chromem-go` (has built-in embedding)
- Shell out to a local embedding server if available, fall back to FTS5-only if not
- Start with FTS5-only, add local embedding in a fast-follow

**Recommendation:** Start with FTS5 for v1 search. Add local embedding model in v1.1 — keep it internal, no API keys needed. The interface stays `agent-memory search "how to deploy"` either way.

### 2. Immutable memories with append-only history
Memories are immutable by default. An "update" creates a new version — old values are preserved. Retrieval returns the latest version by default, with `--history` flag to see all versions.

**Schema implication:** Add a `version` column and `supersedes` (pointing to previous version's ID). Queries default to `ORDER BY version DESC, updated_at DESC` — recency first.

This gives us:
- Full audit trail (how did this memory evolve?)
- No data loss from accidental overwrites
- Recency-biased retrieval by default

### 3. No TTL
No expiry. Memories persist until explicitly deleted (soft-delete with `deleted_at` for safety). Agents clean up after themselves. Simple.

### 4. CLI-only (MCP later)
Ship as CLI first. MCP server mode (`agent-memory serve --mcp`) is an interesting distribution channel for later but not v1 scope.

### Remaining Open Questions
- **Max content size** — Cap at 10KB? 100KB? Or no cap?
- **Local embedding model** — Which one? Need to research small, fast, Go-friendly options.

## Next Steps

1. ✅ Research (this doc)
2. 📋 **Plan** — Write design doc with schema, CLI spec, and build plan
3. 🎯 **Steer** — Review with EV
4. 🔨 **Build** — Implement in Go
