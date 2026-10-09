# remember: imperative new-domain creation consequence warning

**Status:** COMPLETE

**Shared-surface node:** `add-imperative-new-domain-creation-warning-to-remember-s-tool-description-21511d21`

**Distinct from:** `remember-tool-search-first-domai-d560de18` (shipped 2026-05-27 — HOW to find domain; this is WHY getting it wrong matters)

---

## Why

The oauth-domain incident (2026-06-28) happened despite search-first domain inference
already live in `remember`'s description. Heavy domain vocabulary (DCR, CIMD, PKCE, RFC
numbers) returned weak/generic matches rather than nothing — the agent searched but
didn't recognize the weak match as the same topic, then created a premature new domain.

Search-first tells agents **how** to pick a domain. This adds **why** creating a new
domain is costly: the memory becomes invisible to every other domain's scoped `orient()`
and domain-filtered `search()` — a loss-aversion lever layered on top of the existing
how-to instruction.

Pure tool-description text. No schema or logic change. Cheapest of three defense-in-depth
layers (warning → orient flag → candidate check).

---

## Change

### `tools/definitions.go` — `remember` description

Add one imperative sentence near the domain-selection guidance (after search-first /
prefer-existing-domains text):

> Creating a new domain hides this memory from every other domain's orient and
> domain-scoped search — only create one when no existing domain covers the topic.

Keep imperative voice; no structural vocabulary.

---

## Acceptance criteria

- `remember` description states the discoverability consequence of new-domain creation.
- Wording is distinct from search-first HOW guidance (not a restatement).
- Batch-mode `items` property description carries a terse version if space allows.
- `TestListTools_*` description tests pass; `go test ./...` green.

---

## Files (expected)

- `tools/definitions.go`

---

## References

- Shared-surface node: `add-imperative-new-domain-creation-warning-to-remember-s-tool-description-21511d21`
- Layer 2 (orient): `add-lightweight-related-domains-flag-to-orient-output-pointer-not-materialized-nodes-fca25bf0`
- Layer 3 (logic): `stories/remember-new-domain-candidate-check.md`
- Incident: `story-075-spike-outcome-cancel-no-recordari-oauth-client-registration-code-needed-supabase-hosts-dcr-d4c284cc`
