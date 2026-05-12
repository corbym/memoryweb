# memoryweb — Security review findings

**Reviewer:** external (downstream user preparing the binary for an internal AV-whitelisting request)
**Repository:** https://github.com/corbym/memoryweb
**Commit reviewed:** `0fe3391` (master, branch tip at time of review)
**Scope:** full source tree (`main.go`, `db/`, `tools/`, `stats/`, `cmd/`, `hooks/`, `.github/workflows/`)
**Out of scope:** the upstream `mattn/go-sqlite3` and `asg017/sqlite-vec-go-bindings` dependencies (treated as trusted third parties).

## Headline

The codebase is in good shape overall. There is no SQL injection, no inbound network listener, no `unsafe`/reflection abuse, no hard-coded credentials, and no obviously malicious behaviour — the AV detection that motivated this review is almost certainly an ML false positive on a stripped CGO Go binary.

The findings below are nevertheless worth fixing. None are remotely exploitable on a single-user developer machine; the highest-impact ones (F-1, F-3) become more relevant as soon as `memoryweb` is run on a shared host or behind a corporate proxy.

| ID | Severity | Area | Title |
|----|----------|------|-------|
| F-1 | Medium | `setup` subcommand | `curl &#124; sh` Ollama install with no integrity check |
| F-2 | Medium | `db.New` | SQLite foreign keys are declared but never enforced |
| F-3 | Medium | `setup`, `stats`, hook scripts, DB file | World-readable config / state files (`0644`) on multi-user hosts |
| F-4 | Medium | `db.embed` | `http.Post` to Ollama uses `http.DefaultClient` (no timeout) |
| F-5 | Low | hook scripts | `session_id` parsed by regex and interpolated into file paths |
| F-6 | Low | hook scripts | Manual JSON escaping in `dream_digest` misses control chars |
| F-7 | Low | tool handlers | `limit` parameters have no upper bound |
| F-8 | Low | `db.embed` | `MEMORYWEB_OLLAMA_ENDPOINT` is unvalidated |
| F-9 | Low | `setupStartOllama` | `ollama serve` is started detached with no lifecycle management |
| F-10 | Low | `db.New` | DSN built by string concatenation of user-controlled path |
| F-11 | Low | `stats`, `setup` | Non-atomic config / log writes can leave partial files on crash |
| F-12 | Info | release workflow | Release artifacts are unsigned; no SLSA / SBOM provenance |

---

## F-1 — `curl | sh` Ollama install with no integrity check  *(Medium)*

**Location:** `main.go:570`

```go
cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
```

**Impact.** The Ollama install path inside `memoryweb setup` downloads and executes a remote shell script with no checksum or signature verification. TLS is verified (no `--insecure`) and the user must type `y` first, but if `ollama.com` is compromised, a CA misissues a cert, or the user's trust store contains a hostile root, the executed payload is unverifiable. Classic `curl | sh` anti-pattern.

**Recommendation.**
- Prefer a packaged install: detect the platform and recommend `brew install ollama`, `winget install Ollama.Ollama`, or the user's distro package.
- If keeping the `curl` path, download to a temp file first, optionally show a hash to the user, then execute — at minimum this lets advanced users inspect the script before it runs:
  ```go
  tmp, _ := os.CreateTemp("", "ollama-install-*.sh")
  // curl -fsSL ... -o tmp.Name(); chmod +x; exec.Command("sh", tmp.Name())
  ```
- Better: pin a known-good SHA-256 of `install.sh` and refuse to execute if it doesn't match.

---

## F-2 — SQLite foreign keys declared but never enforced  *(Medium)*

**Location:** `db/db.go:94`

```go
db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
```

The schema declares `FOREIGN KEY(from_node) REFERENCES nodes(id)` (and similarly `to_node`) on the `edges` table (`db/db.go:308`), but `mattn/go-sqlite3` does **not** enable FK enforcement unless `_foreign_keys=on` is added to the DSN.

