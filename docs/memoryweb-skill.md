---
name: memoryweb
description: "Activate at the start of any session where memoryweb MCP tools are available. Covers filing, connecting, and retrieving knowledge through the memoryweb graph — any coding, architecture, backlog, or general work an agent is tracking for this user."
---

# memoryweb — Agent Instructions

Two layers, kept deliberately separate: a short imperative contract up top,
reference material below it. Position determines compliance — instructions
that only live in reference material get skipped, not from disagreement, but
because agents never reach them.

---

## Layer 1 — Behavioural Contract

1. Call `orient()` before anything else, unprompted. No domain yet — pick one
   from the result, then `orient(domain=X)`.
2. File a memory the moment something is decided or found — not batched at
   session end. Batching loses the early items.
3. Source material is a finding, not a footnote. If you read code, a doc, a
   log, a search result, or any third-party evidence to reach a decision,
   file that evidence itself as `node_kind=finding` — separately from the
   decision. Don't fold raw evidence into a decision's `description` only. A
   decision can cite a finding by ID; it should not *be* the finding wearing
   a decision's clothes.
4. Right after filing, resolve every `suggested_connections` candidate:
   `connect` it or explicitly skip it. Treat `possible_duplicates` and
   `skipped_connections` on the `remember` response as instructions to act
   on in the same turn, not status lines to narrate and move past.
   Separately check the domain's standing rules (`orient(domain=X)`'s
   `rules` section, or `search(node_kind=standing)`) for a self-referencing
   linkback directive and satisfy it directly — `suggested_connections` is
   pure nearest-neighbour matching and a closer same-domain sibling can
   crowd out a low-frequency but highly relevant standing rule.
5. 🪝 **Hook-backed hosts (Claude Code, Codex):** a Stop hook (save) and a
   PreCompact hook (orphan nudge, dream digest) run behind you. They're a
   backstop, not permission to skip steps — run `audit(mode=orphans)`,
   `audit(mode=stale)`, and `audit(mode=conflicts)` as three separate steps
   before ending the session. **No-hook hosts (claude.ai chat/web, Claude
   Desktop, ChatGPT, raw API):** there is no mechanical sweep behind you. Run
   all three at natural pauses, not just "before ending" — you may not get a
   clean end-of-session moment. Either way, never merge these into one pass or
   one report: orphans, stale drift, and semantic conflict candidates are
   different failure modes needing different handling. `conflicts` is a
   domain-wide semantic sweep — not interchangeable with filing-time
   `suggested_connections` on `remember`.
6. Orphans: resolve every one yourself (`suggest_connections` + `connect`).
   Only ask the user when the correct target is genuinely ambiguous —
   multiple equally plausible candidates, or none at all.
7. Stale / conflicts: triage what comes back. Duplicates and superseded
   labels are yours to fix — `revise`, don't ask. A genuine `contradicts`
   edge is *not* yours to resolve: present both conflicting claims to the user
   and wait for their call. Before escalating a pair, confirm no
   `resolved`/`resolved_by`/`supersedes` edge already links the two IDs —
   that edge is the canonical close signal. A `RESOLVED` label prefix on
   either side is a legacy backstop only; do not treat a label alone as
   closed if the adjudication edge is missing.    Once decided, verify the exact pair via
   `why_connected(from_id=..., to_id=...)` — preferred for exact-ID pair
   verification — or `recall(id)`'s `edges` array. Do **not** rely on
   label-only `why_connected(from_label, to_label)` (resolves each side by
   best-match label search, not exact ID, and can silently pick the wrong
   node) — useful for fuzzy concept lookup, not as contradiction-pair proof
   when IDs are known.
   Once verified, call `connect(relationship=resolved, verdict=...)` (or
   `resolved_by` / `supersedes` as the relationship type). On `resolved`,
   optional `verdict` (`false_positive`, `reconciled`, `superseded`)
   classifies *how* the pair was adjudicated — stored on the edge and
   returned by `recall`/`why_connected`. Additive — the original
   `contradicts` edge stays on the record, and the pair stops surfacing in
   `audit(mode=stale)` automatically.
