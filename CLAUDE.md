# CLAUDE.md — coding agent instructions for memoryweb

Read this before touching any file in this repo.

---

## What this repo is

memoryweb is a **MCP (Model Context Protocol) server** written in Go. It stores
knowledge as a graph of nodes (concepts/decisions) connected by typed, narrative
edges. It communicates over stdin/stdout using JSON-RPC 2.0.

The binary is long-lived: one process per MCP session. All state lives in a
single SQLite file (WAL mode). There is no HTTP server, no daemon — just a loop
reading lines from stdin and writing responses to stdout.

---

## Package layout

`db/` and `tools/` are split by concern, one file per area — not one monolithic
file per package. When adding a new Store method or tool handler, put it in the
file for its concern; create a new file only for a genuinely new concern.

```
main.go               JSON-RPC 2.0 wire loop (stdin → dispatch → stdout)

db/store.go           Store type, New/Close (checkpoints WAL on close), Backup (VACUUM INTO snapshot), schema/version/stats queries, init()
db/migrations.go      migration type, the migrations slice, migrate() — append-only, see below
db/embeddings.go      Ollama embedding client, storeEmbedding, BackfillEmbeddings
db/nodes.go           Node struct, AddNode/GetNode/UpdateNode/ArchiveNode/RestoreNode/ListArchived, batch variants
db/edges.go           Edge struct, AddEdge/AddEdgesBatch/DeleteEdge, collectEdges
db/search.go          SearchNodes/SearchNodesExact, LIKE + semantic search paths
db/graph.go           FindPath/FindConnections/GetNodeNeighbourhood/GetDomainGraph, SuggestEdges
db/timeline.go        RecentChanges(Scoped), Timeline, GetHistoryForMemoryID
db/significance.go    GetSignificance and its memory_id/tags variants
db/audit.go           FindDrift/CountStaleDrift/FindDisconnected
db/domains.go         Alias CRUD, RenameDomain, MergeDomains, ListDomains
db/purge.go           Purge — the one hard-delete path; CLI-only, never an MCP tool, kept visually separate
db/util.go            slug(), shortID(), tagFilter, scanNodeRow(s), and the generic helpers below

tools/tools.go        Handler type, CallTool dispatch switch, Instructions const, shared small helpers
tools/definitions.go  ListTools() — the full MCP tool schema
tools/lean.go         Shared lean-entry helpers (leanEntry, toLeanEntry, truncateWhy, ...)
tools/remember.go     remember tool (addNode + batch)
tools/revise.go       revise tool (updateNode + batch)
tools/connect.go      connect/disconnect/suggest_connections
tools/search.go       search tool
tools/recent.go       recent tool
tools/history.go      history tool
tools/significance.go significance tool
tools/orient.go       orient tool (cross-domain snapshot, topic mode, full domain summary)
tools/domains.go      domains/alias tools, rename_domain
tools/archive.go      forget/restore/forget_all, audit tool, drift
tools/graph.go        why_connected, trace, visualise

cmd/                  CLI-only subcommands (not MCP tools)
stats/                WKD session scoring and stats logging
```

---

## CLI subcommand rule — mandatory

**Decision:** All new CLI commands must be implemented as subcommands of the
main `memoryweb` binary (added to the `os.Args` switch in `main.go`). Never
create a new standalone binary under `cmd/` unless explicitly told to.

The existing `cmd/purge/` is a legacy exception. `cmd/dream/` and `cmd/embeddings/`
contain only `doc.go` placeholders — their logic lives in `main.go`.

New `cmd/` directories must not be created without explicit instruction.

---

## The migration system — critical rules

Migrations live in the `migrations` slice in `db/migrations.go`. They are **append-only**
and version-numbered. The runner stamps applied versions in a `schema_migrations`
table.

**Never edit an existing migration entry.** Only add new entries at the end with
the next version number. The runner skips already-applied versions.

A bootstrap guard stamps all existing migrations as applied if the `nodes` table
already exists but `schema_migrations` doesn't — this handles upgrading pre-
versioning DBs without re-running migrations.

