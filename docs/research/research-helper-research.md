# Research Helper — Design Research

Research into structured multi-step web research patterns for building `research-helper`, a tiny Go CLI for two-tier deep research.

## 1. The Two-Tier Pattern

Every major research tool converges on the same core loop:

1. **Plan** — decompose a research question into specific, searchable sub-queries
2. **Execute** — run queries, fetch/extract content, synthesize findings

The quality delta comes almost entirely from Tier 1. A naive single-query search returns shallow, often biased results. A well-planned 5-10 query strategy covers multiple angles, source types, and perspectives.

## 2. Existing Tools & Architectures

### OpenAI Deep Research
- **Model:** Specialized fine-tune of o3, trained with end-to-end RL on browsing+reasoning tasks
- **Architecture:** Plan-Act-Observe loop (ReAct pattern)
- **Five phases:**
  1. Clarify — ask follow-up questions to narrow scope
  2. Decompose — break query into sub-questions, plan search strategy
  3. Iterative search — search → read → refine queries → search again (progressive refinement)
  4. Analyze — read full documents, extract data, cross-reference
  5. Synthesize — produce structured report with inline citations
- **Key insight:** Backtracking. If a search path is unfruitful, it pivots strategy. This is hard to replicate in a stateless CLI.
- **Output:** 5,000-15,000 word reports with citations. Takes 5-30 minutes.

### Perplexity Deep Research
- **Architecture:** Test-time compute (TTC) expansion — spends more inference compute iterating
- **Process:** Iteratively searches, reads documents, reasons about what to do next, refines research plan as it learns
- **Key pattern:** DAG-based execution with recursive hierarchical iterations until "goal convergence"
- **Difference from OpenAI:** More opaque, but fundamentally similar plan-search-synthesize loop

### Google Gemini Deep Research
- **Model:** Gemini 2.0 Flash Thinking (now Gemini 3 Pro)
- **Architecture:** Multi-step RL-trained agent
- **Process:**
  1. Analyzes question → creates structured research plan with subtopics
  2. User reviews/modifies plan before execution (interactive step)
  3. Iteratively: formulate queries → read results → identify knowledge gaps → search again
  4. Synthesis with self-critique (multiple passes for clarity)
- **Key insight:** The plan review step. Letting the human adjust the plan before execution significantly improves results.
- **Technical challenges they solved:** When to stop searching, how to handle contradictory sources, maintaining coherence across many sources

### GPT Researcher (Open Source)
- **Architecture:** Planner + Execution agents (inspired by Plan-and-Solve paper)
- **Process:**
  1. Create task-specific agent from research query
  2. Planner generates research questions
  3. Crawler agents gather information for each question (parallelized)
  4. Summarize and source-track each resource
  5. Filter and aggregate into final report
- **Stack:** Python, uses Tavily for search, any LLM provider
- **Key insight:** Parallelized execution. Each sub-query runs independently, then results merge. This is the most CLI-friendly architecture.
- **Source:** github.com/assafelovic/gpt-researcher

### STORM (Stanford)
- **Architecture:** Multi-perspective question asking for pre-writing
- **Process:**
  1. Discover perspectives — identify different viewpoints/angles on the topic
  2. Simulated conversations — generate questions from each perspective against Internet sources
  3. Curate — organize collected information into an outline
  4. Write — generate full article from outline + gathered info
- **Key insight:** The "perspective" concept. Instead of just decomposing by subtopic, STORM decomposes by *viewpoint* (e.g., a DBA's perspective vs. a startup founder's perspective on SQLite vs Postgres).
- **Source:** github.com/stanford-oval/storm

### Tavily
- **Not a research agent** — it's a search/extract API purpose-built for LLM agents
- **Features:** Real-time search, content extraction, structured output, web crawling
- **Key value:** Returns pre-chunked, LLM-ready content (not raw HTML)
- **Relevance:** This is the *search backend* that GPT Researcher and others use. Our CLI could either wrap Tavily or wrap raw search APIs (Brave, Google) + our own extraction.

