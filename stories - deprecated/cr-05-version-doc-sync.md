# CR-05: Sync version numbers across all documentation

**Status:** DONE — implemented this session. README/AGENTS/CLAUDE.md synced to v1.54.7 / 18 tools; CLAUDE.md Node struct corrected (node_kind replaces transient); Formula version 1.54.7 with stale hashes removed. go.mod bump deferred to CR-30 (dedicated story). All tests pass.

Four different version numbers exist across five files:

| File | Version |
|------|---------|
| `CLAUDE.md:330` | v1.52.0 |
| `AGENTS.md:273` | v1.43.0 |
| `README.md:96` | v1.43.0, 16 tools |
| `Formula/memoryweb.rb:7` | v1.4.3 |
| `CLAUDE.md:332` | "All 18 MCP tools" |

---

## Acceptance criteria

- [ ] Determine the true current version from the latest git tag or `main.go` version string
- [ ] Update `README.md` version and tool count to match
- [ ] Update `AGENTS.md` version and tool count to match
- [ ] Update `CLAUDE.md` Node struct to reflect `node_kind TEXT` (not `Transient bool`)
- [ ] Update `Formula/memoryweb.rb` version and SHA256 hashes (or remove stale hashes and rely on tap)
- [ ] Verify `go.mod` `go` directive matches actual minimum Go version tested against

---

## Notes

The public README is the most-visible doc and is two major versions behind.
This erodes user trust. Quick fix — all edits are in documentation files.
