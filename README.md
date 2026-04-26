# memoryweb

A memory web MCP server for Claude. Stores knowledge as a graph of nodes connected by typed, narrative edges — designed for how LLMs actually retrieve information, via associative chains and context, rather than spatial hierarchies.

## The idea

Memory palaces work for humans because spatial navigation is a mental shortcut. LLMs navigate token relationships. A node with rich contextual edges ("this connects to that *because*...") is reachable from multiple entry points. A room with a label and contents requires the exact label.

## Tools

| Tool | What it does |
|------|-------------|
| `add_node` | Add a concept, decision, or finding. Requires a label, domain, and optionally a description and "why it matters". |
| `add_edge` | Connect two nodes with a typed relationship and a narrative "because". |
| `get_node` | Retrieve a node and all its connected edges. |
| `search_nodes` | Text search across label, description, and why_matters. Optionally scope to a domain. |
| `recent_changes` | What was filed recently. Good for session orientation. |

### Relationship types
`caused_by` `led_to` `blocked_by` `unblocks` `connects_to` `contradicts` `depends_on` `is_example_of`

## Build

```bash
go build -o memoryweb .
```

Requires Go 1.22+. Uses `github.com/mattn/go-sqlite3` — CGO must be available.

## Storage

Default DB path: `~/.memoryweb.db`

Override with `MEMORYWEB_DB=/path/to/your.db`

## Claude Desktop config

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS):

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

## Dogfooding conventions

- Use `domain` to separate concerns: `deep-game`, `sedex`, `general`
- The `why_matters` field is the most important one for retrieval — don't skip it
- Add edges immediately after adding related nodes — the narrative edge is what makes the web navigable
- Call `recent_changes` at the start of a session to orient without needing to know what to search for
