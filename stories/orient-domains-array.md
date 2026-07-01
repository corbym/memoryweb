# orient: domains array — multi-domain full orient in one call

**Status:** OPEN

**Shared-surface nodes:**
- `cross-cutting-gap-orient-has-no-multi-domain-full-orient-path-agents-must-call-orient-once-per-domain-23640699`
- `spec-orient-domains-array-full-orient-parameter-and-response-shape-79835d5c`

**Recordari backlog:** STORY-155 (filed 2026-06-28). No sequencing dependency — memoryweb and Recordari implement independently.

**Related:** `stories/orient-optional-domain.md` (COMPLETE — STORY-069 bootstrap path)

---

## Why

Three orient modes exist today:

1. **No domain** — STORY-069 bootstrap: lightweight `{domains: [{domain, total_nodes, recent}]}`.
2. **Single `domain`** — full bounded orient (rules, declared_spine, significant/relevant, recent, etc.).
3. **`domains` array** — currently rejected (once strict decode lands) or silently ignored (today).

Cross-product sessions require full orient on `memoryweb-shared-surface` plus a product
domain. Agents naturally pass `domains: ["memoryweb-shared-surface", "recordari"]` and
either get an error or wrong behaviour. Workaround is sequential calls — extra latency,
easy to skip the second domain.

---

## Parameter contract

Optional `domains` — JSON array of 1–5 domain name strings (aliases resolved). Mutually
exclusive with `domain` (string) and with the no-arg bootstrap path.

- Length 1: behaviour equals single-domain orient (same top-level response shape).
- Length 2–5: multi-domain path.
- Optional `topic` applies to every domain (STORY-108 semantics).

**Validation:**
- Reject when both `domain` and `domains` supplied.
- Reject empty `domains` array.
- Reject length > 5 (cite rule-precedence convention from onboarding template design).
- Unknown domain names: prefer empty sections over error (match single-domain behaviour).

**Tool description** must document three paths:
- no domain → bootstrap (STORY-069)
- `domain` → single full orient
- `domains` → multi full orient

Remove "call orient once per domain" steer text.

---

## Response shape

**Length ≥ 2:**

```json
{
  "orientations": [
    {
      "domain": "...",
      "rules": [...],
      "declared_spine": [...],
      "significant": [...],
      "recent": [...],
      "total_nodes": N,
      "stale_count": N,
      "workspace_stats": {...}
    }
  ],
  "server_version": "..."
}
```

Order matches input `domains` order. Each entry uses same section caps and lean format
as single-domain orient today.

**Length 1:** identical to current single-domain orient (top-level fields, no wrapper).

Must not collide with STORY-069 bootstrap shape on the `domains` response key.

---

## Acceptance criteria

- `orient(domains=["a","b"])` returns `orientations` array with two full entries.
- `orient(domains=["a"])` returns same shape as `orient(domain="a")`.
- `orient(domain="a", domains=["b"])` returns validation error.
- `orient(domains=[])` returns validation error.
- `orient(domains=[...6 items...])` returns validation error citing max-5 cap.
- `orient(domains=["a","b"], topic="X")` applies topic weighting per domain.
- `go test ./...` green.

---

## Files (expected)

- `tools/orient.go` — multi-domain routing, validation, response assembly
- `tools/orient_test.go` — success and rejection paths
- `tools/definitions.go` — schema + description updates

---

## References

- Precedent: `stories/orient-optional-domain.md`
- Recordari: STORY-155, `story-155-filed-orient-domains-array-multi-domain-full-orient-in-one-call-2cba04f7`
- Standing workflow: `how-to-use-the-shared-surface-dea47fa6`
