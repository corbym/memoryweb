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

```
main.go          JSON-RPC 2.0 wire loop (stdin → dispatch → stdout)
db/db.go         Store type, all SQL, Node/Edge structs, migrations
db/util.go       slug(), shortID() helpers
tools/tools.go   MCP tool definitions + handler methods
cmd/             CLI-only subcommands (not MCP tools)
```

---

## The migration system — critical rules

Migrations live in the `migrations` slice in `db/db.go`. They are **append-only**
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
   `id, action, node_id, node_label, reason (nullable), actioned_at`.

7. **Purge** (hard delete of archived nodes) is **CLI-only** at `cmd/purge/`.
   It must never be exposed as an MCP tool. See prompts.md Prompt 3.

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
    Transient   bool       `json:"transient,omitempty"`    // true = short-lived; drift flags after 7 days
}
```

Node IDs are `slug(label) + "-" + shortID()` where `shortID()` is 4 random
bytes as lowercase hex (8 chars).

---

## Tool description conventions

**Decision (session 2):** tool descriptions carry agent guidance. Follow these
rules when writing or updating descriptions:

- Never expose structural vocabulary to the user: no "node", "edge", "the web",
  "stored in", "retrieved from", "what's recorded".
- Present retrieved information as direct knowledge, no preamble.
- Every retrieval tool (`search_nodes`, `get_node`, `recent_changes`, `timeline`,
  `find_connections`) must include this sentence:
  > "This tool only returns live nodes. Archived nodes are hidden. If the user
  > asks about something that seems missing, consider suggesting drift or
  > list_archived to check whether it was archived."
- `add_node` must include:
  > "Before adding a node, consider whether a similar node already exists. If
  > so, suggest linking to it with add_edge rather than creating a duplicate.
  > Duplicate nodes with no edges are the most common cause of drift candidates."

---

## Archive / forget protocol

**Decision (session 2):** `forget_node` (not yet wired as a tool) must enforce
a strict archiving protocol in its description:

1. Only suggest archiving after drift surfaces a candidate or the user
   explicitly identifies something as stale.
2. Always present the node and ask: *"Should I archive this?"* Never assume yes.
3. Wait for unambiguous confirmation before calling the tool.
   *"That's probably outdated"* is not confirmation.
4. Never archive based on casual mention or implication.
5. After archiving, report the node ID and note it can be restored.

---

## Drift detection (planned — Prompt 4)

`FindDrift(domain, limit)` to be added to `db/db.go`. Rules (in priority order):

1. `contradicts` edge between two nodes.
2. Label contains "old", "deprecated", "replaced", "legacy", "previous".
3. Label/description contains "open question", "unresolved", "TBD", "TODO"
   and node is older than 30 days.
4. Duplicate label (same domain, after lowercasing and stripping punctuation).

Drift tool must **not** archive anything automatically. It presents candidates
and asks the user about each one individually.

---

## Testing conventions

**Decision (session 2):** all tests live in the same directory as the code under
test — Go convention, no exceptions. There is no top-level `tests/` directory.

| File | Package | What it tests |
|------|---------|---------------|
| `db/db_test.go` | `db_test` | DB-layer unit tests: all Store methods |
| `tools/tools_test.go` | `tools_test` | Outside-in agent-style tests via `CallTool` |
| `cmd/purge/main_test.go` | `main_test` | CLI integration tests via `exec.Command` |

All tests use isolated temp-file SQLite DBs via `t.TempDir()`. Never share
state between tests. Never use `:memory:` (WAL mode and schema stamping behave
differently).

The tools tests call `h.CallTool(params)` with raw JSON exactly as an MCP agent
would. **No direct Store access in tool tests** — all operations must go through
the tool interface. The one exception is `TestAuditLog_RecordsForgetAndRestore`,
which opens a second raw SQL connection to inspect `audit_log` directly; this is
justified because the tool response does not expose audit_log contents.

Helper pattern in tools tests:
```go
func call(t, h, toolName, arguments) *ToolResult   // invokes CallTool
func mustNotError(t, tr)                            // fails if IsError
func mustError(t, tr)                               // fails if not IsError
func addNode(t, h, label, domain, extras) string    // returns ID
func searchIDs(t, tr) []string                      // parses IDs from result
func newEnvWithPath(t) (string, *Store, *Handler)   // use when raw DB access needed
```

**Wire-layer gap (accepted):** `main.go` — the stdin/stdout scanner, JSON-RPC
envelope, and `dispatch()` routing — is not covered by automated tests. This is
an accepted tradeoff: the wire layer is ~50 lines of standard plumbing with no
branching logic beyond a 5-case switch. It is covered by manual MCP integration
testing (see tests.md).

Run tests: `go test ./...`

---

## What's implemented

- [x] Core graph: nodes, edges, search, timeline, connections, aliases
- [x] Soft delete: archived_at, audit_log, ArchiveNode, RestoreNode, ListArchived
- [x] Tool description agent guidance (archive advisory + duplicate warning)
- [x] Outside-in test suite (db + tools packages)
- [x] `update_node` tool: merge label/description/why_matters/tags without archiving; writes audit_log entry on every call with action='update' and a reason listing changed fields and their old values
- [x] `tags` field on nodes (migration v6): searched by all retrieval tools; populated via add_node, add_nodes, update_node
- [x] `transient` field on nodes (migration v7): boolean, default false; accepted by add_node and add_nodes; drift surfaces transient nodes older than 7 days with a note to archive once related work is complete
- [x] `related_to` on `add_node`: accepts plain string IDs (defaults to `connects_to`) or objects `{"id": "...", "relationship": "..."}` for explicit relationship type; invalid IDs silently skipped
- [x] `audit_log` records: archive, restore, purge (CLI), update — all mutating node operations

## What's planned (see prompts.md)

- [ ] Prompt 2: `forget_node`, `restore_node`, `list_archived` MCP tools
- [ ] Prompt 3: `cmd/purge/main.go` CLI (hard delete of archived nodes)
- [ ] Prompt 4: `FindDrift` + `drift` tool
- [ ] Prompt 5: `summarise_domain` tool (calls Anthropic API)

---

## Wire protocol

JSON-RPC 2.0, one message per line (newline-delimited). Methods:

| Method | Handler |
|--------|---------|
| `initialize` | Returns server info + capabilities |
| `tools/list` | Returns all tool definitions |
| `tools/call` | Dispatches to named tool handler |
| `notifications/initialized` | Ignored (no-ID notification) |

Errors at the tool level are returned as `ToolResult{IsError: true}`, not as
JSON-RPC errors. JSON-RPC errors (`-32xxx`) are only for protocol-level failures.

