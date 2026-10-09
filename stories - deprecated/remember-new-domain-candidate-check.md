# remember: filing-time candidate check on new-domain creation

**Status:** COMPLETE

**Shared-surface node:** `extend-filing-time-candidate-check-story-145-148-to-fire-on-new-domain-creation-in-remember-a38efa36`

**Depends on:** `stories/remember-candidate-similarity-floor.md` (COMPLETE — similarity floor + domain affinity infra)

**Layer 1 (cheapest):** `stories/remember-new-domain-warning.md` — ship first

---

## Why

When `remember()` targets a domain that doesn't exist yet, the agent may be
prematurely generalising (oauth-domain incident). Search-first and consequence warnings
are necessary but not sufficient — this layer adds **active detection** at filing time
by reusing existing embedding KNN infrastructure.

---

## Contract

At domain resolution in `remember()`:

1. If the requested domain **already exists** → existing candidate-surfacing path
   (unchanged).
2. If the domain **does not exist yet** → run workspace-wide KNN (same embedding
   infra as filing-time candidates):
   - Apply STORY-145 **similarity floor only** — NOT domain-affinity reranking (no
     "same domain" to boost when target domain is new).
   - Simpler than the standard candidate check.

### Response (non-blocking)

Mirror contradiction-flagging pattern:

```json
{
  "possible_misdomain": true,
  "suggested_domain": "existing-domain-name",
  "suggested_memory_id": "anchor-id-xxxxxxxx",
  ...
}
```

Agent may refile into suggested domain or proceed if genuinely warranted.

### Audit

Write `domain_creation_flagged` event to `audit_log` regardless of agent action —
enables later sweeps for ignored flags.

---

## Acceptance criteria

- New domain creation triggers KNN check against workspace.
- Similarity floor applied; no domain-affinity reranking on this path.
- Response includes `possible_misdomain` + top candidate domain/id when triggered.
- Filing succeeds even when flag present (non-blocking).
- `audit_log` row written on flagged new-domain creation.
- Tests in `tools/remember_test.go`; `go test ./...` green.

---

## Files (expected)

- `tools/remember.go` — domain-exists check + KNN branch
- `db/embeddings.go` or `db/search.go` — reuse KNN query
- `db/audit.go` — new audit action variant if needed
- `tools/definitions.go` — document response fields

---

## References

- Shared-surface node: `extend-filing-time-candidate-check-story-145-148-to-fire-on-new-domain-creation-in-remember-a38efa36`
- Prerequisite infra: `stories/remember-candidate-similarity-floor.md`
- Cheaper layer: `stories/remember-new-domain-warning.md`