## 3. Query Decomposition Strategies

### Faceted Decomposition
Given: "Should I use SQLite or Postgres for agent memory?"

Decompose into facets:
1. **Performance:** "SQLite vs PostgreSQL performance benchmarks read-heavy workloads"
2. **Concurrency:** "SQLite concurrent writes limitations vs PostgreSQL"
3. **Operational:** "SQLite deployment complexity vs PostgreSQL ops overhead"
4. **Use case match:** "SQLite use cases embedded applications agents"
5. **Scale limits:** "SQLite database size limits when to migrate PostgreSQL"
6. **Community wisdom:** "site:reddit.com SQLite vs Postgres for AI agents"
7. **Real-world:** "site:news.ycombinator.com SQLite production experience"
8. **Architecture:** "agent memory storage patterns SQLite PostgreSQL"

### Source-Specific Queries
- `site:reddit.com` — practitioner opinions, war stories
- `site:news.ycombinator.com` — technical community discussion
- `arxiv.org` or `scholar.google.com` — academic/research papers
- Official docs — authoritative reference
- Blog posts — practical experience, tutorials

### Coverage Detection
How to know when you've searched enough:
- **Saturation:** New queries return sources you've already seen
- **Facet coverage:** Every planned facet has ≥2 sources
- **Contradiction resolution:** Conflicting claims have been investigated
- **Source diversity:** Mix of official docs, community discussion, and practical experience
- **Budget:** Fixed query count (e.g., 8-12 queries) as a practical limit

### Query Fan-Out (Google's Approach)
Google's deep research generates queries *dynamically* — initial broad queries spawn more specific follow-ups based on gaps found. This is iterative, not pre-planned. For a CLI tool, a hybrid approach works: pre-plan most queries, but allow 2-3 "follow-up" slots based on initial results.

## 4. Synthesis Patterns

### Merge Without Repetition
- Extract claims/facts from each source independently
- Deduplicate by semantic similarity
- Group by theme/facet
- Attribute each claim to its source(s)

### Claim Verification
- Flag claims that appear in only one source
- Highlight contradictions between sources
- Weight by source quality (official docs > blog > forum)

### Source Quality Ranking
1. Official documentation / specs
2. Peer-reviewed papers
3. Reputable tech publications
4. First-party experience posts (blog with benchmarks)
5. Community discussion (Reddit, HN)
6. Generic content farms (low weight)

### Output Formats
- **Research doc:** Narrative with sections, citations, and confidence levels
- **Comparison table:** Side-by-side feature matrix (great for "X vs Y" questions)
- **Recommendation:** Opinionated conclusion with supporting evidence
- **Source list:** Annotated bibliography with relevance scores

## 5. CLI Design Recommendations

### The Core Question: Where Does the LLM Live?

**Option A: CLI includes LLM calls (query planning + synthesis)**
- CLI takes a natural language question, calls LLM to generate query plan, executes searches, calls LLM to synthesize
- Pro: Self-contained, end-to-end
- Con: Needs API keys, expensive, duplicates what the calling agent already does

**Option B: CLI is a search/fetch orchestrator only**
- Takes a structured query plan (JSON/YAML) as input, executes searches, returns raw results
- The calling agent does planning and synthesis
- Pro: Unix-y, composable, cheap, no LLM dependency
- Con: Requires caller to be smart about planning

**Option C: Hybrid — orchestrator with optional LLM planning**
- Default: takes a query plan and executes it
- Flag: `--plan` takes a natural language question and generates the plan (requires LLM)
- Pro: Best of both worlds
- Con: More complex

### Recommendation: Option B (orchestrator) with a separate `research-plan` command

Two commands, composable:
```
# Agent or human creates a plan
echo "Should I use SQLite or Postgres for agent memory?" | research-plan > plan.json

# Execute the plan
research-exec < plan.json > results.json

# Or pipe them
echo "question" | research-plan | research-exec > results.json
```

