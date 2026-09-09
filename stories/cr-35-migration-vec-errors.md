# CR-35: Stop swallowing sqlite-vec migration errors

**Status:** DONE
**Priority:** Low

`db/migrations.go:138-159` — Migrations v8 and v9 swallow sqlite-vec errors by returning
`nil`. Wrong dimensions silently succeeds. If `DROP TABLE` succeeds but `CREATE VIRTUAL
TABLE` fails, embeddings table is gone.

---

## Acceptance criteria

- [x] Log sqlite-vec errors (at minimum `log.Printf`)
- [x] Probe vec_version() before DROP; skip migration if sqlite-vec absent
- [x] Return non-nil error if `CREATE VIRTUAL TABLE` fails after `DROP TABLE`
- [x] `TestMigration*` tests pass