8. Say nothing about any audit if all three come back clean. Only speak up for
   an unresolved orphan or a live contradiction still awaiting the user's
   call — no routine "orphans checked / stale checked / conflicts checked"
   status line.
9. Delegating to a sub-agent: inject your own `orient()` output into its
   context. It starts cold otherwise.
10. If leaving mid-flight work unfinished, file a `node_kind=goal` before
    stopping — label it "Next session: [what to pick up first]", concrete
    starting point in `why_matters`. `orient`'s `recent` section surfaces it
    at the next bootstrap. Skip this step if the session closed cleanly.
11. File only decisions, findings, standing rules, and resolved issues —
    never conversational noise or self-referential musing.

`audit` sweeps are the real backstop, which is why step 5 anchors both host
variants on the same three separate calls. This is defence in depth, not a
guarantee.

---

## Layer 2 — Reference

### Filing workflow

Before calling `remember`, `search` first. Infer the domain from what comes
back — prefer an existing domain over creating a new one. Creating a new
domain hides the memory from every other domain's `orient` and
domain-scoped `search` — only create one when no existing domain covers
the topic. If a similar memory already exists, `revise` it instead of filing
a duplicate. When revising a decision, do not paste new source material into
its `description` — file a `finding` and `connect` instead.

If `orient()` returned a nonzero stale count for the domain, run
`audit(mode=stale)` before filing anything new there — a fresh contradiction
is easier to reason about before more nodes pile on top of it.

`audit(mode=conflicts)` surfaces semantically close pairs as *candidates*,
not confirmed contradictions — a density signal, not a queue to drive to
zero. A pair is suppressed from future conflicts sweeps only when linked by
`contradicts`, `resolved`, `resolved_by`, or `supersedes` — the same edges
that close a contradiction. Other relationships (`caused_by`, `depends_on`,
`connects_to`, etc.) do not suppress semantic adjacency; pre-connected pairs
can still surface as candidates.

### `node_kind` taxonomy

| `node_kind` | Use for |
|---|---|
| `decision` (default) | A settled choice. Not evidence, not a plan. |
| `standing` | A durable rule governing future sessions; surfaces in `orient`'s `rules`. |
| `finding` | **An observed fact or result — including source material.** Code read, a doc fetched, a log inspected, a test result, third-party evidence. If you could quote/cite where it came from, it's a finding. |
| `issue` | An open question or problem — named gaps, untracked TODOs. |
| `option` | A considered alternative. |
| `assumption` | An unverified premise — distinct from `finding`: a finding is checked, an assumption isn't. |
| `reference` | A person, system, or org — referential, not propositional. |
| `goal` | A desired outcome. **Also the handoff primitive** — see Layer 1 step 10. |
| `transient` | Temporary; expires. Surfaced by `audit(mode=stale)` after 7 days. |

The legacy `transient: true` boolean is still accepted and maps to
`node_kind='transient'` when `node_kind` isn't set — prefer `node_kind`
directly. The legacy `decision_type` field name is rejected.

**The most common miss is `finding` vs `decision`.** Ask: *did I just decide
something, or did I just learn something?* "I checked X and found Y" — Y is
a `finding`, even if it immediately caused a decision. File the finding,
then the decision with a `depends_on`/`caused_by` connection pointing at it.

### Relationship types (`connect`)

