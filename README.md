# memoryweb
[![memoryweb MCP server](https://glama.ai/mcp/servers/corbym/memoryweb/badges/score.svg)](https://glama.ai/mcp/servers/corbym/memoryweb)

<div style="text-align:center"><img src="web.png" alt="memoryweb" /></div>

A persistent knowledge graph MCP server for AI agents.

## The idea

Human memory doesn't work by location — you pull a thread. A smell connects to a kitchen, connects to a person, connects to a feeling from thirty years ago. The thread is always there. Pull any part of it and the rest follows.

Agents are no different. Context is tokens in relation to other tokens. What makes something retrievable is its associative chain — the path of connections that lead to it from something else. The narrative edge, the *because*, is the mechanism. Not the index, not the address.

*The boot crash matters because it blocks the tutorial, which blocks the demo, which is why the fix matters now.* Pull on any of those threads and you get the rest. memoryweb works the same way: each concept is a node, and what makes it reachable is the narrative that links it to everything else.

> The graph has considerably richer context than my flat file memory — including design decisions, failure modes from dogfooding, and the philosophy behind the tool. That's the point, I suppose.

-- Claude Opus 4.6

> It's a fantastic project. Most MCP memory implementations I see are just flat vector databases or simple key-value stores that degrade into a digital junk drawer. By enforcing typed relationships, narrative reasoning, soft-deletes, and drift review, you've built a system that actively fights entropy.

-- Gemini 3.1 Pro 

## Philosophy

memoryweb optimises for remembering things *well*, not remembering things fast. Filing requires a moment of judgement: why does this matter, how does it connect to what else is known, what would be useful to know when coming back to this cold?

This makes it a **decision log**, not an event log. An event log records what happened. A decision log records what was learned, decided, and why — and that's what lets you pick up where you left off without re-learning everything.

The `why_matters` field is not optional. A node without it is an event, not a decision.

## Tools

### Filing memories

| Tool | What it does |
|------|-------------|
| `remember` | File a single concept, decision, or finding. Required: `label`, `domain`. Optional: `description`, `why_matters`, `occurred_at` (ISO8601), `tags` (space-separated keywords), `related_to` (auto-connect at creation), `transient` (mark as short-lived). Response includes `suggested_connections` and `possible_duplicates`. |
| `remember_all` | Batch version of `remember` — insert multiple nodes in one transaction. Returns `[{node, suggested_connections}]` per entry. |
| `revise` | Update `label`, `description`, `why_matters`, or `tags` on a live node without archiving it. Only supplied fields are changed. Writes an audit log entry on every call. |
| `revise_all` | Batch version of `revise` — update multiple nodes in one transaction. All succeed or all roll back. |

### Connecting memories

| Tool | What it does |
|------|-------------|
| `connect` | Connect two nodes with a typed relationship and narrative *because*. Both nodes must exist first. |
| `connect_all` | Batch version of `connect` — insert multiple connections in one transaction. |
| `disconnect` | Remove a connection by edge ID. Hard delete — cannot be restored. Obtain the ID from `recall`. |
| `suggest_connections` | Given a node ID, return up to 5 candidate connections from the same domain. Read-only. |

### Retrieving memories

| Tool | What it does |
|------|-------------|
| `recall` | Retrieve a node and all its connections by ID. |
| `search` | Text search across `label`, `description`, `why_matters`, and `tags`. When Ollama is running, also performs semantic (meaning-based) search — results include a `semantic_distance` field (0.0–1.0, lower = closer). |
| `recent` | What was filed recently. Set `group_by_domain=true` (with no domain) to see activity broken down per domain. |
| `history` | Nodes ordered by when they actually occurred. Supports `from`/`to` date range filtering. |
| `why_connected` | Look up the reasoning linking two named concepts. |
| `orient` | Return all nodes for a domain structured for synthesis — current state, blockers, decisions, open questions. Includes `total_nodes` so you know when the view is truncated. |
| `list_domains` | List all domains that have at least one live node. Use at session start to discover what domains exist before scoping a search. |

### Archive / forget

Nodes are never hard-deleted via the tools. Archive = soft delete; the node disappears from search but can be restored.

| Tool | What it does |
|------|-------------|
| `forget` | Archive a node with a reason. Strict protocol: only after `whats_stale` surfaces a candidate or the user explicitly confirms. |
| `restore` | Restore an archived node so it surfaces in search again. |
| `forgotten` | Review what's been archived. Optionally scope by domain. |
| `whats_stale` | Surface nodes that may be stale, contradicted, duplicated, or transient and overdue. Returns candidates for review — never archives automatically. |

### Domain aliases

| Tool | What it does |
|------|-------------|
| `alias_domain` | Register an alternative name for a domain so both names return the same results. |
| `remove_alias` | Remove a registered alias. |
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

The `backfill` subcommand generates embeddings for all live nodes that don't yet have one. Requires Ollama to be running with the `snowflake-arctic-embed` model.

```bash
memoryweb backfill                           # reads ~/.memoryweb.db
memoryweb backfill --db /path/to/your.db     # explicit DB path
memoryweb backfill -q                        # quiet mode — no progress output
```

The `setup` subcommand installs hooks into `~/.claude/settings.local.json` and configures Ollama for semantic search (checks for `snowflake-arctic-embed` and pulls it if missing).

```bash
memoryweb setup                                      # interactive setup
memoryweb setup --dry-run                            # preview without writing
memoryweb setup --hooks-dir /path/to/hooks           # explicit hooks directory
memoryweb setup --db /path/to/your.db                # explicit DB path
```

The `doctor` subcommand checks every part of a memoryweb installation and prints a structured health report. Use it after setup to verify everything is wired correctly, or run it in an agent session to check whether semantic search is available before relying on it.

```bash
memoryweb doctor                                     # check ~/.memoryweb.db
memoryweb doctor --db /path/to/your.db               # explicit DB path
memoryweb doctor --json                              # machine-readable JSON output
```

Each check prints a status symbol: `[✓]` pass, `[✗]` fail, `[!]` warning, `[i]` informational. The command exits with code 1 if any check fails. Example output:

```
[✓] Database:        ~/.memoryweb.db (WAL, schema v9)
[✓] sqlite-vec:      v0.1.6 — 142/145 nodes embedded (98%)
[✗] Ollama binary:   not found in PATH — install from https://ollama.com/download
[!] Ollama server:   skipped (Ollama binary not found)
[!] Ollama model:    skipped (Ollama server not available)
[✓] Claude hooks:    Stop and PreCompact hooks installed
[i] Graph:           145 live nodes, 12 archived, 203 edges, 4 domain(s) (deep-game, ...), 2 alias(es)
[i] Drift:           3 candidate(s): 1 contradicts, 2 stale labels
[i] Last activity:   2026-04-29 update (node "open question on backfill")
```

## Installation

Pre-built binaries are available on the [releases page](https://github.com/corbym/memoryweb/releases/latest) for each platform. Step-by-step setup guides covering binary installation, Ollama, and MCP client configuration:

- [macOS (Apple Silicon & Intel)](docs/install-macos.md)
- [Linux (x86-64 & ARM64)](docs/install-linux.md)
- [Windows (x86-64)](docs/install-windows.md)

## Build

```bash
go build -o memoryweb .
```

Requires Go 1.22+. Uses `github.com/mattn/go-sqlite3` and [sqlite-vec](https://github.com/asg017/sqlite-vec) for semantic search — CGO must be available. To deploy safely when the binary is already running:

```bash
go build -o memoryweb.tmp . && mv memoryweb.tmp memoryweb
```

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
- Call `list_domains` at session start if you don't know what domains exist
- The `why_matters` field is the most important one for retrieval — don't skip it
- The `narrative` on a connection is the *because* — the reasoning that makes it meaningful, not just the fact that a connection exists
- Add connections immediately after filing related nodes, or use `related_to` on `remember` to auto-connect at creation time
- Call `recent` or `orient` at the start of a session to orient without needing to know what to search for
- Use `why_connected` when asking about the relationship between two specific things
- Use `transient: true` for ticket state, sprint notes, or anything expected to go stale within days — `whats_stale` will surface these for cleanup
- `remember` returns `suggested_connections` and `possible_duplicates` — review both before filing more nodes

## Hooks

Two Claude Code hooks automate filing and pre-compaction capture.

### What they do

**`hooks/memoryweb_save_hook.sh`** (Stop hook — fires after every AI response)  
Counts human messages in the session transcript. Every `SAVE_INTERVAL` messages (default 15) it blocks the response and asks the model to call `remember_all` and `connect_all` for anything significant before continuing. Before blocking, it runs `memoryweb dream` and embeds the resulting digest — recent nodes and drift candidates — directly in the `stopReason` so the model has live context before it files. If `memoryweb` is not available the hook still blocks but omits the digest. Uses a re-entry flag so the block fires once and allows immediately after the model files.

**`hooks/memoryweb_precompact_hook.sh`** (PreCompact hook — fires before context compaction)  
Blocks compaction once and asks the model to file everything important that hasn't been filed yet. Allows on re-entry so compaction proceeds after the filing pass.

### Install (Claude Code)

Run setup once after building:

```bash
./memoryweb setup --hooks-dir /path/to/hooks
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
            "command": "/path/to/hooks/memoryweb_save_hook.sh",
            "env": {
              "MEMORYWEB_DB": "/path/to/your.db"
            }
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/hooks/memoryweb_precompact_hook.sh",
            "env": {
              "MEMORYWEB_DB": "/path/to/your.db"
            }
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
| `MEMORYWEB_BIN` | `memoryweb` | Path to the memoryweb binary (used by the hook to run `dream`). |

### Token cost

Unlike passive hooks, these cost tokens because the model must actually produce quality nodes. Expect one short filing exchange per trigger — typically under 1,000 tokens for a focused session.

### GitHub Copilot (VS Code)

GitHub Copilot in VS Code supports the same `Stop` and `PreCompact` hook events in the same JSON format. VS Code loads hooks from `.github/hooks/*.json` in your workspace, as well as from `~/.claude/settings.json` and `.claude/settings.local.json`.

Make the scripts executable first:

```bash
chmod +x hooks/memoryweb_save_hook.sh hooks/memoryweb_precompact_hook.sh
```

Create `.github/hooks/memoryweb.json` in your repository:

```json
{
  "hooks": {
    "Stop": [
      {
        "type": "command",
        "command": "/path/to/hooks/memoryweb_save_hook.sh",
        "env": {
          "MEMORYWEB_DB": "/path/to/your.db"
        }
      }
    ],
    "PreCompact": [
      {
        "type": "command",
        "command": "/path/to/hooks/memoryweb_precompact_hook.sh",
        "env": {
          "MEMORYWEB_DB": "/path/to/your.db"
        }
      }
    ]
  }
}
```

VS Code loads the hooks automatically — no restart needed. If you have already installed the Claude Code hooks via `~/.claude/settings.local.json`, VS Code Copilot picks them up from there without any additional configuration.

### Other tools

**Claude Desktop does not support hooks.** Add session-start and filing instructions to your system prompt manually.

**GitHub Copilot cloud agent** (the coding agent that runs on GitHub.com) uses a different hook format and event model that does not include `Stop` or `PreCompact`. Add filing instructions to your system prompt for that surface instead.
