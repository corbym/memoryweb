# orient: optional domain arg

**Shared-surface nodes:** `orient-optional-domain-arg-cross-4ba5f377` (memoryweb-meta),
`orient-optional-domain-arg-cross-1b54474c` (memoryweb-shared-surface)

**Status:** COMPLETE (implemented 2026-05-28)

---

## Why

`orient` currently requires a `domain` argument. At session start, the user has no context
to know which domain to orient in. The bootstrapping problem: the tool that would tell
you where to go requires you to already know where to go.

The fix is to make `domain` optional. When called with no domain, `orient` uses the
existing `recent_changes(group_by_domain=true)` path internally and returns headlines
from the most recently active domains — enough signal to let the agent or user choose
where to properly orient.

This is not a full three-section orient per domain. It is a lightweight cross-domain
snapshot: where was work happening? That single question is what the user needs to
bootstrap.

---

## Behaviour

**With domain (existing behaviour, unchanged):**
Returns the full three-section response: `declared_spine`, `significant`, `recent`.

**Without domain (new):**
Returns a single `domains` array, each entry being a recent domain with its top few
recent nodes. Shape:

```json
{
  "mode": "cross_domain_snapshot",
  "domains": [
    {
      "domain": "memoryweb-meta",
      "recent": [ { "id": "...", "label": "...", "updated_at": "..." }, ... ]
    },
    ...
  ]
}
```

Reuses `RecentChanges(group_by_domain=true, limit=5)` — no new DB method needed.

---

## Changes

### 1. Schema — make `domain` optional

In `tools/tools.go`, in the `orient` `InputSchema`:
- Remove `"domain"` from `Required`.
- Keep the `domain` property description, adding: "Optional — omit for a cross-domain
  snapshot to find where work was last happening."

### 2. Handler — dispatch on domain presence

In `handleOrient`:

```go
if domain == "" {
    return h.orientCrossDomain()
}
// existing full orient logic
```

### 3. `orientCrossDomain` helper

Calls `h.store.RecentChanges("", true, 5)` (group_by_domain, 5 per domain). Assembles
the `mode: "cross_domain_snapshot"` response.

### 4. Description update

Add to the orient description:
> "Omit domain for a cross-domain snapshot showing where work was last happening — use
> the result to pick a domain and then call orient with that domain."

---

## Acceptance criteria

- `orient` with a domain returns the existing three-section response (no regression).
- `orient` with no domain returns a `mode: "cross_domain_snapshot"` response with a
  `domains` array.
- Each domain entry includes at least the domain name and up to 5 recent node labels.
- `TestOrient_NoDomain_ReturnsCrossDomainSnapshot` covers the no-domain path.
- `TestOrient_WithDomain_Unchanged` confirms existing behaviour is not regressed.
- AGENTS.md guidance ("call orient for the relevant domain before using any other
  context") is explicitly out of scope — that is for agents to manage, not a repo concern.
