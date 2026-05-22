# Story: Tier 2 vocabulary cleanup — rename `from_node`/`to_node` and all remaining "node" references in tool schema

**Discovered:** 2026-05-22  
**Domain:** memoryweb  
**Related nodes:** `connect-parameter-names-are-from-246e0b62`, `tool-surface-cleanup-orient-disc-49306bf0`, `tool-description-review-seven-qu-d735ef72`

---

## Background

STORY-029 tier 1 renamed vocabulary in tool description *prose* — replacing "node" with "memory" and "edge" with "connection" throughout the tool description strings. It did not rename the JSON schema *parameter keys* or the *property description* sub-strings inside `Property{}` definitions.

The result is a split-brain surface:

- Tool description body says: "connect memories", "provide from_memory, to_memory"
- Schema parameter key is: `from_node`, `to_node`
- Property description says: "ID of the source **node**"

Agents construct calls from schema property names and property descriptions — not from the tool description body. An agent reading the tool description will try `from_memory`/`to_memory`, get a silent unmarshal-zero failure (the field is quietly ignored), and the edge is never created. This is the tier 2 gap confirmed in shared-surface node `connect-parameter-names-are-from-246e0b62`.

A broader audit found the same mismatch pattern across 9 other tools. Two additional sub-issues were also uncovered: a retired tool name (`whats_stale`) in a parameter description, and the blacklisted word `disconnected` in the `audit` mode parameter description.

---

## Full inventory of mismatches

### Category A — Schema parameter key names (highest severity)

These are the JSON keys agents must send. When they mismatch the vocabulary in descriptions, agents send wrong keys and fail silently.

| Tool | Current key | Correct key |
|------|-------------|-------------|
| `connect` (single mode) | `from_node` | `from_memory` |
| `connect` (single mode) | `to_node` | `to_memory` |
| `connect` (batch items inline schema) | `from_node` | `from_memory` |
| `connect` (batch items inline schema) | `to_node` | `to_memory` |

Handler struct json tags must change alongside schema keys (two structs in `handleAddEdge`):
- `tools/tools.go` line ~1035: `json:"from_node"` → `json:"from_memory"`
- `tools/tools.go` line ~1036: `json:"to_node"` → `json:"to_memory"`
- `tools/tools.go` line ~1054: `json:"from_node"` → `json:"from_memory"`
- `tools/tools.go` line ~1055: `json:"to_node"` → `json:"to_memory"`

### Category B — Property description text (medium severity)

These are the `Description` strings inside individual `Property{}` definitions. Agents read these when constructing calls.

| Tool | Location | Current | Correct |
|------|----------|---------|---------|
| `remember` | `label` description | "Short name for this **node**" | "Short name for this **memory**" |
| `remember` | `description` description | "What this **node** is about" | "What this **memory** is about" |
| `remember` | `tags` description | "find this **node** later" | "find this **memory** later" |
| `remember` | `transient` description | "Transient **nodes** older than 7 days are surfaced by **whats_stale**" | "Transient **memories** older than 7 days are surfaced by **audit(mode=stale)**" — fixes both vocabulary and retired tool ref |
| `remember` | `items` description | "array of **node** objects" | "array of **memory** objects" |
| `connect` | tool description body | "provide **from_node**, **to_node**, relationship directly" | "provide **from_memory**, **to_memory**, relationship directly" |
| `connect` | `from_node` description | "ID of the source **node**" | "ID of the source **memory**" |
| `connect` | `to_node` description | "ID of the target **node**" | "ID of the target **memory**" |
| `connect` | `items` description | "Each must have **from_node**, **to_node**" | "Each must have **from_memory**, **to_memory**" |
| `forget` | `id` description | "ID of the **node** to archive" | "ID of the **memory** to archive" |
| `forget` | `reason` description | "Why this **node** is being archived" | "Why this **memory** is being archived" |
| `restore` | `id` description | "ID of the **node** to restore" | "ID of the **memory** to restore" |
| `audit` | `mode` description | "orphans (**disconnected nodes**)" | "orphans (**isolated memories**)" — also fixes blacklisted word `disconnected` |
| `forget_all` | `items` description | "Array of **nodes** to archive" | "Array of **memories** to archive" |
| `revise` | `id` description | "ID of the **node** to update" | "ID of the **memory** to update" |
| `suggest_connections` | `id` description | "ID of the **node** to find connection candidates for" | "ID of the **memory** to find connection candidates for" |
| `trace` | `from_id` description | "ID of the starting **node**" | "ID of the starting **memory**" |
| `trace` | `to_id` description | "ID of the destination **node**" | "ID of the destination **memory**" |

### Category C — Tool description body text (lower severity)

These are in the top-level `Description` field, not in parameter definitions. Agents do read these, but mismatches here are less likely to cause silent call failures than category A/B. Fix for consistency.