**Impact.**
- The "validate node exists, then insert edge" pattern in `AddEdge` (`db/db.go:592`) and `AddEdgesBatch` (`db/db.go:1391`) is a TOCTOU race: another connection (or the same connection on a different goroutine) can archive the node between the existence check and the insert. The edge then references a non-existent live node.
- Hard-deletion in `cmd/purge` (`cmd/purge/main.go:143`) deletes nodes but only opportunistically removes edges in the same loop — without FK enforcement, an interrupted purge can leave dangling edges that never error out.
- This is a quiet violation of the soft-delete contract documented in `CLAUDE.md`: archived nodes are supposed to act as walls in the graph, but edges can still be created against them and will silently survive purges of their endpoints.

**Recommendation.** Enable foreign key enforcement at connection time:

```go
db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
```

Then drop the `SELECT COUNT(*) FROM nodes WHERE id = ?` pre-checks — let the FK constraint produce the error.

---

## F-3 — World-readable config / state files on multi-user hosts  *(Medium)*

**Locations.**
- `main.go:383` — `~/Library/Application Support/Claude/claude_desktop_config.json` and `~/AppData/Roaming/ChatGPT/mcp.json` written `0644`
- `main.go:510` — `~/.claude/settings.local.json` written `0644`
- `main.go:504, 507, 380` — directories under `~/.memoryweb/` and `~/.claude/` created `0755`
- `stats/stats.go:126, 136, 157, 161, 214, 229` — stats and `.current` files written `0644`
- `db/db.go:94` — SQLite database created with the process's umask defaults (typically `0644`/`0664`)

**Impact.** On shared workstations / CI runners / multi-user dev VMs, every other local user can read:
- The full knowledge graph (every node label, description, why-it-matters, occurred-at) from `~/.memoryweb.db`.
- Audit log (which node was archived/restored, by whom, when, with what reason).
- Session statistics including domains, tool-call counts, and timestamps.
- The MCP server config — which exposes the path layout the user is running.

The DB in particular often holds non-public material: ticket numbers, internal architecture notes, design decisions. None of that should default to world-readable.

**Recommendation.**
- Set explicit `0600` on all files written by `memoryweb setup` and `stats` (`os.WriteFile(..., 0600)`, `os.OpenFile(..., 0600)`).
- Set explicit `0700` on directories created under the user's profile.
- For the SQLite file: open the path with `os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)` first to fix the permissions, then close and let `sql.Open` reopen it — or `os.Chmod` after `New` returns. Alternatively, document that users on multi-user hosts must `chmod 0600 ~/.memoryweb.db` themselves.

---

## F-4 — `http.Post` to Ollama uses the default client (no timeout)  *(Medium)*

**Location:** `db/db.go:167`

```go
resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
```

`http.Post` uses `http.DefaultClient`, which has **no timeout**. If Ollama is reachable via TCP (the `vecAvailable` check at `db.New` succeeded) but stops responding, every subsequent `embed()` call hangs forever, and so does every `add_node`/`add_nodes`/`search` call that triggers it.

**Impact.** A slow or wedged Ollama can wedge the entire MCP server, requiring the user to kill the process. Not a security flaw per se, but the MCP host (Claude Code, Claude Desktop) has no way to recover except by terminating memoryweb.

**Recommendation.** Use a bounded client, mirroring the 3-second timeout already used in `checkLatestRelease`:

```go
var ollamaClient = &http.Client{Timeout: 5 * time.Second}
// …
resp, err := ollamaClient.Post(endpoint, "application/json", bytes.NewReader(body))
```

---

## F-5 — `session_id` parsed by regex and interpolated into file paths  *(Low)*

**Location:** `hooks/memoryweb_save_hook.sh:14-22`, `hooks/memoryweb_precompact_hook.sh:14-18`

