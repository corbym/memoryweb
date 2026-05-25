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

## Table of contents

- [Installation](#installation)
- [MCP config](#mcp-config)
- [Storage](#storage)
- [Tools](#tools)
  - [Filing memories](#filing-memories)
  - [Connecting memories](#connecting-memories)
  - [Retrieving memories](#retrieving-memories)
  - [Archive / forget](#archive--forget)
  - [Domain management](#domain-management)
  - [Relationship types](#relationship-types)
- [Conventions](#conventions)
- [CLI](#cli)
- [Hooks](#hooks)
- [Updating](#updating)
- [Build](#build)

## Installation

**Homebrew (macOS and Linux — recommended):**

```bash
brew tap corbym/memoryweb
brew install memoryweb
```

Pre-built binaries are also available on the [releases page](https://github.com/corbym/memoryweb/releases/latest) for each platform. Step-by-step setup guides covering installation, Ollama, and MCP client configuration:

- [macOS (Apple Silicon & Intel)](docs/install-macos.md)
- [Linux (x86-64 & ARM64)](docs/install-linux.md)
- [Windows (x86-64)](docs/install-windows.md)

Once installed, see the **[User guide](docs/user-guide.md)** for how to orient the agent, what phrases to use, and how to get the most out of memoryweb in Claude Code, GitHub Copilot, and Claude Desktop.

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

`memoryweb setup` writes this file automatically when the Claude application directory is detected.

> **Note:** ChatGPT Desktop does not support stdio-based MCP servers and is not compatible with memoryweb.

## Storage

Default DB path: `~/.memoryweb.db`

Override with `MEMORYWEB_DB=/path/to/your.db`

## Tools

### Filing memories

| Tool | What it does |
|------|-------------|
| `remember` | File a single concept, decision, or finding. Required: `label`, `domain`. Optional: `description`, `why_matters`, `occurred_at` (ISO8601), `tags` (space-separated keywords), `related_to` (auto-connect at creation), `transient` (mark as short-lived). Supply an `items` array to file multiple nodes in one transaction. Response includes `suggested_connections` and `possible_duplicates`. |
| `revise` | Update `label`, `description`, `why_matters`, `tags`, or `occurred_at` on a live node without archiving it. Supply an `items` array for batch updates. Writes an audit log entry on every call. |

### Connecting memories

| Tool | What it does |
|------|-------------|
| `connect` | Connect two nodes with a typed relationship and narrative *because*. Both nodes must exist first. Supply an `items` array to create multiple connections in one transaction. |
| `disconnect` | Remove a connection by edge ID. Hard delete — cannot be restored. Obtain the ID from `recall`. |
| `suggest_connections` | Given a node ID, return up to 5 candidate connections. Returns a `domain` field on each suggestion so cross-domain connects can be scoped correctly. Read-only. |

### Retrieving memories

| Tool | What it does |
|------|-------------|
| `recall` | Retrieve a node and all its connections by ID. |
| `search` | Text search across `label`, `description`, `why_matters`, and `tags`. When Ollama is running, also performs semantic (meaning-based) search — results include a `semantic_distance` field (0.0–1.0, lower = closer). Returns `truncated: true` when results are capped by the limit. |
| `recent` | What was filed recently. Set `group_by_domain=true` (with no domain) to see activity broken down per domain. |
| `history` | Nodes ordered by when they actually occurred. Supports `from`/`to` date range filtering, `tags` filtering, and `important_only` for the curated decision spine only. |
| `why_connected` | Look up the reasoning linking two named concepts (by label). |
| `trace` | Find the shortest chain of relationships between two nodes (by ID). Returns intermediate nodes and edges up to 6 hops. Synthesise the result into a narrative explaining how one concept leads to the other. |
| `orient` | Return all nodes for a domain structured for synthesis — current state, recent activity, and a `declared_spine` of key decisions in chronological order. Includes `total_nodes` and `server_version`. |
| `visualise` | Mermaid flowchart for a domain or a single node's neighbourhood (pass `memory_id`). Output inside a mermaid code block. |
| `significance` | Dual-signal importance analysis for a domain. Returns four sections: `declared` (nodes with `occurred_at` set), `structural` (ranked by recency-weighted inbound degree), `uncurated` (structural top-N without `occurred_at` — curation candidates), and `potentially_stale` (declared but low structural score). |

### Archive / forget

Nodes are never hard-deleted via the tools. Archive = soft delete; the node disappears from search but can be restored.

| Tool | What it does |
|------|-------------|
| `forget` | Archive a node with a reason. Strict protocol: only after `audit(mode=stale)` surfaces a candidate or the user explicitly confirms. |
| `forget_all` | Archive multiple nodes atomically in a single call. Same strict protocol applies. |
| `restore` | Restore an archived node so it surfaces in search again. |
| `audit` | Surface nodes that need attention. `mode=stale` — stale, contradicted, duplicated, or overdue transient nodes. `mode=orphans` — live nodes with zero connections. `mode=archived` — review what has been archived. |

### Domain management

| Tool | What it does |
|------|-------------|
| `domains` | List all domains with at least one live node, and all registered aliases. |
| `alias` | Manage domain aliases. Actions: `add`, `remove`, `resolve`, `list`. Register short aliases so both `dg` and `deep-game` return the same results. |
| `rename_domain` | Rename a domain in place. Automatically registers an alias from the old name so existing references keep working. Fails with a clear error if the new domain already has live nodes — use `merge-domains` (CLI) instead. |

### Relationship types
`caused_by` `led_to` `blocked_by` `unblocks` `connects_to` `contradicts` `depends_on` `is_example_of`

## Conventions

- Use `domain` to separate concerns: `deep-game`, `sedex`, `general`
- Call `domains` at session start if you don't know what domains exist
- The `why_matters` field is the most important one for retrieval — don't skip it
- The `narrative` on a connection is the *because* — the reasoning that makes it meaningful, not just the fact that a connection exists
- Add connections immediately after filing related nodes, or use `related_to` on `remember` to auto-connect at creation time
- Call `orient` at the start of a session to orient without needing to know what to search for
- Use `why_connected` when asking about the relationship between two specific things
- Use `transient: true` for ticket state, sprint notes, or anything expected to go stale within days — `audit(mode=stale)` will surface these for cleanup
- `remember` returns `suggested_connections` and `possible_duplicates` — review both before filing more nodes

## CLI

The `purge` subcommand hard-deletes archived nodes from the database. It is intentionally not exposed as an MCP tool — it's a maintenance operation, not an agent operation.

```bash
memoryweb purge --dry-run              # show what would be deleted (default behaviour without --confirm)
memoryweb purge --confirm              # actually deletes
memoryweb purge --domain sedex         # scope to a domain
memoryweb purge --before 2026-01-01    # only nodes archived before a date
```

The `dream` subcommand prints a digest of recent nodes and drift candidates — useful for session orientation and embedded automatically by the save and precompact hooks at filing time.

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

The `setup` subcommand installs hooks into `~/.claude/settings.local.json`, detects Claude Desktop and offers to configure it automatically, and configures Ollama for semantic search. If Ollama is not installed, `setup` will ask whether to install it automatically via `https://ollama.com/install.sh` (Linux and macOS only — on Windows you must install Ollama manually before running setup). If Ollama is already installed but the server is not running, `setup` starts it automatically. Finally it checks for `snowflake-arctic-embed` and pulls it if missing.

```bash
memoryweb setup                                      # interactive setup
memoryweb setup --dry-run                            # preview without writing
memoryweb setup --hooks-dir /path/to/hooks           # explicit hooks directory
memoryweb setup --db /path/to/your.db                # explicit DB path
```

When Claude Desktop is detected, setup prints:

```
Detected Claude Desktop. Configure it? [y/N]
```

The `stats` feature records tool usage for every MCP session. See [docs/stats.md](docs/stats.md) for setup and how to read the output.

The `doctor` subcommand checks every part of a memoryweb installation and prints a structured health report. Use it after setup to verify everything is wired correctly, or run it in an agent session to check whether semantic search is available before relying on it.

```bash
memoryweb doctor                                     # check ~/.memoryweb.db
memoryweb doctor --db /path/to/your.db               # explicit DB path
memoryweb doctor --json                              # machine-readable JSON output
```

Each check prints a status symbol: `[✓]` pass, `[✗]` fail, `[!]` warning, `[i]` informational. The command exits with code 1 if any check fails. Example output:

```
[✓] Database:        ~/.memoryweb.db (WAL, schema v11)
[✓] sqlite-vec:      v0.1.6 — 142/145 nodes embedded (98%)
[✗] Ollama binary:   not found in PATH — install from https://ollama.com/download
[!] Ollama server:   skipped (Ollama binary not found)
[!] Ollama model:    skipped (Ollama server not available)
[✓] Claude hooks:    Stop and PreCompact hooks installed
[i] Graph:           145 live nodes, 12 archived, 203 edges, 4 domain(s) (deep-game, ...), 2 alias(es)
[i] Drift:           3 candidate(s): 1 contradicts, 2 stale labels
[i] Last activity:   2026-04-29 update (node "open question on backfill")
[i] Update:          running dev build — skipping update check
```

The `merge-domains` subcommand consolidates two domains into one:

```bash
memoryweb merge-domains --source <domain> --target <domain> [--dry-run]
```

- `--dry-run` reports what would happen without making any changes
- Detects label collisions between the two domains — reported as warnings, not blocking
- Automatically creates an alias from source → target

The `check-for-updates` subcommand checks GitHub for a newer release:

```bash
memoryweb check-for-updates
```

## Hooks

Two Claude Code hooks automate filing and pre-compaction capture.

### What they do

**`hooks/memoryweb_save_hook.sh`** (Stop hook — fires after every AI response)  
Counts human messages in the session transcript. Every `SAVE_INTERVAL` messages (default 15) it blocks the response and asks the model to call `remember` and `connect` for anything significant before continuing. Before blocking, it runs `memoryweb dream` and embeds the resulting digest — recent nodes and drift candidates — directly in the `stopReason` so the model has live context before it files. If `memoryweb` is not available the hook still blocks but omits the digest. Uses a re-entry flag so the block fires once and allows immediately after the model files.

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

**Claude Desktop and GitHub Copilot cloud agent do not support hooks.** Add session-start and filing instructions to your system prompt manually. `memoryweb setup` configures Claude Desktop's MCP server entry automatically when it detects the application's data directory.

**GitHub Copilot cloud agent** (the coding agent that runs on GitHub.com) uses a different hook format and event model that does not include `Stop` or `PreCompact`. Add filing instructions to your system prompt for that surface instead.

## Updating

To check whether a newer version is available, run:

```bash
memoryweb doctor
```

The `Update:` line in the output will tell you if a newer release is available and where to download it.

To update:

**Homebrew:**

```bash
brew update && brew upgrade memoryweb
```

**Manual:**

1. Download the latest binary for your platform from the [releases page](https://github.com/corbym/memoryweb/releases/latest).
2. Replace the existing binary (build tip: rename to `memoryweb.tmp` first, then `mv memoryweb.tmp memoryweb` so the replacement is atomic).
3. Restart your MCP client (Claude Code, Claude Desktop, etc.) so it picks up the new binary.

Your database is forward-compatible — the binary runs any pending migrations automatically on startup.

## Build

```bash
go build -o memoryweb .
```

Requires Go 1.22+. Uses `github.com/mattn/go-sqlite3` and [sqlite-vec](https://github.com/asg017/sqlite-vec) for semantic search — CGO must be available. To deploy safely when the binary is already running:

```bash
go build -o memoryweb.tmp . && mv memoryweb.tmp memoryweb
```