But honestly, for teeny-claw's Unix philosophy, **Option B alone** is the sweet spot. The agent calling this tool (Claude, GPT, etc.) is already an LLM — it can generate the query plan itself. The CLI just needs to be a reliable, fast, parallel search-and-fetch executor.

### Proposed Interface

```
# Input: JSON lines, each a search task
{"query": "SQLite vs PostgreSQL benchmarks", "sources": ["web"], "max_results": 5}
{"query": "site:reddit.com SQLite agent memory", "sources": ["web"], "max_results": 3}
{"url": "https://specific-doc.com/page", "mode": "extract"}

# Output: JSON lines, each with results
{"query": "...", "results": [{"url": "...", "title": "...", "snippet": "...", "content": "..."}]}
```

### Search Backend Options
1. **Brave Search API** — good free tier, what OpenClaw already uses via `web_search`
2. **Tavily API** — purpose-built for agents, returns cleaner content, costs money
3. **SearXNG** — self-hosted, free, aggregates multiple engines
4. **Shell out to existing tools** — just call `web_search` and `web_fetch` via OpenClaw

For teeny-claw: **wrap Brave Search API directly** (Go HTTP client). It's what we already use, it's cheap, and it gives us control. Add content extraction via a simple HTML→text pipeline (go-readability or similar).

### Key Features
- **Parallel execution** — run all queries concurrently (Go is great at this)
- **Rate limiting** — respect API limits
- **Content extraction** — fetch URLs and extract readable text
- **Source dedup** — don't fetch the same URL twice across queries
- **Structured output** — JSON lines for easy piping
- **Timeout/budget** — max queries, max fetch time, max total content

### What NOT to Build
- No LLM integration (let the caller handle that)
- No fancy UI (it's a CLI)
- No state/memory between runs (stateless)
- No report generation (that's synthesis, the caller's job)

## 6. Architecture Summary

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────┐
│  Agent/LLM  │────▶│  research-helper │────▶│  Agent/LLM  │
│  (planning) │     │  (orchestrator)  │     │ (synthesis)  │
└─────────────┘     └──────────────────┘     └─────────────┘
                           │
                    ┌──────┴──────┐
                    │             │
              ┌─────┴────┐ ┌─────┴────┐
              │  Search   │ │  Fetch   │
              │  (Brave)  │ │ (Extract)│
              └──────────┘ └──────────┘
```

The CLI is the middle box. It takes a query plan, fans out searches and fetches in parallel, deduplicates, and returns structured results. The intelligence (planning and synthesis) stays with the calling agent.

## 7. Prior Art Summary Table

| Tool | Planning | Execution | Synthesis | Open Source |
|------|----------|-----------|-----------|-------------|
| OpenAI Deep Research | LLM (o3) | Built-in browser | LLM (o3) | No |
| Perplexity Deep Research | LLM (proprietary) | Built-in search | LLM | No |
| Gemini Deep Research | LLM (Gemini) + human review | Built-in search | LLM + self-critique | No |
| GPT Researcher | LLM planner agent | Parallel crawler agents | LLM publisher agent | Yes (Python) |
| STORM | Multi-perspective LLM | Web retrieval | LLM writer | Yes (Python) |
| Tavily | N/A (API only) | Search + extract API | N/A | No (API) |
| **research-helper (proposed)** | **Caller's job** | **Parallel search+fetch** | **Caller's job** | **Yes (Go)** |

## 8. Open Questions

1. **Should we support iterative/follow-up queries?** (Input mid-stream additions based on initial results) — Probably not for v1. Keep it batch.
2. **Content extraction quality** — Go readability libraries aren't as good as Mozilla's. May need to shell out to a Node.js extractor or just return snippets.
3. **Rate limiting strategy** — Per-API-key limits vary. Make configurable.
4. **Max content size** — How much extracted text per URL? Need to balance thoroughness vs. token budget of the calling agent.
