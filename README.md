# memoryweb

<div style="text-align:center"><img src="web.png" alt="memoryweb" /></div>

A memory MCP server for agents. Stores knowledge as a graph of concepts connected by typed, narrative edges — designed around how agents navigate context: by following the thread of why things connect, not by knowing where they are stored.

## The idea

You don't remember facts in isolation. You remember them because of what they connect to. *The boot crash is significant because it blocks the tutorial, which blocks the demo, which is why the fix matters now.* Pull on any of those threads and you get the rest.

memoryweb works the same way. Each concept is a node. What makes it retrievable is the narrative edge — the *because* that links it to something else. A concept with rich connections is reachable from many starting points. A concept filed alone, with no story linking it to anything, is effectively lost.

## Tools

### Core graph

| Tool | What it does |
|------|-------------|
| `add_node` | File a concept, decision, or finding. Requires a label and domain. Optionally a description, why_matters, and occurred_at. |
| `add_edge` | Connect two nodes with a typed relationship and a narrative "because". |
| `get_node` | Retrieve a node and all its connections. |
| `search_nodes` | Text search across label, description, and why_matters. Optionally scope to a domain. |
| `find_connections` | Look up the reasoning linking two named concepts. Use this when asked why or how two things relate. |
| `recent_changes` | What was filed recently. Good for session orientation. |
| `timeline` | Nodes ordered by when they actually occurred (not filed). Supports date range filtering. |

### Archive / forget

Nodes are never hard-deleted via the tools. Archive = soft delete; the node disappears from search and retrieval but can be restored.

| Tool | What it does |
|------|-------------|
| `forget_node` | Archive a node with a reason. Strict protocol: only after drift surfaces a candidate or the user explicitly confirms. |
| `restore_node` | Un-archive a node so it surfaces again. |
| `list_archived` | Review what's been forgotten. Optionally scope by domain. |

### Domain aliases

| Tool | What it does |
|------|-------------|
| `add_alias` | Register an alternative name for a domain so both names return the same results. |
| `list_aliases` | List all registered aliases and what they map to. |
| `resolve_domain` | Check what canonical domain a name resolves to. |

### Relationship types
`caused_by` `led_to` `blocked_by` `unblocks` `connects_to` `contradicts` `depends_on` `is_example_of`

## CLI

The `purge` subcommand hard-deletes archived nodes from the database. It is intentionally not exposed as an MCP tool — it's a maintenance operation, not an agent operation.

```bash
memoryweb purge --dry-run              # show what would be deleted (default behaviour without --confirm)
memoryweb purge --confirm              # actually deletes
memoryweb purge --domain sedex         # scope to a domain
memoryweb purge --before 2026-01-01    # only nodes archived before a date
```

## Build

```bash
go build -o memoryweb .
```

Requires Go 1.22+. Uses `github.com/mattn/go-sqlite3` — CGO must be available.

## Storage

Default DB path: `~/.memoryweb.db`

Override with `MEMORYWEB_DB=/path/to/your.db`

## MCP config

Add to your MCP host's config (example for Claude Desktop on macOS — `~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/path/to/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/Users/yourname/.memoryweb.db"
      }
    }
  }
}
```

## Conventions

- Use `domain` to separate concerns: `deep-game`, `sedex`, `general`
- The `why_matters` field is the most important one for retrieval — don't skip it
- The `narrative` on an edge is the *because* — the reasoning that makes the connection meaningful, not just the fact that a connection exists
- Add edges immediately after adding related nodes
- Call `recent_changes` at the start of a session to orient without needing to know what to search for
- Use `find_connections` when asking about the relationship between two specific things
