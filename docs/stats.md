# Session stats — reading the output and what the scores mean

memoryweb can record a structured summary of every MCP session. Enable it by
setting `MEMORYWEB_STATS_FILE` to a writable path in your MCP server config:

```json
{
  "mcpServers": {
    "memoryweb": {
      "command": "/path/to/memoryweb",
      "env": {
        "MEMORYWEB_DB": "/Users/yourname/.memoryweb.db",
        "MEMORYWEB_STATS_FILE": "/Users/yourname/.memoryweb-stats.log"
      }
    }
  }
}
```

When the session ends the server appends one entry to the file. The file grows
over time — one entry per session. You can `tail` it, open it in a text editor,
or `grep` the machine-readable `<!-- data: … -->` lines for programmatic
analysis.

---

## Example output

```
<!-- data: {"start_ts":"2026-04-30T09:15:00Z","wkd":18.5,"type":"filing","nodes":5,"edges":4,"orphans":1,"transient":1,"ratio":0.67,"burst":false} -->

=== memoryweb session -- 2026-04-30 09:15 UTC ===
Active 12 min | 14 tool calls across deep-game (3), sedex (2)
  Most used    search x4, remember x3, connect x3, orient x2, recall x2
  Retrieval    67% retrieval->action ratio (good - checks before filing)
  Filed        5 nodes, 4 edges, 1 transient
  Orphans      1 node(s) filed but never connected - consider linking or archiving
  Usefulness   B+   WKD 18.5

-- 30-day trend --
  Sessions      8 total (2 retrieval-only)
  WKD median    14.2 (30d)  17.8 (7d)  ^ improving
  Retrieval pct 25% of sessions were retrieval-only
=== end ===
```

---

## Fields explained

### Session header

| Field | What it means |
|-------|---------------|
| `Active N min` | Wall-clock time from first to last tool call. |
| `tool calls` | Total number of MCP tool invocations this session. |
| `across …` | Domains touched, with per-domain node counts. |
| `Most used` | Up to 5 tools ranked by call count. |
| `Errors` | Number of tool calls that returned an error, and what percentage of total calls that represents. Only shown when > 0. |

### Session types

| Type | What it means |
|------|---------------|
| `filing` | At least one node or edge was filed. WKD scoring applies. |
| `retrieval` | No nodes or edges filed; only retrieval tools used (≥ 3 calls). This is a positive signal — the agent was using memoryweb for context rather than bulk-filing. |
| `minimal` | Very few calls with no substantive activity. |

### Retrieval ratio

```
Retrieval    67% retrieval->action ratio (good - checks before filing)
```

Measures how often a retrieval call (search, recall, orient, etc.) was followed
within 3 tool calls by a write (remember, connect, revise, etc.).

| Ratio | Label | What it signals |
|-------|-------|-----------------|
| ≥ 40% | good — checks before filing | Agent is looking before it writes |
| < 40% | low — filing without retrieving first | Possible duplicate risk; agent may be filing blindly |

### Filed / orphans / transient

```
Filed        5 nodes, 4 edges, 1 transient
Orphans      1 node(s) filed but never connected
```

- **Nodes**: total nodes created this session via `remember` or `remember_all`.
- **Edges**: total connections created via `connect`, `connect_all`, or `merge`.
- **Transient**: nodes filed with `transient: true` (sprint notes, ticket state).
  These contribute a small score penalty since they're expected to be archived soon.
- **Orphans**: nodes filed but never connected to anything this session.
  Orphans are the primary source of graph entropy — they reduce the WKD score.
  When you see this message, run `disconnected` to find the loose ends and either
  link them or archive them.

---

## WKD score and grade

**WKD** is a composite score that measures how well-knit the knowledge filed this
session is — that is, how likely it is to be retrievable and useful in future
sessions.

### Formula

```
WKD = (connected_nodes × 2.0)
    + (edges_filed    × 1.5)
    - (orphans        × 1.0)
    - (transient      × 0.5)
    + (retrieval_ratio × 10.0)
```

