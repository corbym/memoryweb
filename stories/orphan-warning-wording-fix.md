# Orphan Warning Wording Fix — cross-domain connect misread as unsupported

**Status:** COMPLETE — shipped v1.29.3 (remember.go orphan_warning wording + tests)

**Shared-surface node:** `correction-orphan-warning-wordin-30e8b461`

---

## Why

The `orphan_warning` text returned by `remember` (both single and batch) contains
a sentence that reads:

> "Suggested connections in other domains cannot be connected directly — check
> their domain field first."

Observed on 2026-06-19: an agent read this as a capability limitation and concluded
that cross-domain `connect()` calls are not supported. They are supported — `connect`
works across domains when the caller passes the correct `domain` for each side.

The data gap (missing `domain` field on `suggested_connections` entries) was fixed
in STORY-058 (v1.16.0, `cross-domain-connect-ux.md`). This is the next layer up:
the warning sentence's own prose still misleads even though the domain field is now
present in the suggestions payload.

---

## Change

### tools/tools.go — two call sites

**Site 1** — single-node path (line ~525, `handleAddNode`):

Current:
```go
orphanWarning = fmt.Sprintf("No connections were made. Call connect with domain=%s to link these memories. Suggested connections in other domains cannot be connected directly — check their domain field first.", node.Domain)
```

Replace the final sentence only:
```go
orphanWarning = fmt.Sprintf("No connections were made. Call connect with domain=%s to link these memories. Some suggested connections are in other domains — pass their domain explicitly when calling connect, not the current domain.", node.Domain)
```

**Site 2** — batch path (line ~1318, `handleAddNodes`):

Current:
```go
orphanWarning = "No connections were made. Call connect with domain=<domain> to link these memories. Suggested connections in other domains cannot be connected directly — check their domain field first."
```

Replace the final sentence only:
```go
orphanWarning = "No connections were made. Call connect with domain=<domain> to link these memories. Some suggested connections are in other domains — pass their domain explicitly when calling connect, not the current domain."
```

### tools/tools_test.go — tighten existing assertions

`TestRemember_OrphanWarning_PresentWhenNoConnections` and
`TestRememberAll_OrphanWarning_PresentWhenNoEdges` currently only check that
`"No connections were made"` is present. Add a negative assertion to each so the
old misleading phrase can never silently reappear:

```go
if strings.Contains(tr.Content[0].Text, "cannot be connected directly") {
    t.Error("orphan_warning must not say 'cannot be connected directly' — use usage-instruction wording instead")
}
```

Add a positive assertion that the new wording is present:

```go
if !strings.Contains(tr.Content[0].Text, "pass their domain explicitly") {
    t.Error("orphan_warning must instruct agent to pass domain explicitly for cross-domain connect")
}
```

Apply both assertions to the single-node test and the batch test.

---

## Acceptance criteria

- `TestRemember_OrphanWarning_PresentWhenNoConnections` passes and asserts:
  - `"No connections were made"` is present (existing check, keep it)
  - `"cannot be connected directly"` is absent
  - `"pass their domain explicitly"` is present
- `TestRememberAll_OrphanWarning_PresentWhenNoEdges` passes with the same three
  assertions
- `TestRemember_OrphanWarning_AbsentWhenRelatedToProvided` still passes (no change
  needed — it asserts `orphan_warning` is absent altogether)
- `go test ./...` green

---

## Files

- `tools/tools.go` — two `orphanWarning` string literals
- `tools/tools_test.go` — `TestRemember_OrphanWarning_PresentWhenNoConnections`,
  `TestRememberAll_OrphanWarning_PresentWhenNoEdges`

---

## References

- Shared-surface node: `correction-orphan-warning-wordin-30e8b461`
- Prior fix (data gap): `suggested-connections-in-remembe-2330807b` (STORY-058,
  `cross-domain-connect-ux.md`)
- Related gap: `connect-fails-silently-when-targ-e86973ca`
