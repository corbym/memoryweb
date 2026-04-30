# Future Implementation Plan: Graph Enhancements

This plan outlines the TDD approach for implementing four core graph enhancements in `memoryweb`. Each feature strictly follows your established project conventions: soft deletes, audit logging, append-only migrations, and agent-steered tool descriptions.

## General TDD Workflow
For each feature below:
1. Write integration/tool tests in `tests/integration_test.go` (and/or `tools/tools_test.go`).
2. Run `go test ./...` and verify the new tests **fail**.
3. Implement the DB logic in `db/db.go`.
4. Implement the tool wrapper in `tools/tools.go`.
5. Run `go test ./...` and verify the tests **pass**.

---

## 1. `disconnected` (Graph Cleanliness)
**Goal:** Locate live, non-transient concepts that lack any connections.

### Tests (`integration_test.go`):
- `TestDisconnectedReturnsUnconnectedNodes`: Add one disconnected node and two connected nodes. Assert only the disconnected one is returned.
- `TestDisconnectedExcludesTransient`: Add a disconnected node with `transient=true`. Assert it is excluded.
- `TestDisconnectedExcludesArchived`: Assert archived disconnected nodes are excluded.

### Implementation:
- **DB (`db/db.go`)**: Add `FindDisconnected(domain string) ([]Node, error)`.
  *Query:* `SELECT ... FROM nodes WHERE archived_at IS NULL AND transient = 0 AND id NOT IN (SELECT from_node FROM edges UNION SELECT to_node FROM edges) AND domain = ?`
- **Tool (`tools/tools.go`)**: Add `disconnected`
  *Description Guidance:* "Return live, non-transient nodes with zero connections. Use this to find dropped context. Present findings to the user and suggest either linking them to related concepts using `connect`, or archiving them if they are no longer relevant."

---

## 2. `trace` (Multi-hop Retrieval)
**Goal:** Expose the shortest chain of reasoning between any two nodes.

### Tests (`tools/tools_test.go`):
- `TestTraceReturnsChain`: Add nodes A -> B -> C -> D. Call `trace` from A to D. Assert intermediate nodes B, C and all connecting edges are returned.
- `TestTraceNoConnection`: Call `trace` between two nodes in disconnected subgraphs. Assert a clear empty/not-found response (not an error).
- `TestTraceIgnoresArchived`: Add A -> B -> C. Archive B. Assert `trace` from A to C returns no path.

### Implementation:
- **DB (`db/db.go`)**: Add `FindPath(fromId, toId string, maxDepth int) ([]Node, []Edge, error)`.
  *Query:* Use a SQLite Recursive CTE (`WITH RECURSIVE`) to perform BFS traversal of `edges` up to `maxDepth` (hard cap: 6), joining `nodes` where `archived_at IS NULL`. Return the **shortest path** only.
- **Tool (`tools/tools.go`)**: Add `trace`
  *Description Guidance:* "Find the chain of relationships connecting two concepts. The result includes intermediate nodes and edges. Synthesize the steps into a clear, continuous narrative explaining how A leads to B."

---

## 3. `merge` (Graph Refactoring)
**Goal:** Consolidate duplicate concepts without losing connectivity, adhering to the soft-delete contract.

### Tests (`tools/tools_test.go`):
- `TestMergeRebasesEdges`: Create Node A (edges to C) and Node B (edges from D). Merge B into A. Assert A now has edges to C and from D.
- `TestMergeArchivesSource`: Assert Node B has `archived_at` set after merge.
- `TestMergeWritesAuditLog`: Assert `audit_log` has an entry for Node B with `action='merge'` and reason mentioning Node A's ID.
- `TestMergeDeletesSelfLoops`: Create nodes A and B with an edge A→B. Merge B into A. Assert no self-loop edge A→A exists afterwards.

### Implementation:
- **DB (`db/db.go`)**: Add `MergeNodes(targetId, sourceId string) error`.
  *Transaction required:*
  1. `DELETE FROM edges WHERE (from_node = sourceId AND to_node = targetId) OR (from_node = targetId AND to_node = sourceId)` — remove any direct edge between them first to prevent self-loops.
  2. `UPDATE edges SET from_node = targetId WHERE from_node = sourceId`
  3. `UPDATE edges SET to_node = targetId WHERE to_node = sourceId`
  4. Soft-archive `sourceId`.
  5. Insert into `audit_log` (action='merge').
- **Tool (`tools/tools.go`)**: Add `merge`
  *Description Guidance:* "Merge a duplicate source node into a target node. All connections from the source are moved to the target, and the source is archived. Ask explicitly for user confirmation before executing a merge, presenting both nodes clearly."

---

## 4. `export_mermaid` (Visualisation)
**Goal:** Allow agents to draw the knowledge graph for the user.

### Tests (`tools/tools_test.go`):
- `TestVisualiseMermaidSyntax`: Add a small graph (3 nodes, 2 edges). Call `visualise`. Assert response contains `flowchart TD`, node IDs with labels (`id["Label"]`), and typed edges (`id1 -- "relationship" --> id2`).

### Implementation:
- **DB (`db/db.go`)**: Add `GetDomainGraph(domain string) ([]Node, []Edge, error)`. Fetch all live nodes in the domain, then fetch all edges where `from_node IN (node_ids) OR to_node IN (node_ids)` — **OR not AND**, to capture edges that connect into nodes outside the result set.
- **Tool (`tools/tools.go`)**: Build the Mermaid string in the handler (no separate DB method needed). Add `visualise`
- **Tool (`tools/tools.go`)**: Add `visualise`
  *Description Guidance:* "Generate a Mermaid.js structural diagram of the domain. When responding to the user, strictly output the returned Mermaid string inside a Markdown ` ```mermaid ` code block without additional conversational preamble."