- **Connected nodes**: `min(nodes_filed, edges_filed)` — nodes that have at
  least one edge leaving or arriving at them this session.
- **Edges**: the narrative thread. Each edge makes nodes more reachable.
- **Orphans**: `max(0, nodes_filed - edges_filed)` — nodes that ended the
  session unlinked. They cost a point each.
- **Transient penalty**: each transient node costs half a point.
- **Retrieval ratio bonus**: up to +10 when every retrieval is followed by a
  write. Rewards the pattern of "check → file", not just bulk-filing.

### Grades

| Grade | WKD | What it means |
|-------|-----|---------------|
| `A` | ≥ 25 | Excellent — densely connected, well-retrieved-before-written. |
| `B+` | ≥ 15 | Good — mostly connected with solid retrieval habits. |
| `B` | ≥ 8 | Decent — some orphans or low retrieval ratio; worth improving. |
| `C` | ≥ 2 | Below average — many orphaned nodes or very little retrieval. |
| `D` | ≥ 0 | Sparse — almost no connections or cross-referencing. |
| `D-` | < 0 | Penalty-heavy — orphans and transient nodes outweigh connections. |

Burst sessions (> 15 nodes in one session) are excluded from the trend median
because bulk imports skew the baseline.

### Retrieval-only sessions

When no nodes or edges are filed, WKD is not computed and the grade shows
`retrieval-only`. This is not a failure — a session that pulls context without
filing anything is valuable. The 30-day trend tracks these separately.

---

## 30-day trend

The trend block appears when there are prior sessions in the log file.

```
-- 30-day trend --
  Sessions      8 total (2 retrieval-only)
  WKD median    14.2 (30d)  17.8 (7d)  ^ improving
  Retrieval pct 25% of sessions were retrieval-only
```

| Field | What it means |
|-------|---------------|
| `Sessions` | Total sessions in the last 30 days, with how many were retrieval-only. |
| `WKD median (30d)` | Median WKD across all non-burst filing sessions in the past 30 days. |
| `WKD median (7d)` | Same, but only the past 7 days. |
| Trend symbol | `^ improving` when 7d median > 30d median by > 15%; `v declining` when < 85%; `-> stable` otherwise. |
| `Retrieval pct` | What fraction of sessions were retrieval-only. A high percentage signals the graph is being used heavily for context — a good sign. |

---

## Machine-readable data line

Every entry begins with a `<!-- data: … -->` comment containing a JSON object.
This lets you parse the log with standard tools.

```json
{
  "start_ts":  "2026-04-30T09:15:00Z",
  "wkd":       18.5,
  "type":      "filing",
  "nodes":     5,
  "edges":     4,
  "orphans":   1,
  "transient": 1,
  "ratio":     0.67,
  "burst":     false
}
```

Extract all data lines:

```bash
grep '^<!-- data:' ~/.memoryweb-stats.log \
  | sed 's/<!-- data: //;s/ -->//' \
  | jq -s .
```

Plot WKD over time:

```bash
grep '^<!-- data:' ~/.memoryweb-stats.log \
  | sed 's/<!-- data: //;s/ -->//' \
  | jq -r '[.start_ts, .wkd] | @csv'
```

---

## Tips for improving your score

1. **Reconnect orphans immediately.** After `remember`, call `connect` or use
   `related_to` on `remember` to link the node before moving on.

2. **Search before you file.** Call `search` or `orient` before `remember`.
   This keeps the retrieval ratio high and guards against duplicates.

3. **Avoid transient nodes in long-lived graphs.** Transient is for sprint
   notes and ticket state that you expect to archive within days. If a node will
   still matter next week, don't mark it transient.

4. **Use `disconnected` regularly.** It surfaces nodes that were filed but
   never linked. Either connect them or archive them.

5. **Don't chase the grade during burst sessions.** Importing a large backlog
   in one go is expected to score lower — burst sessions are excluded from the
   trend so they don't skew your baseline.

