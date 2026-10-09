# revise(domain) — node-level domain reassignment

**Status:** COMPLETE — shipped v1.37.0 (commit b7a0f32)

**Shared-surface spec:** `spec-revise-domain-node-level-domain-reassignment-both-products-4ca333ed`

**Depends on:** none (standalone, post-v1.35.0)

---

## Why

There is no non-destructive path to move a mis-filed node to the correct domain.
`rename_domain` requires the target to be empty; `merge_domains` (CLI) moves
everything; forget + remember destroys the stable ID and orphans all edges,
`significance_log` rows, embeddings, and audit history keyed on it.

A single-column UPDATE is the correct repair. Domain is metadata, same class as
`label` or `node_kind`.

---

## Contract (from shared-surface spec)

- **Surface:** `revise({ id, domain, reason })` — `domain` is a new optional field
  on the existing tool; `reason` is required when `domain` is set.
- **Validation at tool layer** before any DB write: if `domain` set and `reason`
  empty → error.
- **Atomic transaction:** `UPDATE nodes SET domain=? WHERE id=? AND archived_at IS NULL`
  + `INSERT INTO audit_log` — rollback if either fails.
- **Audit record format:** `"domain (was OLD → NEW): <reason>"` — self-contained,
  both old and new domain included.
- **Preserve:** ID, edges, `node_embeddings` (keys on `node_id` not domain — free),
  `created_at`, `occurred_at`, full audit history.
- **Target domain need not pre-exist** — creating it implicitly, same as `remember`.
- **Batch:** `reason` required per item when `domain` is set. Do not disallow batch
  domain moves; enforce per-item reason.

### Agent protocol (must appear in tool description at forget-protocol strength)

1. Never set `domain` without the user explicitly naming the target domain.
2. Show current domain and proposed target before calling.
3. "That's probably in the wrong domain" is not confirmation — wait for the user to
   name the correct domain.
4. After moving, call `orient(domain=new_domain)` to confirm the node is visible.

### memoryweb governance

No role gate (solo binary). Reason required + agent protocol, consistent with the
forget protocol.

---

## Changes

### `db/nodes.go` — `UpdateNode`

Extend signature with two trailing params:

```go
func (s *Store) UpdateNode(
    id string,
    label, description, whyMatters, tags *string,
    occurredAt *time.Time,
    nodeKind *string,
    domain *string,   // NEW
    reason  *string,  // NEW — required when domain is set
) (*Node, error)
```

After the `nodeKind` block, before `args = append(args, id)`:

```go
if domain != nil && *domain != cur.Domain {
    if reason == nil || strings.TrimSpace(*reason) == "" {
        return nil, fmt.Errorf("reason is required when changing domain")
    }
    sets = append(sets, "domain = ?")
    args = append(args, *domain)
    changes = append(changes, fmt.Sprintf(
        "domain (was %s → %s): %s", cur.Domain, *domain, strings.TrimSpace(*reason),
    ))
}
```

### `db/nodes.go` — `NodeUpdateInput` / `UpdateNodesBatch`

Add `Domain *string` and `Reason *string` to `NodeUpdateInput`. Mirror the same
domain block in the batch loop (same validation, same audit format).

### `tools/revise.go` — single mode

Add to decode struct: `Domain *string`, `Reason *string`.

Before the `UpdateNode` call:

```go
if a.Domain != nil && (a.Reason == nil || strings.TrimSpace(*a.Reason) == "") {
    return errorResult("reason is required when changing domain — confirm the target domain with the user first"), nil
}
```

Pass `a.Domain, a.Reason` to `store.UpdateNode`.

### `tools/revise.go` — batch mode

Add `Domain *string`, `Reason *string` to `updateItem`. Per-item validation (same
error as single, prefixed with `"update %d: "`). Pass to `NodeUpdateInput`.

### `tools/definitions.go` — revise schema

Add `domain` and `reason` properties to single-mode properties map.
Update batch items JSON schema to include both.
Prepend domain-move protocol to the `revise` description.

---

## Acceptance criteria

- `revise({ id, domain: "new-domain", reason: "mis-filed" })` moves node; returned
  node has `domain = "new-domain"`; original domain no longer shows node.
- `revise({ id, domain: "new-domain" })` (no reason) → tool error.
- Audit log entry for a domain move contains `"domain (was X → Y): reason"`.
- Node ID, edges, and `created_at` are unchanged after domain move.
- Target domain need not pre-exist.
- Batch: per-item reason required; missing reason on any item → error, no moves applied.
- `go test ./...` green.

---

## Non-goals

- Fuzzy duplicate detection on move
- Auto-rewiring edges when node crosses domains
- Bulk domain split
- Replacing `merge_domains` / future duplicate-merge tooling
