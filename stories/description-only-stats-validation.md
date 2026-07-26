# deferred stats validation for description-only story batches

**Status:** OPEN

**memoryweb-meta node:** `description-only-stories-need-de-eaac1440`

---

## Why

Description and skill changes can ship with green tests but **silently fail to change agent
behaviour** — the only signal is production stats (orphan rate, orient call patterns,
connect-after-remember compliance). Wave 3 Phase 1 was a description batch; orphan nudge
effectiveness was measured once (Apr 30–Jun 26 logs) but post-v1.39.0 re-baseline was
deferred (`decision-hold-off-on-auto-connect-re-baseline-orphan-rate-after-v1-38-2-s-orphan-warning-bug-fix-before-judging-the-nudge-a-failure-ffd37ac9`).

This is an **ops/measurement** story, not a code feature — unless stats tooling gaps block it.

---

## Scope

After each description-only release batch, run a checklist against `memoryweb-stats` JSONL:

| Metric | Phase 1 batch hypothesis |
|--------|--------------------------|
| Orphan rate (session + node level) | ↓ after finding-linkback + connect imperative |
| `connect` calls per `remember` | ↑ |
| `audit(mode=conflicts)` after filing | ↑ (conflict framing) |
| New-domain creation rate | ↓ or misdomain warnings visible |

Document baseline date + release version + window length. File finding in memoryweb-meta;
revise description if metric flat after 3 weeks.

---

## Deliverables

1. Script or doc section in README/stats — which JSONL fields to query
2. Post-v1.39.0 baseline run (overdue)
3. Standing reminder in release process node or this story's checklist

---

## Out of scope

- Auto-connect (deferred intentionally)
- Changing tool code unless validation proves description defect

---

## References

- Stats: `stats/stats.go`, `MEMORYWEB_STATS_FILE`
- Orphan baseline: `orphan-rate-baseline-28-recent-s-2faef552`
