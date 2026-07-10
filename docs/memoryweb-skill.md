---
name: memoryweb
description: "Activate at the start of any session where memoryweb MCP tools are available. Covers filing, connecting, and retrieving knowledge through the memoryweb graph — any coding, architecture, backlog, or general work an agent is tracking for this user."
---

# memoryweb — Agent Instructions

Two layers, kept deliberately separate: a short imperative contract up top,
reference material below it. Position determines compliance — instructions
that only live in reference material get skipped, not from disagreement, but
because agents never reach them. See "Why this shape" below Layer 1 before
changing that split.

Adapted from `recordari-skill.md` v7.1 (Recordari's own agent-behaviour
contract), re-grounded line-by-line against memoryweb's actual live tool
surface rather than ported verbatim — the two systems share a lineage and a
`node_kind` taxonomy, but their tool names, response shapes, and a few sharp
edges differ. See Provenance at the bottom for what changed and why.

---

## Layer 1 — Behavioural Contract

Pick the variant that matches your host. If unsure, use variant B — it's the
safer default.

### A. Hook-backed hosts (Claude Code, Codex)

memoryweb ships a Stop hook (save) and a PreCompact hook (orphan nudge, dream
digest) that run behind you. They're a backstop, not permission to skip steps.

1. Call `orient()` before anything else. No domain yet — pick one from the
   result, then `orient(domain=X)`.
2. File a memory the moment something is decided or found — not batched at
   session end. Batching loses the early items.
3. Source material is a finding, not a footnote. If you read code, a doc, a
   log, a search result, or any third-party evidence to reach a decision,
   file that evidence itself as `node_kind=finding` — separately from the
   decision. Don't fold raw evidence into a decision's `description` only.
   A decision can cite a finding by ID; it should not *be* the finding
   wearing a decision's clothes.
