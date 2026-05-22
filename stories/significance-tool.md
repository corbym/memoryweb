# Story: significance tool

**Source:** `significance-tool-dual-signal-im-84b471ff` (memoryweb-shared-surface)  
**Target count:** 20 current tools → 21 with significance  
**Spec confirmed:** 2026-05-19

---

## What it does

`significance(domain, limit=10, recency_window=90)` returns four sections:

1. **declared** — nodes with `occurred_at` set, chronological. Same output as `history(important_only=true)`.
2. **structural** — nodes ranked by weighted inbound degree. Decay formula:  
   `SUM(1.0 / (1 + days_since_linker_updated))` per linking node. Dormant linkers contribute near-zero.
3. **uncurated** — nodes in structural top-N that have no `occurred_at`. Curation candidates the human hasn't promoted yet.
4. **potentially_stale** — nodes with `occurred_at` but low structural score. Declared important but nothing current depends on them.

The divergence between `uncurated` and `potentially_stale` is the most actionable output.

---

## Tasks

### 1. Migration v11 — `significance_log` table

Add a new migration (append-only, never edit existing):

```sql
CREATE TABLE IF NOT EXISTS significance_log (
    id          TEXT PRIMARY KEY,
    called_at   DATETIME NOT NULL,
    domain      TEXT NOT NULL,
    limit_n     INTEGER NOT NULL,
    node_id     TEXT NOT NULL,
    node_label  TEXT NOT NULL,
    rank_type   TEXT NOT NULL,   -- 'structural' | 'potentially_stale' | 'uncurated'
    score       REAL             -- importance_score for structural entries, NULL for derived sets
);
CREATE INDEX IF NOT EXISTS idx_significance_log_domain ON significance_log(domain);
CREATE INDEX IF NOT EXISTS idx_significance_log_node ON significance_log(node_id);
```

**Why a separate table, not `audit_log`:** significance calls are read operations, not mutations. Mixing them into audit_log (which tracks writes) would pollute provenance queries. The log is needed to validate whether the decay function's structural rankings subsequently get confirmed as significant (`occurred_at` set) or stale (archived).

### 2. DB layer — `GetSignificance` in `db/db.go`

```go
type SignificanceResult struct {
    Declared        []Node
    Structural      []ScoredNode
    Uncurated       []ScoredNode   // in structural top-N, no occurred_at
    PotentiallyStale []Node        // has occurred_at, low structural score
}

type ScoredNode struct {
    Node
    ImportanceScore float64 `json:"importance_score"`
}

func (s *Store) GetSignificance(domain string, limit int, recencyWindowDays int) (SignificanceResult, error)
```

**Structural query (SQLite-compatible — no `extract`, use julianday):**

```sql
SELECT n.id, n.label, n.description, n.why_matters, n.tags, n.domain,
       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.transient,
       SUM(1.0 / (1.0 + (julianday('now') - julianday(n2.updated_at)))) AS importance_score
FROM edges e
JOIN nodes n  ON e.to_node   = n.id
JOIN nodes n2 ON e.from_node = n2.id
WHERE n.domain = ?
  AND n.archived_at IS NULL
  AND n2.archived_at IS NULL
  AND (julianday('now') - julianday(n2.updated_at)) <= ?
GROUP BY n.id
ORDER BY importance_score DESC
LIMIT ?
```

Params: `domain`, `recencyWindowDays`, `limit`.

**Declared:** `SELECT ... FROM nodes WHERE domain = ? AND occurred_at IS NOT NULL AND archived_at IS NULL ORDER BY occurred_at ASC`

**Uncurated:** structural top-N nodes where `occurred_at IS NULL`.

**Potentially stale:** nodes in declared set whose `id` does NOT appear in structural top-N (i.e. declared important but graph-structurally irrelevant).

**Logging:** after computing results, insert one row into `significance_log` per returned node (structural, uncurated, potentially_stale). Use a shared `call_id` (shortID) per invocation so a full call can be reconstructed.

### 3. Tool handler — `handleSignificance` in `tools/tools.go`

**Tool definition:**

