# memoryweb

A memory MCP server for agents. Stores knowledge as a graph of concepts connected by typed, narrative edges — designed for how humans actually remember things: not by location, but by story.

## The idea

You don't remember facts in isolation. You remember them because of what they connect to. *The boot crash is significant because it blocks the tutorial, which blocks the demo, which is why the fix matters now.* Pull on any of those threads and you get the rest.

memoryweb works the same way. Each concept is a node. What makes it retrievable is the narrative edge — the *because* that links it to something else. A concept with rich connections is reachable from many starting points. A concept filed alone, with no story linking it to anything, is effectively lost.

## Tools

| Tool | What it does |
|------|-------------|
| `add_node` | File a concept, decision, or finding. Requires a label, domain, and optionally a description and "why it matters". |
| `add_edge` | Connect two concepts with a typed relationship and a narrative "because". |
| `get_node` | Retrieve a concept and all its connections. |
| `search_nodes` | Text search across label, description, and why_matters. Optionally scope to a domain. Returns matching concepts and any edges between them. |
| `find_connections` | Look up the specific reasoning linking two named concepts. Use this when asked why or how two things relate. |
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
