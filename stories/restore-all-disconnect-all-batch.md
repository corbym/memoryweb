# batch restore_all and disconnect_all

**Status:** OPEN

**memoryweb-meta node:** `missing-batch-operations-forget--42b2eb9d`

**Precedent:** `forget_all` (shipped)

---

## Why

Sequential single-node calls for bulk restore or disconnect are slow, verbose, and risk
partial failure mid-batch. `forget_all` shipped; `restore_all` and `disconnect_all` were
flagged May 2026 and never filed as repo stories.

Verified 2026-07-26: `forget_all` exists in `tools/archive.go`; no `restore_all` or
`disconnect_all` in `tools/tools.go` dispatch or `tools/definitions.go`.

---

## Contract

Mirror `forget_all` shape:

### `restore_all`

```json
{ "items": [{ "id": "node-id" }, ...] }
```

- Atomic: all restore or none
- Each id must be archived (`archived_at IS NOT NULL`)
- Audit log `action=restore` per node
- Returns `{ "restored": N, "ids": [...] }`

### `disconnect_all`

```json
{ "items": [{ "edge_id": "..." }, ...] }
```

- Atomic batch hard-delete of edges by id
- Returns `{ "disconnected": N }`
- Edge ids from `recall` / `why_connected`

---

## Priority

Lower than correctness gaps. Ship when bulk archive/restore workflows appear in
dogfooding stats.

---

## Acceptance criteria

- Both tools in `ListTools` with agent-facing descriptions
- Handler tests in `tools/archive_test.go` / `tools/connect_test.go`
- `go test ./...` green
