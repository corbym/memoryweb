# memoryweb

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

## Hooks

Two Claude Code hooks automate filing and pre-compaction capture.

### What they do

**`hooks/memoryweb_save_hook.sh`** (Stop hook — fires after every AI response)  
Counts human messages in the session transcript. Every `SAVE_INTERVAL` messages (default 15) it blocks the response and asks the model to call `add_nodes` and `add_edges` for anything significant before continuing. Uses a re-entry flag so the block fires once and allows immediately after the model files.

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

Set `MEMORYWEB_SAVE_INTERVAL` in the hook's environment to change the trigger frequency (default: 15 human messages).

### Token cost

Unlike passive hooks, these cost tokens because the model must actually produce quality nodes. Expect one short filing exchange per trigger — typically under 1,000 tokens for a focused session.

### Other tools

**GitHub Copilot and Claude Desktop do not support hooks.** For those tools, add session-start and filing instructions to your system prompt manually.

