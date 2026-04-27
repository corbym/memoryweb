# memoryweb
[![memoryweb MCP server](https://glama.ai/mcp/servers/corbym/memoryweb/badges/score.svg)](https://glama.ai/mcp/servers/corbym/memoryweb)

<div style="text-align:center"><img src="web.png" alt="memoryweb" /></div>

A memory MCP server for agents.

## The idea

Human memory doesn't work by location. You don't retrieve a fact from a filing cabinet. You pull a thread. A smell connects to a kitchen, connects to a person, connects to a feeling from thirty years ago. The thread is always there. Pull any part of it and the rest follows.

This is why the memory palace works. Place feels like storage — *this fact lives in this room* — but that's not what's actually happening. Place creates a sequence. A sequence creates a story. The story is what holds the memory. Location is just a human shortcut for narrative.

Agents don't navigate by location at all. There is no place. Context is tokens in relation to other tokens. What makes something retrievable is its associative chain — the path of connections that lead to it from something else. The narrative edge, the *because*, is the mechanism. Not the index, not the address.

You don't remember facts in isolation. You remember them because of what they connect to. *The boot crash is significant because it blocks the tutorial, which blocks the demo, which is why the fix matters now.* Pull on any of those threads and you get the rest.

memoryweb works the same way. Each concept is a node. What makes it retrievable is the narrative edge — the *because* that links it to something else. A concept with rich connections is reachable from many starting points. A concept filed alone, with no story linking it to anything, is effectively lost.

## Philosophy

memoryweb optimises for remembering things well, not remembering things fast. Filing requires a moment of judgement: why does this matter, how does it connect to what else is known, what would be useful to know when coming back to this cold?

The metric is whether the next session starts with genuine understanding rather than a pile of raw facts.

This makes memoryweb a decision log, not an event log. The difference matters:

- An event log records what happened. A decision log records what was learned, decided, and why.
- An event log grows automatically. A decision log requires intent.
- An event log is useful for reconstructing the past. A decision log is useful for continuing work.

For long-running technical projects — where the hard problems are architectural decisions, subtle bugs, and design tradeoffs — the decision log is what lets you pick up where you left off without re-learning everything.

The `why_matters` field is not optional. A node without it is an event, not a decision. Nodes without `why_matters` will surface as drift candidates.

## Tools

### Core graph

| Tool | What it does |
|------|-------------|
| `add_node` | File a concept, decision, or finding. Required: `label`, `domain`. Optional: `description`, `why_matters`, `occurred_at` (ISO8601 date/datetime when it actually happened), `tags` (space-separated search keywords), `related_to` (auto-connect to existing nodes at creation), `transient` (mark as short-lived knowledge). |
| `add_edge` | Connect two nodes with a typed relationship and a narrative "because". Both nodes must exist first. |
| `add_nodes` | Batch version of `add_node` — insert multiple nodes in one transaction. Supports all the same fields per node. |
| `add_edges` | Batch version of `add_edge` — insert multiple edges in one transaction. |
| `update_node` | Update `label`, `description`, `why_matters`, or `tags` on an existing node without archiving it. Only supplied fields are changed; omitted fields keep their current values. Writes an audit log entry on every call recording changed fields and their previous values. |
| `get_node` | Retrieve a node and all its connections by ID. |
| `search_nodes` | Text search across `label`, `description`, `why_matters`, and `tags`. Optionally scope to a domain. Falls back to individual-word OR matching when no field contains the full phrase. |
| `find_connections` | Look up the reasoning linking two named concepts. Use this when asked why or how two things relate. |
| `recent_changes` | What was filed recently. Good for session orientation. Set `group_by_domain=true` (with no domain) to see recent activity broken down per domain. |
| `timeline` | Nodes ordered by when they actually occurred (not when filed). Supports date range filtering with `from` and `to`. |
| `summarise_domain` | Return all nodes for a domain structured for synthesis — covering current state, blockers, recent decisions, and open questions. Includes node IDs so agents can pass them directly to `update_node` or `add_edge` without a second lookup. |
| `suggest_edges` | Given a node ID, return up to 5 candidate connections from the same domain whose labels, descriptions, or tags overlap. Use this after filing a new node to discover likely connections before calling `add_edge`. Read-only — never creates edges. |

### Archive / forget

Nodes are never hard-deleted via the tools. Archive = soft delete; the node disappears from search and retrieval but can be restored at any time.

| Tool | What it does |
|------|-------------|
| `forget_node` | Archive a node with a reason. Strict protocol: only after drift surfaces a candidate or the user explicitly confirms. |
| `restore_node` | Un-archive a node so it surfaces in search again. |
| `list_archived` | Review what's been forgotten. Optionally scope by domain. |

### Drift detection

| Tool | What it does |
|------|-------------|
| `drift` | Surface nodes that may be stale, contradicted, duplicated, or transient and overdue for archiving. Returns candidates for review — never archives automatically. |

### Domain aliases

| Tool | What it does |
|------|-------------|
| `add_alias` | Register an alternative name for a domain so both names return the same results. |
| `remove_alias` | Remove a registered alias. Returns an error if it does not exist. |
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

The `dream` subcommand prints a digest of recent nodes and drift candidates — useful for session orientation and embedded automatically by the save hook at filing time.

```bash
memoryweb dream                              # reads ~/.memoryweb.db
memoryweb dream --db /path/to/your.db        # explicit DB path
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
- Add edges immediately after adding related nodes, or use `related_to` on `add_node` to auto-connect at creation time
- Call `recent_changes` or `summarise_domain` at the start of a session to orient without needing to know what to search for
- Use `find_connections` when asking about the relationship between two specific things
- Use `transient: true` for ticket state, sprint notes, or anything expected to go stale within days — `drift` will surface these for cleanup
- After filing a new node, call `suggest_edges` to find connection candidates before moving on

## Hooks

Two Claude Code hooks automate filing and pre-compaction capture.

### What they do

**`hooks/memoryweb_save_hook.sh`** (Stop hook — fires after every AI response)  
Counts human messages in the session transcript. Every `SAVE_INTERVAL` messages (default 15) it blocks the response and asks the model to call `add_nodes` and `add_edges` for anything significant before continuing. Before blocking, it runs `memoryweb dream` and embeds the resulting digest — recent nodes and drift candidates — directly in the `stopReason` so the model has live context before it files. If `memoryweb` is not available the hook still blocks but omits the digest. Uses a re-entry flag so the block fires once and allows immediately after the model files.

**`hooks/memoryweb_precompact_hook.sh`** (PreCompact hook — fires before context compaction)  
Blocks compaction once and asks the model to file everything important that hasn't been filed yet. Allows on re-entry so compaction proceeds after the filing pass.

### Install (Claude Code)

Run the setup tool once after building:

```bash
go build -o memoryweb-setup ./cmd/setup
./memoryweb-setup --hooks-dir /path/to/hooks
```

Or install manually:

```bash
chmod +x hooks/memoryweb_save_hook.sh hooks/memoryweb_precompact_hook.sh
```

Add to `~/.claude/settings.local.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/hooks/memoryweb_save_hook.sh"
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/hooks/memoryweb_precompact_hook.sh"
          }
        ]
      }
    ]
  }
}
```

Restart Claude Code to activate.

### Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `MEMORYWEB_SAVE_INTERVAL` | `15` | Human messages between filing prompts. |
| `MEMORYWEB_DB` | `~/.memoryweb.db` | Path to the SQLite database. |

### Token cost

Unlike passive hooks, these cost tokens because the model must actually produce quality nodes. Expect one short filing exchange per trigger — typically under 1,000 tokens for a focused session.

### Other tools

**GitHub Copilot and Claude Desktop do not support hooks.** For those tools, add session-start and filing instructions to your system prompt manually.
