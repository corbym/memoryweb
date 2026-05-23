# visualise Truncation Metadata

> **COMPLETE** — shipped prior to 2026-05-23.
> - Truncation envelope (`mermaid`, `node_count`, `nodes_total`, `edge_count`, `edges_total`, `truncated`) in place — `tools/tools.go` ~line 1452.
> - Description already says "Not suitable for orphan detection or programmatic analysis — use audit(mode=orphans)" and "Output may be truncated for large domains."
> - `GetDomainGraph` returns total counts — `tools/tools.go` ~line 1392.

Adds a truncation envelope to `visualise` responses and updates the description to
explicitly state what the tool is not for. Mirrors recordari STORY-050 (shipped 2026-05-21).

---

## Motivation

An agent used `visualise` to infer orphans from a large domain. `visualise` silently dropped
edges when the graph exceeded its output limit, making connected nodes appear isolated. The
agent flagged false orphans in a digest and acted on them.

Two failure modes:
1. **Wrong tool called**: `visualise` is for human visual inspection. Agents should use
   `audit(mode=orphans)` for orphan detection. The description didn't say this.
2. **Silent truncation**: no signal that the output was incomplete. An agent seeing a
   partial graph assumes it is complete.

The description fix (1) has higher leverage — it prevents the misuse before it happens.
The metadata fix (2) is defensive — catches truncation when `visualise` is correctly used
on a large domain.

---

## Changes

### 1. Tool description update

Replace the current `visualise` description with one that leads with what the tool is
**not** for:

> "For human visual inspection only. **Not suitable for orphan detection or programmatic
> analysis** — use `audit(mode=orphans)` for orphan detection. Output may be truncated for
> large domains; see `truncated` in the response. Returns a Mermaid diagram of the domain
> graph."

Remove the "if the client supports HTML widgets" conditional (see also
tool-description-quality-pass.md issue 7).

### 2. Metadata envelope in response

Wrap the Mermaid output in a JSON envelope:

```json
{
  "truncated": false,
  "nodes_shown": 23,
  "nodes_total": 23,
  "edges_shown": 41,
  "edges_total": 41,
  "data": "graph LR\n  ..."
}
```

`truncated` is `true` iff `nodes_shown < nodes_total || edges_shown < edges_total`.

In `db/db.go`, `GetDomainGraph` (or equivalent) must return total counts alongside the
graph data. The handler in `tools/tools.go` computes `truncated` and wraps the response.

The recordari STORY-050 implementation is the reference. Check
`recordari/internal/mcp/visualise_envelope_test.go` for the test pattern.

---

## Acceptance criteria

- `TestVisualise_ResponseEnvelope`: call `visualise` on a domain with at least one node;
  assert the response JSON contains `truncated`, `nodes_shown`, `nodes_total`,
  `edges_shown`, `edges_total`, and `data` fields.
- `TestVisualise_TruncatedTrue`: populate a domain with more nodes than the visualise
  limit; assert `truncated: true` and `nodes_shown < nodes_total`.
- `TestVisualise_TruncatedFalse`: populate a domain within the limit; assert
  `truncated: false` and `nodes_shown == nodes_total`.
- `visualise` tool description contains "Not suitable for orphan detection".
- `visualise` tool description contains "audit(mode=orphans)".
- `visualise` tool description contains no "if the client supports" conditional.
- `go test ./...` green.

---

## Files

- `db/db.go` — `GetDomainGraph` (or `BuildGraphMermaid`) returns total node/edge counts
- `tools/tools.go` — `handleVisualise`, response struct, description
- `tools/tools_test.go` — new envelope and truncation tests

## References

- shared-surface node: `visualise-tool-add-truncation-me-618fdb93`
- recordari reference: STORY-050
