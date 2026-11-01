# STORY-008: memoryweb import — cross-database merge CLI command

**Type:** feature

## Goal

Add a `memoryweb import --src <source.db> --dst <target.db>` CLI command that merges one database into an already-populated target database, preserving node IDs, all four timestamps, every relationship type, and audit log entries.

Implements the feature requested in https://github.com/corbym/memoryweb/issues/38.

## Design decisions

- **Storage-layer insert only** — bypasses MCP enum validation so non-standard relationship types migrate as-is.
- **Preserve node IDs** — imported nodes keep their source IDs; existing target nodes/edges are untouched. Re-running is idempotent (same ID skipped, not re-written).
- **Preserve all four timestamps** — `created_at`, `updated_at`, `occurred_at`, `archived_at` written verbatim. Source chronology survives intact in `history` / timeline views.
- **Column-probe source schema** — older source databases (missing newer columns) import with sensible defaults, same as the normal migration path.
- **Embeddings not migrated** — they are derived data and depend on the target's configured model. Post-import hint in report directs operator to run `memoryweb backfill`.
- **Audit log not migrated by default** — operational history of the source instance. `--include-audit-log` opt-in flag.
- **Archived nodes included by default** — faithful history migration. `--skip-archived` to exclude.
- **Conflict handling** — same ID + identical label/domain → idempotent skip. Same ID + different content → error by default. `--on-conflict skip` downgrades to a skip with a report entry.
- **Batched transactions** — import in chunks of ~500 nodes/edges per transaction (configurable), all-or-nothing per chunk.
- **Source opened read-only** (`mode=ro`).

## CLI flags

```
memoryweb import \
  --src <source.db> \
  --dst <target.db> \
  [--dry-run] \
  [--report <file.json>] \
  [--skip-archived] \
  [--include-audit-log] \
  [--on-conflict skip|error] \
  [--batch-size N]
```

## Report output (JSON)

Source/target node and edge counts, skipped IDs (with reason), conflict IDs, and a post-import hint if embeddings exist in the source.

## Out of scope

- Recordari — needs different merge mechanisms; tracked separately.
- `occurred-pending` tagging — defer until a concrete need surfaces.
- Domain filtering — keep the first version simple; add `--domain` scope in a follow-up if needed.

## Acceptance criteria

- [x] AC-STORY-008-ddaf60c3: `memoryweb import --src A.db --dst B.db` merges all live nodes, archived nodes, and edges from A into B without touching B's existing content.
- [x] AC-STORY-008-317aa730: Node IDs from A are preserved verbatim in B.
- [x] AC-STORY-008-d0433182: All four timestamps (created_at, updated_at, occurred_at, archived_at) are preserved exactly.
- [x] AC-STORY-008-8d830cf3: Non-standard relationship type strings import without error (storage-layer path, no MCP enum check).
- [x] AC-STORY-008-0a06796e: Re-running the same import is idempotent — no duplicate nodes or edges.
- [x] AC-STORY-008-088cc2da: --dry-run prints what would be imported without writing anything.
- [x] AC-STORY-008-5b4e74f8: --report file.json writes a structured summary (counts, skips, conflicts).
- [x] AC-STORY-008-7330578f: --skip-archived excludes archived nodes and their edges.
- [x] AC-STORY-008-6bf48d84: Same ID + different content → exits non-zero with a clear error by default; --on-conflict skip records the conflict in the report and continues.
- [x] AC-STORY-008-8ec5a184: Source is opened read-only; if the source file doesn't exist the command exits with a clear error.
- [x] AC-STORY-008-d67a41d4: Post-import report includes a hint to run memoryweb backfill if embeddings exist in the source.
- [x] AC-STORY-008-f8a9ef2c: All existing tests pass (go test ./...).
- [x] AC-STORY-008-38e9f689: A new integration test covers the round-trip: create nodes/edges in A, import into B, verify IDs/timestamps/relationships survive in B.

## Notes

<!-- backlog-mcp: 2026-11-01T14:34:08Z -->
Implemented `memoryweb import --src <source.db> --dst <target.db>` in db/import.go and main.go.

New types: ImportOptions, ImportReport, ImportSkip, ImportConflict in db/import.go.

ImportDB opens source read-only, applies pending migrations to destination, column-probes the source schema (handles older databases missing occurred_at/tags/archived_at/node_kind/verdict columns), classifies nodes as import/idempotent-skip/conflict, inserts in configurable batches (default 500), batch-checks edge endpoints against the destination, and optionally imports audit_log rows.

Flags: --src, --dst, --dry-run, --report (JSON), --skip-archived, --include-audit-log, --on-conflict (error|skip), --batch-size.

11 new tests in db/import_test.go cover: round-trip, idempotency, conflict-error, conflict-skip, skip-archived, include-archived, dry-run, timestamp preservation, same-file error, nonexistent source error, non-standard relationship types. All 10 packages pass go test ./...