Current migrations:
| v | Description |
|---|-------------|
| 1 | Initial schema: nodes, edges, indexes |
| 2 | nodes: add occurred_at column and index |
| 3 | Add domain_aliases table |
| 4 | nodes: add archived_at column and index (`idx_nodes_archived`) |
| 5 | Add audit_log table |
| 6 | nodes: add tags column and index (`idx_nodes_tags`) |
| 7 | nodes: add transient column (`INTEGER NOT NULL DEFAULT 0`) |
| 8 | Add node_embeddings virtual table (sqlite-vec) |
| 9 | Resize node_embeddings to 1024 dimensions (snowflake-arctic-embed default) |
| 10 | audit_log: add provenance TEXT column |
| 11 | Add significance_log table |
| 12 | nodes: add decision_type TEXT column; migrate transient=1 → decision_type='transient'; drop transient column |
| 13 | nodes: rename decision_type column to node_kind |

---

## Soft delete — the archived_at contract

**Decision (session 1):** nodes are never hard-deleted via the MCP tools. They
are soft-deleted by setting `archived_at DATETIME`.

Rules that must be upheld everywhere:

1. **All retrieval queries** must include `archived_at IS NULL` in their WHERE
   clause. This applies to: `SearchNodes`, `GetNode`, `RecentChanges`,
   `Timeline`, and `bestMatch` (which powers `FindConnections`).

2. **`archived_at`** must be included in every SELECT that reads the `nodes`
   table, and scanned into a `sql.NullTime`, then mapped to `Node.ArchivedAt
   *time.Time`.

3. **`ArchiveNode(id, reason string) error`** — sets `archived_at = now()`.
   Looks up the node label first (so it errors on a missing ID), then writes an
   `audit_log` row with `action='archive'` and the provided reason.

4. **`RestoreNode(id string) error`** — clears `archived_at` (sets to NULL).
   Writes an `audit_log` row with `action='restore'`, reason nil.

5. **`ListArchived(domain string) ([]Node, error)`** — returns nodes where
   `archived_at IS NOT NULL`, optionally scoped to a domain, ordered by
   `archived_at DESC`.

6. **`audit_log`** records every archive / restore / purge action. Schema:
   `id, action, node_id, node_label, reason (nullable), actioned_at, provenance (nullable)`.
   When `occurred_at` is agent-assigned, `provenance='agent-assigned'` is written.

7. **Purge** (hard delete of archived nodes) is **CLI-only** at `cmd/purge/`.
   It must never be exposed as an MCP tool. See the CLI section of README.md.

---

## Node struct