| Type | Use when |
|---|---|
| `connects_to` | General association (default/fallback) |
| `depends_on` | A has a hard prerequisite on B |
| `led_to` / `caused_by` | Same link from opposite ends: A `led_to` B ≡ B `caused_by` A |
| `blocked_by` / `unblocks` | A is blocked by B / A unblocks B |
| `contradicts` | A and B directly conflict |
| `governed_by` | A must satisfy a standing rule or constraint B |
| `is_example_of` | A illustrates B |
| `resolved` / `resolved_by` / `supersedes` | Adjudicates a `contradicts` pair. **Verify the exact pair first** via `why_connected(from_id=..., to_id=...)` — preferred — or `recall(id)`'s `edges` array (Layer 1 step 7). Do not rely on label-only `why_connected`. Additive. On `relationship=resolved`, optional `verdict` (`false_positive`, `reconciled`, `superseded`) classifies *how* the contradiction was adjudicated — stored on the edge and returned by `recall`/`why_connected`. |

Custom relationship strings are accepted as a fallback; prefer a typed one
from the table above.

### Domain routing

memoryweb has no fixed domain list — domains are created implicitly by
filing into them.

- Call `domains()` (or `orient()` with no domain) at session start to see
  what already exists before proposing a new one.
- Prefer an existing domain over creating a new one; keep domains scoped to
  one project or topic.
- Never file credentials, connection strings, API keys, or tokens.

### Domain move protocol

Two different operations move memories between domains; don't confuse them:

- **`revise(id, domain=..., reason=...)`** moves a single memory. Only set
  `domain` when the user explicitly names the target — never on your own
  inference. State current domain and proposed target and wait for
  confirmation first; `reason` is required and recorded in the audit log
  verbatim. Confirm with `orient(domain=new_domain)` afterward.
- **`domains(action=rename, old_domain=..., new_domain=...)`** renames an
  entire domain in place — every memory moves, and an alias from the old name
  is registered automatically. Fails if the new name already has memories (use
  the CLI `merge_domains` for that, not an MCP tool).

### `occurred_at`

- Witnessed directly this session → set without asking; default to today if
  no date was given.
- Inferred or back-dated events you did not directly observe → **propose,
  then confirm.** State the date and reasoning, wait for confirmation, only
  then set it.
- Turn-boundary rule: if proposing to file something as significant, that
  proposal is the only thing in that turn. Set `occurred_at` in a follow-up
  call, after the user replies.
- Always pair `occurred_at` with `why_matters`.

### Archiving & drift protocol

- `audit(mode=stale)` surfaces contradictions, superseded labels,
  duplicates, stale open questions, old transient memories. Contradiction
  signals are recomputed from content each call — resolution must be
  structural (a `resolved`/`resolved_by`/`supersedes` edge), not a label
  edit.
- `audit(mode=orphans)` surfaces live, non-transient memories with zero
  connections. `audit(mode=archived)` lists archived memories — use it when
  `search` returns nothing but you expect content to exist.