```bash
session_id=$(printf '%s' "${json}" \
  | grep -o '"session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 | grep -o '"[^"]*"$' | tr -d '"')
…
count_file="${STATE_DIR}/${session_id}.count"
saving_flag="${STATE_DIR}/${session_id}.saving"
…
transcript=$(find "${PROJECTS_DIR}" -name "${session_id}.jsonl" 2>/dev/null | head -1)
```

Two issues:

1. **Regex JSON parsing.** `[^"]*` does not handle escape sequences. A session_id containing `\"` would terminate the match early; one containing newlines or null bytes would behave unpredictably.
2. **Path traversal.** `session_id` is interpolated directly into file paths with no validation. A session_id of `../../foo` would cause `count_file=$STATE_DIR/../../foo.count`, breaking out of the state dir. Realistic Claude Code session IDs are UUIDs, so this is not currently exploitable — but the script reads attacker-influenced JSON from stdin, and any future change to the session_id format could turn this into a write-anywhere primitive.

**Severity.** Downgraded from Medium to Low after recalibration: in practice session_ids are UUIDs generated by Claude Code itself, the JSON source is a local trusted IPC channel, and exploiting this requires already having local code execution on the user's machine. Defence-in-depth, not a live vector.

**Recommendation.** Sanitise after extraction (one line):

```bash
session_id=$(printf '%s' "${session_id}" | tr -cd 'a-zA-Z0-9_-')
```

Or, more strictly, refuse to proceed on any non-conforming value:

```bash
case "$session_id" in
  *[!A-Za-z0-9_-]*|"") printf '{"continue":true}\n'; exit 0 ;;
esac
```

The same change should be applied to both hook scripts.

---

## F-6 — Manual JSON escaping in `dream_digest` misses control characters  *(Low)*

**Location:** `hooks/memoryweb_save_hook.sh:77-83`

```bash
_esc="${dream_digest//$'\\'/\\\\}"
_esc="${_esc//$_dq/\\\"}"
_esc="${_esc//$'\n'/\\n}"
```

The script escapes `\`, `"`, and `\n`, then emits `_esc` inside a JSON string in the hook's stdout response. RFC 8259 requires *all* control characters (U+0000–U+001F) to be escaped in JSON strings — at minimum `\t` (U+0009), `\r` (U+000D), `\b` (U+0008), and `\f` (U+000C). If `memoryweb dream` ever produces output containing a literal tab, carriage return, backspace, or form feed (e.g. via a node label or description that contains one), the resulting JSON is malformed and the hook response is rejected by Claude Code.

**Impact.** Hook break / session DoS, not a security boundary breach. Low likelihood today (dream digest is plain ASCII), but the failure mode is silent and confusing.

**Recommendation.** Use `jq` if available — it handles all control chars correctly:

```bash
if command -v jq >/dev/null 2>&1; then
  _esc=$(printf '%s' "$dream_digest" | jq -Rs '.[1:-1]')   # strip the surrounding quotes jq adds
else
  # existing manual escaping as fallback
fi
```

Or extend the manual cascade to cover all control chars:

```bash
_esc="${_esc//$'\t'/\\t}"
_esc="${_esc//$'\r'/\\r}"
_esc="${_esc//$'\b'/\\b}"
_esc="${_esc//$'\f'/\\f}"
```

---

## F-7 — `limit` parameters have no upper bound  *(Low)*

**Location:** `tools/tools.go:601, 659, 697, 840, 1079` (handler-side defaults), corresponding store methods in `db/db.go`.

The `search`, `recent`, `history`, `whats_stale`, and `suggest_connections` tools accept a `limit` integer, default it to 10/20/40 if `<= 0`, and pass it straight through to the store layer. The only handler with an upper cap is `visualise_domain` / `GetDomainGraph` (`db/db.go:1707-1709`, `if limit > 100 { limit = 100 }`).