```go
Name: "significance",
Description: `Dual-signal importance analysis for a domain. Returns four sections:
- declared: nodes explicitly marked as significant (occurred_at set), in chronological order.
- structural: nodes ranked by weighted inbound degree — SUM(1 / (1 + days_since_linker_updated)). High score means many recent active nodes depend on this node right now.
- uncurated: nodes in structural top-N with no occurred_at — significance candidates you haven't curated yet.
- potentially_stale: nodes with occurred_at but low structural score — declared important but nothing current depends on them anymore.

The gap between uncurated and potentially_stale is the most actionable output: use it to promote missed decisions onto the timeline and archive claims that no longer hold.

Do not use this tool to list all nodes chronologically — use history for that. For age-based and disconnected staleness, use audit(mode=stale). significance and audit are complementary: significance catches importance-based staleness; audit catches age-based staleness. A full domain health check runs both.

This tool only returns live nodes. Archived nodes are hidden.`,
```

**Input schema:**

```json
{
  "domain": "string (required)",
  "limit": "integer (optional, default 10) — top-N for structural ranking",
  "recency_window": "integer (optional, default 90) — days; linkers older than this contribute zero weight"
}
```

**Response shape:**

```json
{
  "declared": [...],
  "structural": [{"id": "...", "label": "...", "importance_score": 0.94, ...}],
  "uncurated": [...],
  "potentially_stale": [...],
  "call_id": "a3f9b2c1"
}
```

### 4. Update `history` tool description

Add to the history description: `"For importance analysis beyond the timeline — which nodes are structurally load-bearing right now — use significance."`

### 5. Tests

**`db/db_test.go` — `TestGetSignificance_*`:**

- `TestGetSignificance_Empty` — empty domain returns four empty slices, no error.
- `TestGetSignificance_Declared` — nodes with `occurred_at` appear in declared, ordered chronologically.
- `TestGetSignificance_Structural` — node with many recent inbound edges ranks above node with few. Verify score > 0.
- `TestGetSignificance_RecencyWindow` — linker updated > `recency_window` days ago contributes zero weight (node absent from structural).
- `TestGetSignificance_Uncurated` — node in structural top-N without `occurred_at` appears in uncurated.
- `TestGetSignificance_PotentiallyStale` — node with `occurred_at` and no inbound edges appears in potentially_stale, not in structural.
- `TestGetSignificance_Logging` — after call, `significance_log` rows exist for the returned nodes with correct `rank_type` and shared `call_id`.
- `TestGetSignificance_ArchivedExcluded` — archived nodes do not appear in any section.

**`tools/tools_test.go` — `TestSignificance_*`:**

- `TestSignificance_ReturnsAllFourSections` — response JSON has `declared`, `structural`, `uncurated`, `potentially_stale` keys (may be empty arrays).
- `TestSignificance_IsErrorOnMissingDomain` — calling without `domain` returns `IsError: true`.
- `TestSignificance_StructuralRankingCorrect` — build two nodes, more-connected one ranks higher.
- `TestSignificance_PotentiallyStaleDetected` — node with `occurred_at` and zero inbound edges appears in `potentially_stale`.
- `TestSignificance_DefaultsApplied` — omitting `limit` and `recency_window` uses 10 and 90.

---

## Acceptance criteria

- [ ] `significance(domain)` returns JSON with keys `declared`, `structural`, `uncurated`, `potentially_stale`, `call_id`.
- [ ] `declared` is ordered by `occurred_at` ASC; all entries have `occurred_at` set.
- [ ] `structural` entries have `importance_score > 0`; ordered descending by score.
- [ ] A node in `structural` with no `occurred_at` also appears in `uncurated` and NOT in `declared`.
- [ ] A node in `declared` with no inbound edges (or only stale linkers) appears in `potentially_stale` and NOT in `structural`.
- [ ] Archived nodes never appear in any section.
- [ ] Every call writes rows to `significance_log`; rows for the same call share a `call_id`.
- [ ] Linkers updated more than `recency_window` days ago contribute zero weight; those nodes are absent from `structural`.
- [ ] `limit` caps the `structural` list; `uncurated` is derived from that capped list.
- [ ] `history` description includes a forward-reference to `significance`.
- [ ] `go test ./...` passes.

---

## Out of scope

- UI for confirming `uncurated` candidates as significant (future story).
- `user-confirmed` vs `agent-assigned` provenance on `occurred_at` (separate shared-surface story `audit-provenance-marker-for-agen-27f10f5c`).
- Tuning the decay function — the log is the mechanism for that, not a task here.