- **`forget(id, reason)` / `forget_all(items=[...])`** — archive only after
  explicit, unambiguous user confirmation: only suggest after
  `audit(mode=stale)` surfaces a candidate or the user names something
  stale; always ask "Should I archive this?", never assume yes; wait for
  unambiguous confirmation ("that's probably outdated" doesn't count);
  never archive on casual mention; after archiving, report the ID(s) and
  note they're un-archivable with `forget(restore=true)`. Use `forget_all`
  (one atomic transaction) once you have 2+ confirmed IDs rather than repeated
  `forget` calls.
- `forget(id, restore=true)` un-archives — get the ID from
  `audit(mode=archived)`.
- `disconnect(id)` hard-deletes an edge (by edge ID, from `recall`'s `edges`
  array) — no built-in confirmation protocol, but treat it like `forget`:
  irreversible.
- `significance(mode=trust)` ranks memories by computed epistemic trust
  (from `node_kind` and connected relationship types). A `contradicts` edge
  lowers trust; resolving it lifts the penalty automatically. Only
  meaningful if `node_kind` is filed honestly. `orient(domain=X)`'s
  `significant` section also annotates load-bearing low-trust nodes inline
  (`trust: "low — …"`). `remember`/`revise` may return an advisory
  `trust_nudge` when resting on a low-trust dependency neighbourhood. On
  `remember`, targets named in `related_to` are assessed before edges are
  created. On `revise`, only when `label`, `description`, `why_matters`, or
  `node_kind` change — not tags-only or domain-only updates — and outbound
  `connects_to`, `depends_on`, `caused_by`, or `blocked_by` edges reach
  low-trust targets. Batch `items` entries carry the same optional fields per
  node. Creating a new domain may return
  `possible_misdomain`, `suggested_domain`, and `suggested_memory_id` when
  workspace KNN finds a closer existing domain — requires Ollama embeddings
  and sqlite-vec; absent when embeddings are unavailable.
- `audit(mode=kind_coverage)` returns per-kind counts, legacy
  decision/standing dominance, and lean `migration_candidates` (`id`, `label`,
  truncated `why_matters`) — decision nodes whose text suggests a different
  `node_kind`. Call `recall(id)` before acting on content. Candidate-surfacing
  only; never auto-revise.

### Lean output — `recall(id)` before acting on content

`orient`, `search`, `history`, `significance`, `audit` all return
**lean** entries: `id`, `label`, and a truncated `why_matters` excerpt —
never the full `description`. When graph state warrants it, entries also carry
`lifecycle_state` (`contested`, `resolved`, or `superseded`); digest lines append
the same token as a `(state)` suffix after any date. Treat these as an index, not the content.
Before quoting, citing, or acting on what a memory actually says, call
`recall(id)` for the full node plus its `edges` array.

### List truncation — `results_truncated`

Multi-result tools return wrapped objects, not bare arrays. Each includes
`results_truncated: true|false` (or section-specific booleans on `orient` and
`significance`). When `true`, raise `limit` (or `declared_limit` on
`significance`) and call again until `false` before concluding the list is
complete.

| Tool | Response shape |
|---|---|
| `history(order=effective)` | `{nodes, results_truncated}` — default; chronological by effective date |
| `history(order=modified)` | `{nodes, results_truncated}` or `{groups, results_truncated}` when `group_by_domain=true` |
| `audit(mode=stale)` | `{candidates, results_truncated}` — empty is `{candidates: [], results_truncated: false}` |
| `audit(mode=orphans)` | `{nodes, results_truncated}` — empty is `{nodes: [], results_truncated: false}` |
| `audit(mode=archived)` | `{nodes, results_truncated}` — empty is `{nodes: [], results_truncated: false}`; **default cap 25**; raise `limit` to enumerate |
| `audit(mode=conflicts)` | `{candidates, results_truncated}` — empty is `{candidates: [], results_truncated: false}` |
| `audit(mode=kind_coverage)` | `{total_nodes, by_kind, legacy_dominant_pct, migration_candidates, results_truncated}` |
| `significance` | section booleans: `declared_results_truncated`, `structural_results_truncated`, `uncurated_results_truncated`, `potentially_stale_results_truncated`; `call_id` is opaque analytics metadata — ignore |
| `orient(domain=X)` | `significant_results_truncated`, `recent_results_truncated`, `declared_spine_results_truncated`, `rules_results_truncated`; low-trust nodes in `significant` carry optional `trust` |
| `orient()` (no domain) | `{domains, results_truncated}` — each domain entry has `recent_results_truncated`; pass `limit` to raise per-domain recent cap (default 5) |

Per-node excerpt truncation uses `truncated` on lean entries — distinct from
list-level `results_truncated`.

### Search notes

- `search` is lexical (LIKE) unless Ollama is running, in which case it also
  ranks by semantic distance. Query vocabulary must match stored text.
- Set `exact: true` for identifiers (ticket numbers, short codes) — normal
  ranking can bury an exact match, and short hyphenated codes don't tokenise
  well for lexical matching either way.

### Version awareness

Last verified against **v1.43.0** (16 MCP tools). `orient` returns
`server_version`. If it doesn't match, re-check tool behaviour via
`tools/list` rather than assuming this document is still accurate.

### Retired tools (v1.43.0 hard-cut)

Calling a retired tool name returns an error with the replacement. Common
migrations from the 21-tool surface:

| Retired | Use instead | Hard-cut error (abbrev.) |
|---|---|---|
| `recent` | `history(order=modified)` | `unknown tool: recent — use history with order=modified` |
| `restore` | `forget(restore=true)` | `unknown tool: restore — use forget with restore=true` |
| `trace` | `why_connected(from_id, to_id)` or `recall(id)` | `unknown tool: trace — use why_connected … or recall …` |
| `alias` | `domains(action=add_alias\|remove_alias\|resolve)` | `unknown tool: alias — use domains with action=…` |
| `rename_domain` | `domains(action=rename)` | `unknown tool: rename_domain — use domains with action=rename` |
| `list_domains` / `list_aliases` | `domains` | `unknown tool: list_domains — use domains` |
| `forgotten` | `audit(mode=archived)` | `unknown tool: forgotten — use audit with mode=archived` |
| `whats_stale` | `audit(mode=stale)` | `unknown tool: whats_stale — use audit with mode=stale` |
| `disconnected` | `audit(mode=orphans)` | `unknown tool: disconnected — use audit with mode=orphans` |
| `remember_all` / `connect_all` / `revise_all` | `remember` / `connect` / `revise` with `items` | `unknown tool: … — use … with an items array …` |
| `check_for_updates` | CLI: `memoryweb check-for-updates` | `unknown tool: check_for_updates — use the CLI: …` |

### Tool quick reference

| Tool | When |
|---|---|
| `orient()` | Session start — cross-domain bootstrap |
| `orient(domain=X)` | Full view: `rules`, `declared_spine`, `significant`/`relevant`, `recent` |
| `orient(domains=[...])` | Same, for 1–5 domains in one call |
| `orient(domain=X, topic=Y)` | `relevant` semantically matched to a known session purpose |
| `domains()` | List active domains and aliases |
| `domains(action=add_alias\|remove_alias\|resolve\|rename)` | Domain alias admin and in-place domain rename |
| `search(query=...)` | Find by vocabulary in stored labels/descriptions/tags |
| `recall(id)` | Full memory + connections |
| `history(order=modified)` | Where work was last happening (by updated_at); `group_by_domain=true` for per-domain activity |
| `history(order=effective)` / `history(important_only=true)` | Chronological decision spine |
| `significance()` | Dual-signal importance (declared + structural) |
| `significance(mode=trust)` | Epistemic trust ranking |
| `suggest_connections(id)` | Candidates to wire up after filing |
| `connect(...)` | Wire memories together; adjudicate contradictions via `relationship=resolved` (verify the pair via `why_connected(from_id, to_id)` first) |
| `disconnect(id)` | Hard-delete an edge by edge ID — irreversible |
| `remember(...)` | File a new memory; may return `trust_nudge`, `possible_misdomain`/`suggested_domain`/`suggested_memory_id` on new-domain creation (KNN requires embeddings) |
| `revise(id, ...)` | Update an existing memory; optional `trust_nudge` on content-changing updates when outbound `connects_to`/`depends_on`/`caused_by`/`blocked_by` reach low-trust targets; also handles single-node domain moves |
| `forget(id, reason)` / `forget_all(items=[...])` | Archive — confirmation required |
| `forget(id, restore=true)` | Un-archive |
| `audit(mode=...)` | `stale` / `orphans` / `archived` / `conflicts` / `kind_coverage` |
| `visualise(domain=X)` / `visualise(memory_id=X)` | Mermaid graph, human inspection only |
| `why_connected(from_id, to_id)` | Direct edges between exact IDs — preferred pair verification |
| `why_connected(from_label, to_label)` | Fuzzy label best-match — not exact-ID verification |

`purge` and `merge_domains` are CLI-only — never call them as MCP tools;
they don't exist as one.

Do not call `orient()` repeatedly to dig for more — its sections are bounded
by design. Use `search` for anything specific.