**Impact.** A caller can request `limit: 1_000_000`. SQLite's `LIMIT` clause prevents runaway result sets when the matching row count is small, but for `searchNodesSemantic` the `vec_distance_cosine(...) AS dist ORDER BY dist ASC LIMIT ?` query forces a full scan of `node_embeddings` and a sort. On a non-trivial graph this can pin a CPU for seconds. In a single-user local MCP context the blast radius is the user's own machine, but it is a free DoS handle for any malicious content that ever reaches the LLM and influences tool args.

**Recommendation.** Cap each handler at a sensible maximum (e.g. 500), mirroring `GetDomainGraph`:

```go
if a.Limit <= 0 {
    a.Limit = 10
}
if a.Limit > 500 {
    a.Limit = 500
}
```

Applies to: `searchHandler`, `recentHandler` (and `recentByDomainHandler`), `historyHandler` (Timeline), `whatsStaleHandler` (FindDrift), `suggestConnectionsHandler`.

---

## F-8 — `MEMORYWEB_OLLAMA_ENDPOINT` is unvalidated  *(Low)*

**Location:** `db/db.go:155-167`

The env var lets the user redirect embedding traffic, including the full content of every node label, description, and why-it-matters field, to an arbitrary URL. Env vars are user-controlled, so this is not a privilege boundary — but it is a silent data-exfiltration channel that is easy to set by accident (e.g. inheriting from a shell rc or a misconfigured launchd plist) or via prompt-injected setup instructions.

**Recommendation.**
- Refuse non-`http://localhost`/`http://127.0.0.1` endpoints unless an explicit `MEMORYWEB_ALLOW_REMOTE_EMBED=1` is also set, **and** log a one-line warning to stderr at startup whenever the override is in effect.
- Document the variable in the README's privacy/network section so its existence is discoverable.

---

## F-9 — `ollama serve` started detached with no lifecycle management  *(Low)*

**Location:** `main.go:609-643`

```go
cmd := exec.Command("ollama", "serve")
cmd.Stdout = nil
cmd.Stderr = nil
if err := cmd.Start(); err != nil { … }
```

The child process is started but never waited on, never killed when `memoryweb` exits, stdout/stderr are discarded. After `memoryweb setup` finishes, an `ollama serve` daemon keeps running indefinitely with no parent.

**Impact.** Resource leak; the user has no clear way to know who started the Ollama daemon. Discarded stderr also hides install-time errors from the user.

**Recommendation.**
- At minimum, log that an Ollama daemon was started and how to stop it.
- Consider not starting `ollama serve` at all from `setup`; recommend `ollama serve` be launched from a launchd/systemd unit (or `brew services start ollama`).

---

## F-10 — DSN built by string concatenation of user-controlled path  *(Low)*

**Location:** `db/db.go:94`, `cmd/purge/main.go:52`

```go
sql.Open("sqlite3", path+"?_journal_mode=WAL")
```

If `path` already contains a `?` (e.g. someone sets `MEMORYWEB_DB=db?mode=rwc`), the resulting DSN is malformed. Not a security issue — env vars are user-controlled — but a robustness papercut.

**Recommendation.** Use `net/url` or a small helper that handles existing query strings, or escape the path with the documented `mattn/go-sqlite3` `file:` URL form:

```go
dsn := "file:" + url.PathEscape(path) + "?_journal_mode=WAL&_foreign_keys=on"
```

---

## F-11 — Non-atomic config / log writes  *(Low)*

**Locations:** `main.go:383, 510`, `stats/stats.go:157, 161`, etc.

`os.WriteFile` truncates and writes in place. A SIGKILL (or full disk) mid-write leaves a half-written `~/.claude/settings.local.json`, which Claude Code can then fail to parse on next start.

**Recommendation.** Write to `<path>.tmp` and `os.Rename` over the destination — atomic on POSIX, near-atomic on Windows. The existing `.current` recovery path in `stats` already does this conceptually; generalising it to all config writes is cheap.

---