```go
type Node struct {
    ID          string     `json:"id"`
    Label       string     `json:"label"`
    Description string     `json:"description"`
    WhyMatters  string     `json:"why_matters"`
    Tags        string     `json:"tags,omitempty"`
    Domain      string     `json:"domain"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
    OccurredAt  *time.Time `json:"occurred_at,omitempty"`
    ArchivedAt  *time.Time `json:"archived_at,omitempty"`  // nil = live
    Transient   bool       `json:"transient,omitempty"`    // true = short-lived; audit(mode=stale) flags after 7 days
}
```

Node IDs are `slug(label) + "-" + shortID()` where `shortID()` is 4 random
bytes as lowercase hex (8 chars).

---

## Tool description conventions

**Decision:** tool descriptions carry agent guidance. Follow these rules when
writing or updating descriptions.

**When removing or renaming a tool:** add the old tool name to the `removedTools`
slice in `TestListTools_NoStaleToolReferences` in `tools/tools_test.go`. This
prevents stale references to the removed name surviving in other tools'
descriptions. No exceptions — the test will not catch the omission automatically.
This rule is also enforced by `TestListTools_PropertyDescriptionsNoForbiddenWords`,
which scans all property-level descriptions too.

- Never expose structural vocabulary to the user: no "node", "edge", "the web",
  "stored in", "retrieved from", "what's recorded".
- Present retrieved information as direct knowledge, no preamble.
- The presentation instruction ("Never acknowledge that you are retrieving from
  a tool or memory system") must appear on **all** retrieval tools: `search`,
  `recall`, `orient`, `history`, `why_connected`, `significance`, `recent`.
- `remember` must include a strong imperative to follow up on `suggested_connections`
  with `connect` before ending the session, placed at the top of the description.
- `connect` description must include relationship semantics guidance so agents
  choose `caused_by`/`led_to` vs `blocked_by`/`depends_on` correctly rather than
  defaulting to `connects_to`.
- `visualise` must instruct agents to output the mermaid string inside a mermaid
  code block unconditionally — no client-conditional HTML widget branching.

**Decision:** tool changes and `docs/memoryweb-skill.md` are reviewed against
each other, in both directions, every time either one changes.

- Changing a tool (new tool, added/removed/renamed parameter, changed enum
  values, changed response shape, changed description text) — before
  merging, re-read `docs/memoryweb-skill.md`'s Layer 1 contract and Layer 2
  reference (node_kind taxonomy, relationship types, tool quick reference,
  domain move protocol) for anything that now names the old shape or old
  wording, and update the skill in the same change.
- Changing `docs/memoryweb-skill.md` — if the edit documents a workaround for
  a tool quirk ("ignore what the tool tells you", "the tool does X, work
  around it by doing Y"), that's a signal the tool itself has a bug or a
  description defect. Flag it as a candidate fix rather than letting the
  workaround become permanent skill content.

A stale skill actively misleads agents into calling tools the way they used
to work; an unfixed tool bug papered over by a skill workaround hides a real
defect behind agent-side prose that has to be re-taught in every host
variant. See the `before-shipping-any-tool-schema...` standing rule in the
`memoryweb-meta` domain — this session's `docs/memoryweb-skill.md` v2 draft
surfaced exactly this coupling: it documented a workaround for `remember`'s
`orphan_warning` telling agents to pass `connect` a `domain` parameter that
never existed, and reviewing *why* the workaround was needed led straight to
the actual fix (v1.38.2) instead of the workaround becoming permanent.

---

## Archive / forget protocol

**Decision:** `forget` must enforce a strict archiving protocol in its description:

1. Only suggest archiving after `audit(mode=stale)` surfaces a candidate or the
   user explicitly identifies something as stale.
2. Always present the node and ask: *"Should I archive this?"* Never assume yes.
3. Wait for unambiguous confirmation before calling the tool.
   *"That's probably outdated"* is not confirmation.
4. Never archive based on casual mention or implication.
5. After archiving, report the node ID and note it can be restored with `restore`.

---

## Go generics — standing rule

**Decision:** Use Go generics whenever they remove real duplication across multiple
call sites. The bar is: would you write the same 3+ line pattern twice? Use a
generic. Would it be the first and only use? Don't.

Canonical candidates in this codebase:
- Row iteration → `scanRows[T]` in `db/util.go`
- SQL IN clauses → `inClause[T]` in `db/util.go`
- Slice transform / filter → `mapSlice[T,U]`, `filter[T]` in `db/util.go`
- Optional field SQL update → `applyStringField` in `db/util.go`
- Single-vs-batch JSON dispatch → `dispatchBatch[T]` in `tools/util.go`

**Self-enforcement:** every story that ships a generic helper must be linked in
the examples list below. Future agents reading this file must consult the list
before deciding "we don't do that here."

### Shipped examples

| Story | Generic introduced | Applied in |
|-------|--------------------|-----------|
| `stories/generics-wave1.md` | `nullTimeToPtr`, `scanRows`, `inClause`, `filter`, `mapSlice` | `db/util.go` |
| `stories/generics-optional-field-update.md` | `applyStringField` | `db/util.go` — used by `UpdateNode`, `UpdateNodesBatch` in `db/nodes.go` |
| `stories/generics-json-batch-dispatch.md` | `dispatchBatch` | `tools/util.go` — used by `addNode` (`tools/remember.go`), `addEdge` (`tools/connect.go`), `updateNode` (`tools/revise.go`) |

---

## Testing conventions

**Decision:** all tests live in the same directory as the code under test — Go
convention, no exceptions. There is no top-level `tests/` directory.

| File | Package | What it tests |
|------|---------|---------------|
| `db/*_test.go` | `db_test` | DB-layer unit tests: all Store methods, one test file per production file (e.g. `nodes_test.go` tests `nodes.go`). `db_test.go` itself holds only the shared helpers (`newStore`, `mustAddNode`, ...) |
| `tools/*_test.go` | `tools_test` | Outside-in agent-style tests via `CallTool`, one test file per production file (e.g. `remember_test.go` tests `remember.go`). `tools_test.go` itself holds only the shared helpers (`call`, `newEnv`, `mustNotError`, ...) plus the handful of tests that exercise `tools.go` directly (`CallTool` dispatch, `getNode`, `checkForUpdates`) |
| `cmd/purge/main_test.go` | `main_test` | CLI integration tests via `exec.Command` |
| `main_test.go` | `main_test` | Wire-layer tests for setup and subcommand dispatch |

New production files always get a matching `_test.go` file of the same name — don't add a new concern's tests to an unrelated existing file.

All tests use isolated temp-file SQLite DBs via `t.TempDir()`. Never share
state between tests. Never use `:memory:` (WAL mode and schema stamping behave
differently).

The tools tests call `h.CallTool(params)` with raw JSON exactly as an MCP agent
would. **No direct Store access in tool tests** — all operations must go through
the tool interface.

Helper pattern in tools tests:
```go
func call(t, h, toolName, arguments) *ToolResult   // invokes CallTool
func mustNotError(t, tr)                            // fails if IsError
func mustError(t, tr)                               // fails if not IsError
func addNode(t, h, label, domain, extras) string    // returns ID
func searchIDs(t, tr) []string                      // parses IDs from result
func newEnvWithPath(t) (string, *Store, *Handler)   // use when raw DB access needed
```

Run tests: `go test ./...`

**Windows note:** `go test ./...` requires CGO header setup before running.
See the `windows-cgo-build-copy-sqlite3` memory in memoryweb-meta for the exact
PowerShell steps.

**Rule:** Always run `go test ./...` and confirm all tests pass before deploying
the binary or committing any change. No exceptions.

**Preferred deployment: Homebrew.** The canonical binary path is
`/opt/homebrew/bin/memoryweb`. Use `brew upgrade memoryweb` to deploy a release.
The manual `mv` pattern is superseded — use Homebrew.

---

## What's implemented (v1.39.0)

All 21 MCP tools are live. See the tools table in AGENTS.md for the full list.

Key implemented features:
- Core graph: nodes, edges, search (LIKE + semantic), timeline, connections, aliases
- Soft delete: archived_at, audit_log with provenance column, ArchiveNode, RestoreNode
- Semantic search via sqlite-vec and Ollama (snowflake-arctic-embed)
- Batch operations: remember/revise/connect all accept `items` arrays
- orient: lean field format (id + label + why_matters ≤150 chars, sentence-boundary truncated, truncated flag); significant=10/recent=5/spine=20; live_nodes + archived_nodes; optional topic parameter returns relevant section instead of significant
- significance: dual-signal importance (declared + structural recency-weighted); memory_id + tags filter modes
- history: memory_id mode (neighbourhood-scoped); tags filter
- visualise: domain graph and single-node neighbourhood as Mermaid with truncation metadata
- audit: stale/orphans/archived modes replacing whats_stale/disconnected/forgotten
- forget_all: batch archive in a single atomic call
- rename_domain + merge-domains CLI
- node_kind filter on search/recent/history/significance/audit
- revise(domain): non-destructive node-level domain reassignment with mandatory reason and audit log; batch supported
- Stats: WKD session scoring logged to MEMORYWEB_STATS_FILE
- Hooks: Stop (save) and PreCompact with orphan nudge and dream digest
- Schema staleness defence: legacy key rejection, server_version in orient, tools/list_changed notification
- Instructions: credentials advisory (never file credentials/API keys/tokens in memories)
- purge: domain filter is case/whitespace-insensitive; `--include-live` hard-deletes live nodes in a domain (requires `--domain`); dry-run/confirm both report `LiveRemaining` so an operator can't mistake "0 archived candidates" for "domain is empty"
- connect: `relationship` enum includes `resolved`, `resolved_by`, `supersedes` for contradiction resolution (previously missing from the schema, which silently blocked the mechanism for any client enforcing enum constraints); `audit`'s stale/conflicts suppression recognises all three; `audit`'s description no longer instructs disconnecting the `contradicts` edge
- remember: `orphan_warning` no longer instructs agents to pass `connect` a `domain` parameter — `connect` has never accepted one (IDs are global); the instruction dated back to a false premise in the original cross-domain-connect-ux fix and had stood since 2026-05-23

---

## Wire protocol

JSON-RPC 2.0, one message per line (newline-delimited). Methods:

| Method | Handler |
|--------|---------|
| `initialize` | Returns server info + capabilities |
| `tools/list` | Returns all tool definitions |
| `tools/call` | Dispatches to named tool handler |
| `notifications/initialized` | Emits `notifications/tools/list_changed` then continues |

Errors at the tool level are returned as `ToolResult{IsError: true}`, not as
JSON-RPC errors. JSON-RPC errors (`-32xxx`) are only for protocol-level failures.
