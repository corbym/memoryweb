# Skill audit protocol — separate orphan/stale passes, silent unless problems

**Status:** COMPLETE

**Shared-surface node:** `skill-audit-protocol-orphans-auto-resolved-by-default-contradictions-escalated-to-user-report-only-on-problems-b5766b39`

**Recordari precedent:** recordari-skill.md v3 (2026-07-01)

---

## Why

Live agent sessions against `docs/memoryweb-skill.md` showed two failure modes:

1. **Orphan and stale audits conflated** — a genuine contradiction buried next to
   routine orphan busywork in one combined report.
2. **Unconditional status lines** — "orphans checked: yes/no, stale checked: yes/no"
   on every session, training users to skim past the line when a real contradiction
   needs attention.

Recordari skill v3 implements the fix. memoryweb-skill.md has partial overlap (audit
modes documented) but not the full protocol: separate passes, orphan auto-resolution
default, contradiction escalation rules, silent-unless-problems reporting.

---

## Protocol (from shared-surface standing rule)

### Two separate steps — never one combined pass or report

**Orphans (`audit(mode=orphans)`):**
- Agent resolves every orphan itself by default (`suggest_connections` + `connect`).
- Stop and ask the user only when the correct connection is genuinely ambiguous
  (multiple equally plausible targets, or none).

**Stale/contradictions (`audit(mode=stale)` + `audit(mode=conflicts)`):**
- Duplicates and superseded labels → agent fixes directly (`revise`), don't ask.
- Genuine `contradicts` edge → **not** the agent's call alone; present both claims
  to the user and wait before touching either node.

### RESOLVED-marker check before escalating

Before escalating a `contradicts` pair, check whether either side already carries a
`RESOLVED <date>` label prefix. If so, treat as closed and stay silent. (Edge-level
retirement shipped in `audit-contradiction-resolution.md`; label check remains a
defence-in-depth habit.)

### Silent by default

If both audits come back clean (or every stale hit is already RESOLVED), say **nothing**
about audits. Only speak up when there is an unresolved orphan or a live, unmarked
contradiction awaiting the user's decision.

---

## Change

Update `docs/memoryweb-skill.md` (Layer 1 workflow + Layer 2 audit section):

- Add explicit two-step audit workflow at session end / maintenance.
- Orphan auto-resolution default with ambiguity escape hatch.
- Contradiction escalation rules (user confirmation required).
- RESOLVED-marker check before re-escalating.
- Remove or forbid routine audit status-line reporting.

No tool schema changes — skill/document only.

---

## Acceptance criteria

- Skill Layer 1 states orphan and stale/contradiction audits as separate steps.
- Orphan auto-resolution-by-default is explicit; ambiguity escape hatch documented.
- Contradiction escalation requires user confirmation; duplicates/superseded labels
  are agent-fixable without asking.
- RESOLVED-marker check documented.
- Silent-unless-problems reporting rule is explicit.
- Cross-check against Recordari skill v3 for parity on protocol semantics (wording
  may differ for single-tenant vs multi-tenant).

---

## Files (expected)

- `docs/memoryweb-skill.md`

---

## References

- Shared-surface node: `skill-audit-protocol-orphans-auto-resolved-by-default-contradictions-escalated-to-user-report-only-on-problems-b5766b39`
- Depends on (workaround until edge retirement): `issue-resolving-a-contradicts-edge-doesn-t-clear-it-same-pair-re-flags-in-audit-mode-stale-forever-a87db2d5` — COMPLETE in `stories/audit-contradiction-resolution.md`