## F-12 — Release artifacts are unsigned, no SLSA / SBOM provenance  *(Info)*

**Location:** `.github/workflows/release.yml`

The release workflow produces tarballs/zips and a `checksums.txt`, but:
- The Windows binary is not signed (acknowledged in `docs/install-windows.md:274`). Unsigned + stripped CGO is exactly the combination that Symantec/Defender ML models flag as `Heur.AdvML.D` — i.e. this is partly self-inflicted.
- macOS binaries are not codesigned/notarised.
- No SLSA-3 attestation, SBOM, or signed checksum file. Users who download `checksums.txt` cannot prove it came from the workflow.

**Recommendation.**
- Add `sigstore/cosign` keyless signing of the release archives (free, no certificate cost) — `cosign sign-blob` produces a `.sig` and `.cert` users can verify against the workflow's OIDC identity.
- Generate an SLSA-3 provenance attestation via `slsa-framework/slsa-github-generator`.
- For Windows specifically: drop `-ldflags="-s -w"` so the binary keeps its symbol table — or apply for an Authenticode certificate (the Sigstore project also has experimental signing for Windows).

These together would cut the false-positive rate dramatically and make this exact whitelisting request unnecessary for future users.

---

## Things that were checked and found clean

- **SQL injection.** Every query in `db/db.go`, `cmd/purge/main.go`, and the in-tool dispatchers uses parameter placeholders. Dynamic `IN(?, ?, …)` clauses build the placeholder list but bind through `args...`. No user input is concatenated into SQL.
- **Inbound network.** No `net.Listen`, no HTTP server, no Unix socket. MCP is stdin/stdout only.
- **Outbound network.** Two destinations, both bounded and discoverable: `localhost:11434` (Ollama, optional, opt-in) and `https://api.github.com/repos/corbym/memoryweb/releases/latest` (update check, only fired by `memoryweb doctor`, 3s timeout, no body).
- **Process execution.** All `exec.Command` calls use literal program names and literal arguments — no user input is passed to a shell. The single `sh -c` call (Ollama install) is the F-1 finding above.
- **Unsafe / reflection.** No `unsafe` imports. No reflection beyond the standard `encoding/json`. Only `syscall` reference is `signal.Notify(SIGTERM, SIGINT)` for graceful shutdown.
- **Crypto / randomness.** `crypto/rand` for ID generation; no security-sensitive use of `math/rand`.
- **Secret handling.** No hard-coded credentials, no `.env` file reads, no token storage.
- **Path resolution in Go code.** All FS writes from Go go through `filepath.Join` rooted at `os.UserHomeDir()` or `os.Getenv("APPDATA")`. The path-traversal exposure is in the shell hook scripts only (F-5).
- **Dependencies.** Two direct deps (`mattn/go-sqlite3 v1.14.22`, `asg017/sqlite-vec-go-bindings v0.1.6`), both pinned in `go.sum`. No known CVEs at these versions.

---

## Methodology

- Whole-tree read of every `.go` file plus the two hook scripts.
- Targeted `grep` passes for known-risky patterns: `exec.`, `unsafe.`, `reflect.`, `cgo`, `http.`, `net.Listen`, `os.WriteFile`, `os.Setenv`, `0o644`/`0644`, `_foreign_keys`, `sql.Open`, string concatenation into SQL.
- Read of `go.mod`, `go.sum`, all `.github/workflows/*.yml`, and the Homebrew formula.
- Cross-checked against an independent second-pass review; severities recalibrated where the second review made a more defensible case (F-1, F-5), and two additional findings adopted (F-6, F-7).
- No dynamic analysis; no fuzzing. A follow-up `go test ./...` + `govulncheck` + `gosec` pass would be a reasonable next step for the maintainer.

The findings are listed roughly in the order the maintainer would most usefully act on them; severities are calibrated for a single-developer machine and would all rise by one notch if `memoryweb` is ever deployed in a multi-tenant or server-side context.