4. Right after filing, resolve every `suggested_connections` candidate:
   `connect` it or explicitly skip it. Also treat `possible_duplicates` and
   `skipped_connections` on the `remember` response as instructions to act
   on in the same turn, not status lines to narrate and move past —
   narrating a warning and continuing counts as ignoring it. Before treating
   any filing as done, separately check the domain's standing rules
   (`orient(domain=X)`'s `rules` section, or `search(node_kind=standing)`)
   for a self-referencing linkback directive and satisfy it directly —
   `suggested_connections` is pure nearest-neighbour matching and a closer
   same-domain sibling can crowd out a low-frequency but highly relevant
   standing rule.

   `orphan_warning` on a `remember` response is now accurate: it tells you to
   call `connect` with the two IDs directly and states plainly that `connect`
   takes no `domain` parameter. (Older versions of this response — before the
   fix described in Provenance — told agents to pass a `domain` value that
   the tool silently ignored. If you ever see `domain=` in that wording
   again, treat it as a regression and ignore it — `connect` only needs
   `from_memory`/`to_memory`.)
5. Before ending the session, run `audit(mode=orphans)` and
   `audit(mode=stale)` as two separate steps — never merge them into one
   pass or one report. They're different failure modes and need different
   handling.
6. Orphans: resolve every one yourself (`suggest_connections` + `connect`).
   Only ask the user when the correct target is genuinely ambiguous —
   multiple equally plausible candidates, or none at all.
7. Stale: triage what comes back. Duplicates and superseded labels are yours
   to fix — `revise`, don't ask. A genuine `contradicts` edge is *not* yours
   to resolve: present both conflicting claims to the user and wait for
   their call. Once they've decided, verify the exact pair before adjudicating
   — `recall(id)` on one side and check its `edges` array for a direct edge
   naming the other ID. Do **not** rely on `trace(from_id, to_id)` alone (it
   does a 6-hop BFS and can report a path through an unrelated third memory,
   which is not the same as a direct edge) or `why_connected(from_label,
   to_label)` alone (it resolves each side by best-match label search, not
   exact ID, and can silently pick the wrong node). Once verified, call
   `connect` with `relationship=resolved` (or `resolved_by` / `supersedes` —
   whichever fits is the verdict; memoryweb's `connect` has no separate
   `verdict` field). This is additive — the original `contradicts` edge
   stays on the record. A resolved pair stops surfacing in
   `audit(mode=stale)` automatically.
8. Say nothing about either audit if it comes back clean. Only speak up for
   an unresolved orphan or a live contradiction still awaiting the user's
   call — no routine "orphans checked / stale checked" status line.
9. Delegating to a sub-agent: inject your own `orient()` output into its
   context. It starts cold otherwise.
10. If leaving mid-flight work unfinished, file a `node_kind=goal` before
    stopping — label it "Next session: [what to pick up first]" and put the
    concrete starting point in `why_matters`. `orient`'s `recent` section
    surfaces it at the next bootstrap. Skip this step if the session closed
    cleanly.
11. File only decisions, findings, standing rules, and resolved issues —
    never conversational noise or self-referential musing.

### B. No-hook hosts (claude.ai chat/web, Claude Desktop, ChatGPT, raw API)

There is no mechanical sweep behind you. Everything in A still applies,
front-loaded harder:

1. Call `orient()` before anything else, unprompted — don't wait to be
   asked. Ambient tool use is not guaranteed on this host.
2. File and connect in the same breath. There is no end-of-session sweep
   coming to catch what you skip.
3. Source material is a finding, not a footnote — same rule as variant A,
   matters more here because there's no sweep to catch a decision that
   quietly absorbed its own evidence.
4. Treat `suggested_connections`, `possible_duplicates`, and
   `skipped_connections` on a `remember` response as required actions in the
   same turn, not narration. Separately check the domain's standing rules
   for a self-referencing linkback directive before treating a filing as
   done — same reasoning as A.4.

   `orphan_warning` correctly states that `connect` takes no `domain`
   parameter — just call it with the two IDs. (This was a live wording bug
   until the fix noted in Provenance; if the old `domain=` phrasing ever
   reappears, treat it as a regression, not an instruction to follow.)
5. Run `audit(mode=orphans)` and `audit(mode=stale)` at natural pauses, not
   just "before ending" — you may not get a clean end-of-session moment.
   Keep them as two separate steps even when run back to back.
6. Orphans: resolve every one yourself by default. Ask the user only when
   the correct target is genuinely ambiguous.
7. Stale: duplicates and superseded labels are yours to fix directly. A
   genuine `contradicts` edge goes to the user, not to you. Once decided,
   verify the exact pair — `recall(id)` on one side, check its `edges` for a
   direct edge to the other ID — before calling `connect(relationship=
   resolved)` (or `resolved_by`/`supersedes`). Don't infer the pair from
   labels or from `trace`/`why_connected` alone (see A.7 for why). Additive;
   the pair stops surfacing in `audit(mode=stale)` automatically.
8. If leaving mid-flight work unfinished, file a `node_kind=goal` before
   stopping — label it "Next session: [what to pick up first]", concrete
   starting point in `why_matters`. Skip if the session closed cleanly.
9. If you're about to stop and haven't checked orphans/stale this session,
   check now — resolve orphans silently. Only break silence for an
   unresolved orphan or a live contradiction still awaiting the user.

---

## Why this shape

- **Position and framing determine agent compliance.** Critical instructions
  must be at the top, in imperative form, with no exposed mechanism
  vocabulary ("node", "edge", "retrieved from"). Instructions that only live
  in reference material get skipped, not because agents disagree, but
  because they never reach them. memoryweb's own tool descriptions follow
  this same principle (see `CLAUDE.md`'s tool-description conventions).
- **The platform matters more than the prompt.** Some hosts honour the MCP
  `instructions` field and support Stop/PreCompact hooks; others silently
  drop both. On hosts without hooks, the entire compliance burden sits on
  this document — there is no second line of defence. Variant B exists
  because of that gap.
- **Ambiguous wording gets read the more permissive way, silently.** The
  `orphan_warning` history noted in A.4/B.4 is the concrete example: from
  2026-05-23 to 2026-07-10 the tool response told the agent to pass a
  `domain` parameter `connect` never accepted, and an agent following it
  literally either wasted a turn constructing it or, worse, treated a
  subsequently successful call as evidence the parameter did something. It
  traced to a false premise in the story that introduced it (`connect`
  resolves node IDs within a domain scope — it doesn't; IDs are global) that
  nobody revisited across two later wording passes, both of which were
  test-locked by regression assertions checking the wording, never the
  underlying behaviour. Now fixed at the source (see Provenance) — kept here
  as the reason A.4/B.4 still tell you to treat a `domain=` reappearance as a
  regression rather than an instruction.
- **Comprehension is not compliance.** An agent can read a warning, narrate
  that it read it, and then not act on it in the same turn — this is a
  distinct failure mode from never seeing the warning at all, and it's why
  A.4/B.4 treat `orphan_warning`/`possible_duplicates`/`skipped_connections`
  as instructions to execute, not facts to mention.

`audit` sweeps are the real backstop — which is why both variants above end
on one. This is defence in depth, not a guarantee.

---

## Layer 2 — Reference

### Filing workflow

Before calling `remember`, `search` first. Infer the domain from what comes
back — prefer an existing domain over creating a new one. If a similar
memory already exists, `revise` it instead of filing a duplicate.

If `orient()` returned a nonzero stale count for the domain, run
`audit(mode=stale)` before filing anything new there — a fresh contradiction
is easier to reason about before more nodes pile on top of it.

`audit(mode=conflicts)` surfaces semantically close pairs as *candidates*,
not confirmed contradictions — a density signal, not a queue to drive to
zero. Connecting a flagged pair (any relationship) suppresses it from future
sweeps; a later substantive revision to either memory lifts the suppression,
so the count can rise again without indicating new drift.

### `node_kind` taxonomy

| `node_kind` | Use for |
|---|---|
| `decision` (default) | A specific decision made — a settled choice. Not evidence, not a plan. |
| `standing` | A durable rule or principle — governs future sessions in the domain; surfaces in `orient`'s `rules` section. |
| `finding` | **An observed fact or result — including source material.** Code you read, a doc you fetched, a log you inspected, a test result, a search result, third-party evidence. If you could quote or cite where it came from, it's a finding. |
| `issue` | An open question or problem. Named gaps, things undecided, untracked TODOs. |
| `option` | A considered alternative. |
| `assumption` | An unverified premise — distinct from `finding`: a finding is checked, an assumption is not. |
| `reference` | A person, system, or org — referential, not propositional. |
| `goal` | A desired outcome — plans, next steps, pending tasks. **Also the handoff primitive:** file a `goal` labelled "Next session: [what to pick up first]" when leaving mid-flight work. |
| `transient` | Temporary; will expire (sprint notes, in-progress status). Surfaced by `audit(mode=stale)` after 7 days. |

The legacy `transient: true` boolean is still accepted on `remember`/`revise`
and maps to `node_kind='transient'` when `node_kind` isn't set — prefer
`node_kind` directly. The legacy `decision_type` field name is rejected.

**The most common miss is `finding` vs `decision`.** Before filing as
`decision`, ask: *did I just decide something, or did I just learn
something?* If the content is "I checked X and found Y", Y is a `finding` —
even if it immediately caused a decision. File the finding, then file the
decision with a `depends_on`/`caused_by` connection pointing at it.

### Relationship types (`connect`)

| Type | Use when |
|---|---|
| `connects_to` | General association (default/fallback — use only when no typed relationship fits) |
| `depends_on` | A has a hard prerequisite on B |
| `led_to` / `caused_by` | Same link from opposite ends: A `led_to` B ≡ B `caused_by` A |
| `blocked_by` / `unblocks` | A is blocked by B / A unblocks B |
| `contradicts` | A and B directly conflict |
| `governed_by` | A must satisfy a standing rule or constraint B |
| `is_example_of` | A illustrates B |
| `resolved` / `resolved_by` / `supersedes` | Adjudicates a `contradicts` pair. **Verify the exact pair first** — `recall(id)` on one side, check its `edges` for a direct edge to the other ID (see Layer 1 A.7). Additive — the original `contradicts` edge stays on the record; the pair stops surfacing in `audit(mode=stale)`/`audit(mode=conflicts)` once resolved. There is no separate `verdict` parameter — the relationship type chosen *is* the verdict. |

Custom relationship strings are accepted as a fallback, but prefer a typed
one from the table above.

### Domain routing

memoryweb has no fixed domain list — domains are created implicitly by
filing into them. Because of that:

- Call `domains()` at session start (or `orient()` with no domain) to see
  what already exists before proposing a new one.
- Infer the domain from `orient()`/`search()` hits or `alias(action=resolve)`.
- Prefer an existing domain over creating a new one; keep domains scoped to
  one project or topic — don't fold unrelated work into a convenient bucket.
- Never file credentials, connection strings, API keys, or tokens.

### Domain move protocol (memoryweb-specific — no Recordari equivalent)

Two different operations move memories between domains; don't confuse them:

- **`revise(id, domain=..., reason=...)`** moves a single memory. Only set
  `domain` when the user explicitly names the target — never on your own
  inference. Before calling, state the current domain and the proposed
  target and wait for confirmation; "that's probably in the wrong domain" is
  not a target name. `reason` is required and is recorded in the audit log
  verbatim. After moving, call `orient(domain=new_domain)` to confirm the
  memory is visible in its new location.
- **`rename_domain(old, new)`** renames an entire domain in place — every
  memory in it moves, and an alias from the old name is registered
  automatically. It fails if the new name already has memories (use the CLI
  `merge_domains` for that case, not an MCP tool).

### `occurred_at`

- Witnessed directly this session → set `occurred_at` without asking;
  default to today if no date was given.
- Inferred or back-dated events you did not directly observe → **propose,
  then confirm.** State the date and reasoning, wait for confirmation, only
  then set it. Never guess a historical date silently.
- Turn-boundary rule: if proposing to file something as significant, that
  proposal is the only thing in that turn. Set `occurred_at` in a follow-up
  call, after the user replies.
- Always pair `occurred_at` with `why_matters`.

### Archiving & drift protocol

- `audit(mode=stale)` surfaces drift candidates: contradictions, superseded
  labels, duplicates, stale open questions, old transient memories.
  Contradiction signals are recomputed from content on each call — which is
  why resolution must be structural (a `resolved`/`resolved_by`/`supersedes`
  edge) rather than a label edit.
- `audit(mode=orphans)` surfaces live, non-transient memories with zero
  connections. `audit(mode=archived)` lists archived memories — use it when
  `search` returns nothing but you expect content to exist.
- `audit(mode=conflicts)` surfaces candidate pairs for contradiction review,
  not confirmed contradictions — see Filing workflow above.
- **`forget(id, reason)` / `forget_all(items=[...])`** — archive only after
  explicit, unambiguous user confirmation:
  1. Only suggest archiving after `audit(mode=stale)` surfaces a candidate,
     or the user explicitly identifies something as stale.
  2. Always present the memory and ask: *"Should I archive this?"* Never
     assume yes.
  3. Wait for unambiguous confirmation. *"That's probably outdated"* is not
     confirmation.
  4. Never archive based on casual mention or implication.
  5. After archiving, report the ID(s) and note they can be restored with
     `restore`.
  Use `forget_all` (not repeated `forget` calls) once you have 2+ confirmed
  IDs — it's one atomic transaction; all archive or none do.
- `restore(id)` reverses `forget` — get the ID from `audit(mode=archived)`.
- `disconnect(id)` is a hard delete of an edge (by edge ID, from `recall`'s
  `edges` array) — no confirmation protocol built into the tool itself, but
  treat it with the same care as `forget`: it can't be undone.
- `significance(mode=trust)` ranks memories by computed epistemic trust
  (derived from `node_kind` and connected relationship types, not a
  hand-asserted score) — useful for "which of these claims should I
  believe." A `contradicts` edge lowers trust; resolving it lifts the
  penalty automatically. Only meaningful if `node_kind` is filed honestly.

### Lean output — `recall(id)` before acting on content

`orient`, `search`, `recent`, `history`, `significance`, `audit` all return
**lean** entries: `id`, `label`, and a truncated `why_matters` excerpt only —
never the full `description`. Treat these as an index, not the content.
Before quoting, citing, or acting on what a memory actually says, call
`recall(id)` for the full node plus its `edges` array.

### Search notes

- `search` is lexical (LIKE) unless Ollama is running, in which case it also
  ranks by semantic distance. Query vocabulary must match stored text — use
  words likely to appear in a label, not words describing your intent.
- Set `exact: true` for identifiers (ticket numbers, short codes) — normal
  ranking can bury an exact match under conceptually-similar results, and
  short hyphenated codes don't tokenise well for lexical matching either way.

### Version awareness

`orient` returns `server_version` in its response. If it doesn't match what
this document was last verified against, tool behaviour may have drifted —
re-check tool descriptions via `tools/list` rather than assuming this
document is still accurate.

### Tool quick reference

| Tool | When |
|---|---|
| `orient()` | Session start — cross-domain bootstrap |
| `orient(domain=X)` | Full view: `rules`, `declared_spine`, `significant`/`relevant`, `recent` |
| `orient(domains=[...])` | Same, for 1–5 domains in one call |
| `orient(domain=X, topic=Y)` | `relevant` semantically matched to a known session purpose |
| `domains()` | List active domains and aliases |
| `alias(action=...)` | Manage domain aliases: add/remove/resolve/list |
| `search(query=...)` | Find by vocabulary in stored labels/descriptions/tags |
| `recall(id)` | Full memory + connections |
| `recent()` | Where work was last happening |
| `history()` | Chronological decision spine |
| `significance()` | Dual-signal importance (declared + structural) |
| `significance(mode=trust)` | Epistemic trust ranking |
| `suggest_connections(id)` | Candidates to wire up after filing |
| `connect(...)` | Wire memories together; adjudicate contradictions via `relationship=resolved` (verify the pair via `recall` first) |
| `disconnect(id)` | Hard-delete an edge by edge ID — irreversible |
| `remember(...)` | File a new memory |
| `revise(id, ...)` | Update an existing memory; also handles single-node domain moves |
| `rename_domain(old, new)` | Rename an entire domain in place |
| `forget(id, reason)` / `forget_all(items=[...])` | Archive — confirmation required |
| `restore(id)` | Un-archive |
| `audit(mode=...)` | `stale` / `orphans` / `archived` / `conflicts` |
| `visualise(domain=X)` / `visualise(memory_id=X)` | Mermaid graph, human inspection only |
| `trace(from_id, to_id)` | Shortest connection chain between two memories — chain narration, not pair verification |
| `why_connected(from_label, to_label)` | Connections between two memories resolved by best-match label — not exact-ID, not pair verification |

`purge` and `merge_domains` are CLI-only — never call them as MCP tools;
they don't exist as one.

Do not call `orient()` repeatedly to dig for more — its sections are bounded
by design. Use `search` for anything specific.

---

## Provenance

| Version | Change | Trigger |
|---|---|---|
| v1 | Drafted as a decision (two-layer Behavioural Contract + Reference, modelled on `recordari-skill.md`), but never materialised to disk — the decision recorded intent, not content. | Closing the same documentation gap `recordari-skill.md` closed for Recordari, grounded in memoryweb's own dogfooding: the 69% binder-session orphan rate / connect-instruction-position fix, and the 90%-monochrome-decision-graph finding that motivated `node_kind` enforcement. |
| v2 (this document) | First materialised version. Adapts `recordari-skill.md` v7.1 — the `why_connected`-as-pair-verification rule, the warning-as-instruction rule, and the deployed-file audit discipline — but does **not** port it mechanically. Re-verified against memoryweb's live 21-tool surface and corrected three places where the two systems diverge: (1) pair verification uses `recall(id)`'s `edges` array, not `why_connected` — memoryweb's `why_connected` is the fuzzy label-matched tool and `trace` is the multi-hop one, the reverse of Recordari's tool split; (2) no ownership/`override_reason` section — memoryweb is single-tenant, that entire v7 addition doesn't apply; (3) `forget_all` is a real, separate memoryweb tool (unlike Recordari, where v7 replaced a nonexistent `forget_all` with batched `forget`). Added a memoryweb-only Domain move protocol section (`revise(domain=...)` vs `rename_domain`) with no Recordari analogue. | User request to adapt Recordari's v7.1 skill for memoryweb (2026-07-10). |
| v2.1 | Drafting this document surfaced a live wording bug in `remember`'s `orphan_warning`: it told agents to pass `connect` a `domain` parameter the tool has never accepted (confirmed down to `db/edges.go`'s `AddEdge(fromID, toID, relationship, narrative)` — no domain anywhere). Traced to a false premise in `stories/cross-domain-connect-ux.md` (2026-05-23) — "`connect` resolves node IDs within a domain scope" — that a later wording pass (v1.29.3) polished without re-checking, and that two regression tests (`TestRemember_OrphanWarning_PresentWhenNoConnections`, `TestRememberAll_OrphanWarning_PresentWhenNoEdges`) then locked in by asserting the wrong phrase's *presence*. Fixed same-session in `tools/remember.go` (both single and batch `orphanWarning` strings) and `tools/remember_test.go` (assertions flipped to forbid the old phrase); `go test ./...` green; committed as `300a9d2` and pushed. This document's A.4/B.4 and "Why this shape" updated to describe the fix instead of carrying it as an open quirk. | User confirmation to fix after the quirk was flagged in v2 (2026-07-10). |
