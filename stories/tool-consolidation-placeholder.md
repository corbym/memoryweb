# Story Placeholder: Tool consolidation

**Status:** Placeholder  
**Created:** 2026-05-19  
**Updated:** 2026-05-19 (shared surface augmented)  
**Shared reference:** `tool-consolidation-reduce-28-too-5ba2a680`  
**Target count:** 28 → 22

---

## Intent

Reduce cognitive/tool-surface load while preserving capability, by merging tool families where the behavior is naturally mode-based or single-vs-batch variants.

---

## Why this is a placeholder

Consolidation is high-impact and touches schemas, handlers, descriptions, and tests broadly. It should land after orient spine (shipped v1.10.0) and any immediate quality-of-life work.

---

## Confirmed consolidation map

### Merge (saves 7)

| Merged into | Sources removed |
|---|---|
| `remember` (items: single or array) | `remember_all` |
| `revise` (items: single or array) | `revise_all` |
| `connect` (items: single or array) | `connect_all` |
| `audit(mode=stale\|orphans\|archived)` | `whats_stale`, `disconnected`, `forgotten` |
| `domains` | `list_domains`, `list_aliases` |
| `alias(action=...)` | `alias_domain`, `remove_alias`, `resolve_domain` |

### Add (net +1)

- `forget_all` — batch archive. Same governance wording as `forget` but **stronger**: agent must list all nodes to be archived and receive explicit confirmation before calling. Blast radius is larger than single forget.

### Remove (saves 1)

- `check_for_updates` → CLI subcommand only, not an agent concern.

### Keep as-is (all distinct, no consolidation)

- `recall`, `search`, `orient`, `recent`, `history` — read tools answering genuinely different questions
- `forget`, `restore`, `disconnect`, `rename_domain` — governance-weight operations, explicit is correct
- `trace`, `why_connected` — **kept separate**: `trace` is precise (node IDs, shortest path), `why_connected` is fuzzy (labels, concept matching). The ID-vs-label distinction is a genuine semantic difference worth preserving.
- `suggest_connections`, `visualise`, `significance` (new)

### Final arithmetic

`28 - 7 (merges) - 1 (removed) + 1 (forget_all) + 1 (significance) = 22`

Orient spine is an existing tool — no count change.

---

## Non-negotiable description constraints

Before writing any merged tool description, read **all** source descriptions in full and carry forward every constraint. Specific wording that must survive exactly:

1. `forget` / `forget_all`: `"only call after explicit unambiguous user confirmation — never on implication or casual mention"`
2. `remember`: the full propose+confirm occurred_at contract
3. `connect`: the relationship type enum verbatim — `caused_by`, `led_to`, `blocked_by`, `unblocks`, `connects_to`, `contradicts`, `depends_on`, `is_example_of`
4. `audit(mode=archived)`: inherits forget's governance wording — lists candidates only, never archives without confirmation

A poorly described merged tool is worse than two well-described separate ones.

---

## Open questions

1. Backward compatibility policy for existing tool names (hard cut vs deprecated aliases).
2. Whether to consolidate in one release or staged slices by tool family.

---

## Acceptance criteria skeleton

1. Net MCP tool count reaches 22 (28 - 7 + 1 + 1 - 1).
2. All previously supported capabilities remain available.
3. Governance wording listed above is preserved verbatim in merged descriptions.
4. Tests cover both singular and array/batch mode paths in merged tools.
5. `audit(mode=archived)` does not archive anything — it lists candidates only.
