# connect: align contradiction resolution with Recordari's `resolved` relationship name

**Status:** COMPLETE — P0 fix shipped 2026-07-09

**Shipped 2026-07-09:**
- `connect`'s `relationship` enum now includes `resolved`, `resolved_by`,
  `supersedes` (`tools/definitions.go`) — this was the actual live-breaking bug.
- `db/audit.go`'s stale-mode and conflicts-mode suppression queries now recognise
  `resolved` alongside `resolved_by`/`supersedes`.
- `audit`'s tool description no longer instructs disconnecting the `contradicts`
  edge; instructs `connect(relationship=resolved, ...)` instead.
- TDD: `TestListTools_ConnectRelationshipEnumIncludesResolutionTypes`,
  `TestFindDrift_ContradictsPair_ExcludedWhenResolved`,
  `TestFindConflictCandidates_ExcludesResolvedRelationshipPairs`,
  `TestAudit_Stale_ContradictsPair_NotFlaggedAfterResolvedRelationship`, and a
  tightened `TestAudit_DescriptionMentionsResolutionWorkflow` — all written first,
  confirmed failing, then green after the fix. `go test ./...` green.

**Deliberately out of scope, split to its own story:** the optional `verdict`
field (`false_positive`/`reconciled`/`superseded`) for full parity with
Recordari's `connect(relationship=resolved, verdict=...)` shape. See
`stories/connect-resolved-verdict-field.md` — non-blocking follow-up now that the
P0 unblock has shipped.

**Shared-surface node:** `finding-connect-audit-descriptions-don-t-document-resolved-by-supersedes-audit-s-own-description-tells-agents-to-disconnect-the-contradicts-edge-instead-7ff03718`

**Found while:** cross-checking memoryweb against the recordari skill's
`connect(relationship=resolved, verdict=...)` contradiction-resolution convention

**Severity: CONFIRMED LIVE — not a docs gap, a shipped feature is currently
unreachable.** A memoryweb v1.38.0 agent called `connect(relationship="resolved")`
and got an explicit rejection ("enum resolved isn't available"). Root-caused below:
this blocks `resolved_by` and `supersedes` too — memoryweb's own "COMPLETE"
contradiction-resolution mechanism (`stories/audit-contradiction-resolution.md`) is
not callable through the declared tool schema by any real agent host that enforces
enum constraints client-side, which the live report confirms is happening in
practice. Tests pass because they call `CallTool` directly, bypassing schema
enforcement entirely.

---

## Why

memoryweb already implements contradiction resolution — `db/audit.go`'s stale-mode
query (line ~263) excludes a `contradicts` pair from re-flagging only when a
`resolved_by` or `supersedes` edge exists directly between the two nodes (checked
both directions). This is Option A from `stories/audit-contradiction-resolution.md`
(COMPLETE): pure-additive, the original `contradicts` edge stays on the record, and
the resolution is visible in `recall`/`why_connected`.

Recordari independently arrived at the same topology (direct pair-to-pair edge,
pure-additive — memoryweb's shape was explicitly cited as prior art in Recordari's
own design decision) but settled on a **different relationship name**:
`connect(relationship=resolved, verdict=false_positive|reconciled|superseded)` —
one relationship type, an optional `verdict` field carrying the outcome nuance
(shipped as STORY-162, 2026-07-02). This is the name taught in the shared recordari
skill document's Layer 2 relationship-types table, which is used verbatim by agents
operating on **either** product.

**The divergence causes a silent failure, not just an inconsistency.** `connect`'s
`relationship` field is not runtime-validated against an enum (`db/edges.go`'s
`AddEdge` accepts any string). So a skill-compliant agent calling
`connect(relationship=resolved)` against memoryweb today:

1. Succeeds — the edge is created, no error.
2. Does nothing useful — memoryweb's stale-suppression query only checks
   `relationship IN ('resolved_by', 'supersedes')`. It does not recognise
   `'resolved'`.
3. Leaves the contradiction re-flagging in `audit(mode=stale)` forever, with no
   signal to the agent that its resolution attempt didn't take.

An agent following the skill exactly as written gets silently wrong behaviour on
memoryweb specifically. This is worse than the original framing of this gap
(undocumented relationship types) — it's an active trap for the one document meant
to keep both products' agent behaviour consistent.

**Confirmed live, 2026-07-09 — and worse than hypothesised.** A memoryweb v1.38.0
agent called `connect(relationship="resolved")` and got an explicit rejection
("enum resolved isn't available"), not a silent no-op. Root cause: `connect`'s
`relationship` property in `tools/definitions.go` declares an actual JSON-schema
`Enum` array containing only the 9 documented types — `resolved`, `resolved_by`, and
`supersedes` are **all three** absent from it. memoryweb's own server performs zero
enum validation (`tools/connect.go` passes the string straight to `db.AddEdge`; no
JSON-schema-validation library is even a dependency — checked `go.mod`), so the
rejection is happening client-side: the calling agent host validates tool-call
arguments against the declared schema before ever dispatching to memoryweb.
Recordari's `connect` tool, by contrast, declares `relationship` as a bare string
with no enum (`internal/mcp/tools.go`) — prose-only guidance, so `resolved` passes
straight through there.

**This means memoryweb's own shipped `resolved_by`/`supersedes` mechanism is
currently unreachable too**, for any schema-respecting client — which the live
report confirms is not a hypothetical. `stories/audit-contradiction-resolution.md`
is marked COMPLETE and its tests pass, but those tests call `CallTool` directly,
bypassing the schema layer the way a real MCP client does not. In production, an
agent cannot resolve a contradiction in memoryweb at all right now. **Adding the
values to the `Enum` array is the P0 fix** — everything else in this story
(description wording, `verdict` field) is valuable but secondary to unblocking the
enum.

