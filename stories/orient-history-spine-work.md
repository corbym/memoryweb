# Story: Orient history spine enhancement

**Created:** 2026-05-19  
**Domain context:** memoryweb-shared-surface, memoryweb, recordari  
**Shared reference:** orient should include declared decision spine (history important_only=true)

---

## Background

Session-start context currently answers two questions well:
1. What exists now? (orient node list)
2. What changed recently? (orient recent section)

It does not answer a third critical question directly: what key decisions shaped this domain over time. The shared-surface contract calls for orient to add a declared decision spine, equivalent to history with important_only=true, so agents can start with state + recency + significance in one call.

This is an additive change to orient and should remain lightweight. Structural importance and divergence analysis are intentionally separate and belong to the future significance tool.

---

## Goal

Add a decision-spine section to orient responses, sourced from the existing history logic in important_only mode, ordered chronologically.

---

## Scope

In scope:
1. Extend orient output shape to include a declared_spine section.
2. Reuse timeline/history query behavior so ordering and live-node filtering remain consistent.
3. Add tests for empty and populated spine behavior.
4. Update orient tool description to document the new section and intended usage.

Out of scope:
1. Structural centrality calculations.
2. Divergence detection between declared and structural importance.
3. Any tool consolidation work.

---

## Implementation plan

1. Define orient response extension
- Add a new field in orient response payload, e.g. declared_spine.
- Keep existing keys unchanged for backward compatibility.

2. Reuse existing history semantics
- Pull nodes where occurred_at is set (important_only behavior).
- Order chronologically by effective date expression already used by history.
- Preserve live-only behavior (archived entries excluded).

3. Update tool description
- Explain that orient now includes current state, recent activity, and declared decision spine.
- Keep wording concise so orient remains discoverable and not overloaded.

4. Add tests
- Unit/integration tests for:
  - domain with no occurred_at entries (empty declared_spine)
  - domain with mixed nodes (only occurred_at entries appear in declared_spine)
  - chronological ordering in declared_spine
  - archived occurred_at entries excluded

5. Validate
- Run go test ./...

---

## Acceptance criteria

1. Calling orient returns the existing sections plus a new declared_spine section.
2. declared_spine contains only live entries with occurred_at set.
3. declared_spine ordering is chronological and consistent with history important_only behavior.
4. Existing orient consumers remain compatible (no breaking field removals/renames).
5. Test coverage includes empty, mixed, and archived edge cases.

---

## Risks and notes

1. Response-size growth if a domain has a very large spine.
2. Description bloat can reduce discoverability; keep wording tight.
3. Cross-project parity is required because this is a shared-surface contract.

---

## Next slice after this story

Use this as the base for the separate significance tool story, which will add structural importance and divergence analysis without overloading orient.
