# Story Placeholder: Tool consolidation

**Status:** Placeholder  
**Created:** 2026-05-19  
**Shared reference:** Tool consolidation: reduce 28 tools to ~19 without losing capability

---

## Intent

Reduce cognitive/tool-surface load while preserving capability, by merging tool families where the behavior is naturally mode-based or single-vs-batch variants.

---

## Why this is a placeholder

Consolidation is high-impact and touches schemas, handlers, descriptions, and tests broadly. It should land behind a dedicated migration/test pass after orient spine and immediate quality-of-life work.

---

## Proposed consolidation map (draft)

1. Batch variants
- remember + remember_all
- revise + revise_all
- connect + connect_all

2. Graph traversal
- trace + why_connected

3. Lifecycle audit
- whats_stale + disconnected + forgotten

4. Domain/alias management
- list_domains + list_aliases
- alias_domain + remove_alias + resolve_domain

5. Utility
- remove check_for_updates from MCP surface (CLI-only)

---

## Non-negotiable constraints

1. Preserve all governance wording in tool descriptions.
2. Preserve propose+confirm and occurred_at guidance.
3. Preserve explicit user-confirmation requirements for archiving.
4. Preserve relationship-type clarity in connect behavior.
5. Preserve existing behavior semantics; consolidation should reduce shape count, not remove capability.

---

## Open questions

1. Backward compatibility policy for existing tool names.
2. Transition strategy for clients relying on old schemas.
3. Whether to phase via aliases/deprecated names before hard removal.
4. Whether to consolidate in one release or in staged slices by tool family.

---

## Acceptance criteria skeleton

1. Net MCP tool count is reduced toward the shared-surface target.
2. All previously supported capabilities remain available.
3. Description-level constraints from merged tools are retained verbatim where required.
4. Tests cover both singular and batch mode paths in merged tools.
5. Documentation and examples reflect the consolidated surface.
