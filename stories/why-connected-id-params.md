# why_connected: from_id/to_id exact-match pair verification

**Status:** OPEN

**Shared-surface node:** `decision-why-connected-gains-explicit-from-id-to-id-params-exact-match-error-on-miss-becomes-the-recommended-pair-verification-tool-not-trace-2d31785f`

**Recordari precedent:** STORY-183 (shipped 2026-07-04 — memoryweb-side counterpart never filed)

**Related:** `stories/audit-contradiction-resolution.md` (COMPLETE), `stories/connect-resolved-relationship-discoverability.md` (COMPLETE)

---

## Why

The skill document's contradiction-resolution flow tells agents to verify a pair
before calling `connect(relationship=resolved)`. Neither `trace` nor the current
`why_connected` is fit for that job:

- **trace** — multi-hop BFS; false-positive if two nodes connect only via a third
  node; direction-sensitive on direct edges. Deliberately downgraded from the
  verification role by the shared-surface decision.
- **why_connected** — asks the right question (direct edges between exactly these
  two nodes, both directions) but only accepts fuzzy `from_label`/`to_label`, resolved
  via `ILIKE '%label%' ORDER BY created_at DESC LIMIT 1` — silent most-recent
  substring match (STORY-174/175 failure class).

Recordari shipped `from_id`/`to_id` optional params (STORY-183). memoryweb still
label-only.

---

## Contract

Add optional `from_id` and `to_id` to `why_connected`:

- Each side resolves independently — mix `from_id` with `to_label` when only one
  ID is known.
- If an id param is supplied and no live node with that id exists → **error** (no
  silent fallback to label search).
- `from_label`/`to_label` unchanged for the fuzzy/concept-only case.
- Reject supplying both `from_id` and `from_label` (same for `to_*`) — explicit
  conflict, not silent preference.

`why_connected(from_id, to_id)` becomes the recommended pair-verification tool;
update `docs/memoryweb-skill.md` Layer 2 after shipping (not before).

---

## Acceptance criteria

- `why_connected(from_id=X, to_id=Y)` returns direct edges between those exact nodes
  or a clear empty result — no label fallback when ids supplied.
- Unknown id → tool error with the missing id named.
- Both id and label supplied for the same side → tool error.
- Label-only calls behave exactly as today.
- `trace` description no longer implies pair verification — chain narration only.
- Tests in `tools/graph_test.go`; `go test ./...` green.

---

## Files (expected)

- `tools/definitions.go` — schema + description
- `tools/graph.go` — handler: id vs label resolution
- `db/graph.go` — exact-id lookup helper if needed (reuse `GetNode`)
- `docs/memoryweb-skill.md` — pair-verification wording (same change set)

---

## References

- Shared-surface node: `decision-why-connected-gains-explicit-from-id-to-id-params-exact-match-error-on-miss-becomes-the-recommended-pair-verification-tool-not-trace-2d31785f`
- Recordari: STORY-183 (`story-183-shipped-why-connected-gains-from-id-to-id-exact-match-pair-verification-tdd-8c7ef08e`)
- Vestigial trace context: `stories/trace-vestigial-fate.md`