Separately, `audit`'s own tool description currently says the opposite of what's
implemented: *"After resolving a contradiction, disconnect the contradicts edge to
retire it from future conflict surfacing."* That's Option C from
`audit-contradiction-resolution.md` (destructive), not Option A (additive) — the one
actually built. Following it literally happens to work (the stale query requires
`relationship = 'contradicts'` to match a row at all, so removing that edge does
suppress the re-flag) but silently discards the historical record and makes the
`resolved_by`/`supersedes` exclusion logic dead code for any agent that only reads
the tool description.

---

## Recommendation

Align memoryweb's mechanism to Recordari's, rather than just documenting the
existing divergent names — cross-product parity is the point of the shared surface,
and the skill document already teaches the Recordari shape to agents on both
products.

0. ✅ **SHIPPED — P0 — add `resolved`, `resolved_by`, and `supersedes` to the
   `relationship` property's `Enum` array in `tools/definitions.go`'s `connect`
   tool schema.** Unblocked the already-shipped `resolved_by`/`supersedes`
   mechanism for real agents.
1. ✅ **SHIPPED — `resolved` recognised as a resolution relationship.**
   `db/audit.go`'s suppression queries (stale-mode, conflicts-mode) add
   `'resolved'` to their `relationship IN (...)` lists.
2. ✅ **SHIPPED — `resolved_by` and `supersedes` stay accepted, not removed.**
   Pre-existing synonyms, already exercised by the live test suite — no forced
   migration of historical data, no breaking change. `supersedes` maps naturally
   onto Recordari's `verdict=superseded` value, so it isn't purely redundant; kept
   as a distinct relationship rather than collapsed into a verdict, avoiding a
   migration on existing edges.
3. **Moved to `stories/connect-resolved-verdict-field.md`.** An optional
   `verdict` field on `connect`, honoured when `relationship=resolved`: enum
   `false_positive | reconciled | superseded`, stored on the edge, returned by
   `recall`/`why_connected` — full parity with Recordari's `connect(relationship=
   resolved, verdict=...)` shape. Split out because it's additive polish, not
   part of the P0 unblock.
4. ✅ **SHIPPED — `connect`'s description and enum** document `resolved`
   (primary) and `resolved_by`/`supersedes` (accepted synonyms) together.
5. ✅ **SHIPPED — `audit`'s description** corrected to stop telling agents to
   disconnect the `contradicts` edge; instructs `connect(relationship=resolved,
   ...)` instead.

---

## Acceptance criteria (all met — shipped 2026-07-09)

- [x] `connect(relationship=resolved)` against a `contradicts` pair suppresses that
  pair from `audit(mode=stale)` and `audit(mode=conflicts)`, exactly as
  `resolved_by`/`supersedes` already do.
- [x] Existing `resolved_by`/`supersedes` behaviour is unchanged — no regression, no
  required data migration.
- [x] `connect`'s description and `relationship` enum list `resolved` as the primary
  contradiction-resolution type, with `resolved_by`/`supersedes` noted as accepted
  synonyms.
- [x] `audit`'s mode=stale/mode=conflicts description no longer instructs
  disconnecting the `contradicts` edge; instructs
  `connect(relationship=resolved, ...)`.
- [x] `tools/archive_test.go`'s resolution-workflow description assertion tightened:
  requires `resolved`/`resolved_by` and fails if the old destructive instruction
  (`disconnect the contradicts edge to retire`) appears — mirroring the
  negative-assertion pattern `stories/orphan-warning-wording-fix.md` used for its
  own wording fix.
- [x] New tests: `connect(relationship=resolved)` end-to-end suppresses a
  `contradicts` pair in both stale and conflicts modes (parallel to the existing
  `resolved_by` tests).
- [x] `go test ./...` green.

The `verdict` field acceptance criterion moved to
`stories/connect-resolved-verdict-field.md`.

---

## Files (expected)

- `db/audit.go` — add `'resolved'` to both suppression query relationship lists
- `db/edges.go` — optional `verdict` column/param on `AddEdge`, or a narrative
  convention if a schema migration is out of scope for this story (decide at
  implementation; a migration is the cleaner match to Recordari's shape)
- `tools/connect.go`, `tools/definitions.go` — `verdict` param, `resolved` in enum
  and description, `resolved_by`/`supersedes` documented as synonyms
- `tools/definitions.go`, `tools/archive.go` — `audit` description correction
- `db/audit_test.go`, `tools/connect_test.go`, `tools/archive_test.go` — new/updated
  tests

---

## References

- Implementation this story corrects: `stories/audit-contradiction-resolution.md`
  (COMPLETE) — Option A, `db/audit.go` lines 247–270
- Recordari's canonical decision (this story aligns to it):
  `decision-contradiction-resolution-is-a-first-class-resolved-edge-not-a-label-resolve-primitive-replaces-contradicts-edge-and-closes-the-lifecycle-e12972a9`
  (STORY-162, shipped 2026-07-02) — explicitly cites memoryweb's direct-pair-edge
  shape as prior art; only the relationship name and `verdict` field diverge
- Shared-surface finding: `finding-connect-audit-descriptions-don-t-document-resolved-by-supersedes-audit-s-own-description-tells-agents-to-disconnect-the-contradicts-edge-instead-7ff03718`
- Style precedent for a wording fix with negative+positive test assertions:
  `stories/orphan-warning-wording-fix.md`