| Tool | Current | Correct |
|------|---------|---------|
| `remember` description | "link the **nodes** you've just filed — **nodes** without connections lose context" | "link the **memories** you've just filed — **memories** without connections lose context" |
| `remember` description | "any **node** expected to become stale within days. Transient **nodes** are candidates for archiving" | "any **memory** expected to become stale within days. Transient **memories** are candidates for archiving" |
| `history` description | "Returns **nodes** in a domain" / "returns ALL **nodes**" / "return only **nodes** where occurred_at" | "Returns **memories** in a domain" / "returns ALL **memories**" / "return only **memories** where occurred_at" |
| `history` | `important_only` description: "return only **nodes** with occurred_at" / "return all **nodes** ordered by" | → "memories" in both places |
| `history` | `tags` description: "Only **nodes** matching at least one tag" | → "Only **memories** matching" |
| `history` | `from` description: "**nodes** whose effective date" | → "**memories** whose effective date" |
| `history` | `to` description: "**nodes** whose effective date" | → "**memories** whose effective date" |
| `audit` description | "non-transient **nodes** with zero connections" | "non-transient **memories** with zero connections" |
| `forget_all` description | "All **nodes** are archived or none" / "**nodes** can be restored at any time" | → "**memories**" in both places |

### Category D — Response field names in `orient` and `visualise` descriptions (do not change)

The `orient` description says `"nodes (all live memories)"` — here `nodes` is the *literal JSON response key*. Changing it to `memories` would misdirect agents reading the response (the field is still `nodes` in the wire format). Similarly `visualise` refers to `node_count`, `nodes_total` which are response keys.

These are **out of scope for this story**. Renaming response field names in the wire format is a separate, larger change.

---

## Piggyback fixes

Two additional issues uncovered during the audit that can be fixed in the same commit:

1. **Retired tool reference in `remember.transient`**: `whats_stale` is in the `removedTools` blocklist. The property description says "surfaced by `whats_stale`". The `TestListTools_NoStaleToolReferences` test does not scan property-level descriptions — only top-level `Description` fields. This is currently a blind spot in the test. Fix: update wording to `audit(mode=stale)` and extend the test to also scan all `Property.Description` sub-fields.

2. **Blacklisted word in `audit.mode`**: `disconnected` is a blacklisted retired tool name. The mode parameter description says "orphans (disconnected nodes)". The same test blind spot applies. Fix: change to "orphans (isolated memories)". Extend the test as above.

---

## Test requirements

### Existing tests to update

All call sites in `tools/tools_test.go` that pass `from_node`/`to_node` as argument keys must be updated to `from_memory`/`to_memory`. Affected lines (approximate): 740–741, 762–763, 774–775, 784–785, 796, 826, 895, 1712–1713. Also the inline struct definitions at lines ~2163–2164 and ~2198.

### New test: property descriptions scanned for forbidden words

Extend `TestListTools_NoStaleToolReferences` (or add a separate `TestToolProperties_NoForbiddenWords`) to scan not just `tool.Description` but also every `Property.Description` in the schema for:
- Every name in the `removedTools` blocklist (catches `whats_stale` style regressions)
- The blacklisted word `disconnected`
- The word `node` when used as a standalone noun (whole-word, case-insensitive) — this enforces the vocabulary contract at CI level going forward

The "node" check needs care: `from_node`, `to_node` are the old parameter key *names*, not prose. After the rename, no property description should contain the standalone word "node" (not "nodes" in some contexts either). A regex like `\bnode\b` (case-insensitive) would catch new regressions. Add any intentional exceptions as a named allowlist rather than disabling the check.

---

## Implementation order

1. **Rename schema keys and handler structs** (Category A) — do this first; it's the breaking change that requires the test updates to follow.
2. **Update all test call sites** from `from_node`/`to_node` → `from_memory`/`to_memory`.
3. **Fix property descriptions** (Category B) — all in one multi-replace pass.
4. **Fix tool description body text** (Category C) — second multi-replace pass.
5. **Extend `TestListTools_NoStaleToolReferences`** to scan property descriptions.
6. Run `go test ./...` — must be green before shipping.

---

## Out of scope

- Renaming `Edge.FromNode`/`Edge.ToNode` struct fields or their `json:"from_node"`/`json:"to_node"` *output* tags in `db/db.go`. The `Edge` struct is serialized into every response that returns edges — `recall`, `orient`, `trace`, `why_connected`, `suggest_connections`, `visualise`, and the connect response itself. The only cost is test churn: every test that unmarshals an edge response would need updating. The remaining asymmetry (output says `from_node`, input expects `from_memory`) is livable — agents read the connect schema to know what to send, not the field names in a recall response. File a separate story if the output asymmetry proves to cause confusion in practice.
- Any recordari changes — the equivalent audit should be run against recordari's tool surface separately and tracked in a recordari story.
