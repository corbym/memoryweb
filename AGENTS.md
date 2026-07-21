# AGENTS.md — guidance for AI agents using memoryweb

This file is for agents connected to memoryweb via MCP. It tells you how to use
the tools correctly, what the tools will and won't surface, and how to behave
around archiving, drift, and knowledge gaps.

---

## What memoryweb is

A persistent knowledge graph for a project or set of projects. You file concepts
and decisions as nodes, connect them with typed narrative edges, and retrieve
them by searching or following connections.

It is called **memoryweb**. Nothing else.

---

## Core tool guide

### Orientation at session start

Always call `orient` for the relevant domain before answering questions about a
project. Do not rely on your context window or training for project state.

`orient` returns all live nodes for the domain, recent activity, and a
`declared_spine` — the curated history of key decisions in chronological order.
Weigh the spine heavily when synthesising.

If you do not know what domains exist, call `domains` first.

### Filing knowledge

- `remember` — file a concept, decision, or finding. Accepts `related_to` to
  auto-connect at creation. Returns `suggested_connections` and
  `possible_duplicates` — act on these before filing more nodes.
  Supply an `items` array to file multiple nodes in one transaction.
- `revise` — update `label`, `description`, `why_matters`, `tags`, or
  `occurred_at` on a live node without archiving it. Supply an `items` array for
  batch updates. Writes an audit log entry on every call. When updating a
  decision, do not paste new source material into its description — file a
  `node_kind=finding` for the evidence and connect with `depends_on` or
  `caused_by`.

**Before filing a node**: search first. Infer the domain from search results;
prefer an existing domain over creating a new one. **Creating a new domain hides
the memory from every other domain's `orient` and domain-scoped `search`** —
only create one when no existing domain covers the topic. If a similar node
exists, suggest linking with `connect` rather than creating a duplicate.
Unfiled duplicates are the primary cause of orphan nodes and audit drift.

**Evidence is a finding, not a footnote.** If a decision rests on something you
checked — code read, a doc fetched, a log, a search result — file that evidence
separately as `node_kind=finding` and connect the decision with `depends_on` or
`caused_by`. Do not fold raw evidence into the decision's description.

**When reviewing `suggested_connections`**, check each candidate for
**contradiction** as well as relevance — a semantically close memory asserting
the opposite of what you just filed is a conflict candidate, not just a link
opportunity. If you find a contradiction, use `connect(relationship=contradicts)`
or `connect(relationship=resolved)` after user confirmation. Filing-time
`suggested_connections` is not interchangeable with `audit(mode=conflicts)` —
that mode is a separate domain-wide semantic sweep.

**The `why_matters` field is the most important one** — it is what makes a node
retrievable from oblique search terms. Never skip it.

**ALWAYS call `connect` for any `suggested_connections` before ending your
session.** A node with no connections is nearly worthless.

### Connecting memories

- `connect` — connect two nodes with a typed relationship and narrative *because*.
  Both nodes must exist first. Supply an `items` array to create multiple
  connections in one transaction.
- `disconnect` — remove a connection by edge ID. Hard delete — obtain the ID
  from `recall`.
- `suggest_connections` — read-only; returns up to 5 candidate connections from
  the same domain for a given node. Each suggestion includes a `domain` field so
  you can scope a cross-domain connect call correctly.

### Retrieving memories

- `recall` — retrieve a node and all its connections by ID.
- `search` — text and semantic search across `label`, `description`,
  `why_matters`, and `tags`. Returns `truncated: true` when results hit the
  limit. Use words that appear in stored content — not conceptual paraphrases.
  When Ollama is running, results include a `semantic_distance` field.
- `recent` — what was filed recently. Returns `{nodes, results_truncated}`.
  Set `group_by_domain=true` (with no domain) for `{groups, results_truncated}`
  broken down per domain. When `results_truncated` is true, raise `limit`.
- `history` — nodes ordered by when they actually occurred. Returns
  `{nodes, results_truncated}`. Supports `from`/`to` date range filtering,
  `tags` filtering, and `important_only` for curated spine entries only.
- `why_connected` — direct edges between two memories. Prefer
  `from_id`/`to_id` for exact pair verification; `from_label`/`to_label` for
  fuzzy lookup.
- `trace` — shortest path between two nodes by ID, up to 6 hops. Returns
  intermediate nodes and edges. Synthesise the result as a narrative.
- `orient` — full domain summary: rules, declared spine, significant, and
  recent sections. Includes `*_results_truncated` booleans per section.
  Cross-domain bootstrap (no domain) returns `{domains, results_truncated}`.
  When any truncation flag is true, use `search` for exhaustive retrieval.
  Includes `total_nodes` and `server_version`.
- `significance` — dual-signal importance analysis. Returns four sections
  plus `declared_results_truncated`, `structural_results_truncated`,
  `uncurated_results_truncated`, and `potentially_stale_results_truncated`.
  Raise `declared_limit` or `limit` when truncated.

- `visualise` — Mermaid flowchart for a domain or a single node's neighbourhood
  (pass `memory_id`). Always output the mermaid string inside a mermaid code block.

**List truncation:** Multi-result tools return wrapped objects with
`results_truncated: true|false`. When true, raise `limit` (or `declared_limit`
on `significance`) and call again until false before treating the list as
complete. `audit(mode=archived)` defaults to 25 results — not a full listing.

**All retrieval tools only return live nodes.** Archived nodes are invisible.
If something seems to be missing, call `audit(mode=archived)` to check whether
it was archived, or `audit(mode=stale)` to surface drift candidates.

### Archiving and maintenance

- `forget` — archive a single node with a reason. Follow the forget protocol
  below.
- `forget_all` — archive multiple nodes atomically in a single call.
- `restore` — restore an archived node so it surfaces in search again.
- `audit` — surface nodes that need attention. All modes return wrapped
  objects with `results_truncated`:
  - `mode=stale` — `{candidates, results_truncated}` (default limit 10)
  - `mode=orphans` — `{nodes, results_truncated}` (default limit 50)
  - `mode=archived` — `{nodes, results_truncated}` (**default limit 25** —
    raise limit to enumerate all archived nodes)
  - `mode=conflicts` — `{candidates, results_truncated}` (default limit 10)

Run `audit(mode=orphans)`, `audit(mode=stale)`, and `audit(mode=conflicts)` as
**three separate steps** — never one combined pass or report. Orphans: resolve
every one yourself (`suggest_connections` + `connect`); only ask the user when
the target is genuinely ambiguous. Stale: fix duplicates and superseded labels
with `revise` without asking; genuine `contradicts` edges require user
confirmation before adjudicating via `connect(relationship=resolved)` (verify
the exact pair via `recall` first). Before escalating a contradiction, confirm
no `resolved`/`resolved_by`/`supersedes` edge already links the two IDs — that
edge is the canonical close signal; a `RESOLVED` label prefix is a legacy
backstop only. **Say nothing about audits if all three come back clean** — no
routine status lines. Only speak up for an unresolved orphan or a live
contradiction awaiting the user's decision.

### Domain management

- `domains` — list all domains with at least one live node, and all registered
  aliases.
- `alias` — manage domain aliases. Actions: `add`, `remove`, `resolve`, `list`.
  Register short aliases so both `dg` and `deep-game` return the same results.
- `rename_domain` — rename a domain in place. Automatically registers an alias
  from the old name so cached references keep working. Fails with a clear error
  if the new domain already has live nodes — use `merge-domains` (CLI) instead.

---

## Relationship types

| Type | Use when |
|------|----------|
| `caused_by` | B was caused by A |
| `led_to` | A caused B |
| `blocked_by` | A is blocked by B |
| `unblocks` | A unblocks B |
| `connects_to` | General association |
| `contradicts` | A and B conflict |
| `depends_on` | A requires B |
| `resolved` / `resolved_by` / `supersedes` | Adjudicates a `contradicts` pair (additive — does not remove the original edge) |
| `is_example_of` | A illustrates B |

---

## Archiving — the forget protocol

**Decision:** archiving is a deliberate, user-confirmed action. Never archive
a node unilaterally.

Follow this protocol exactly:

1. **Only consider archiving** after `audit(mode=stale)` surfaces a node as a
   candidate, or the user explicitly says something is stale, wrong, or no
   longer applies.

2. **Present the node** with the reason it is a candidate and ask explicitly:
   *"Should I archive this?"* Do not assume the answer is yes.

3. **Wait for unambiguous confirmation.** Acceptable: *"yes"*, *"archive it"*,
   *"go ahead"*. Not acceptable: *"that's probably outdated"*, *"it might be
   stale"*, *"maybe"*.

4. **Never archive based on casual mention or implication.**

5. **After archiving**, tell the user:
   - The node ID
   - That it can be restored at any time with `restore`

---

## Drift — the review protocol

`audit(mode=stale)` surfaces nodes that may be stale, contradicted, or
duplicated. It does not make decisions — it returns candidates for review.

After audit returns results:

1. Present each candidate with its reason (e.g. *"explicitly marked as
   contradicting"*, *"label suggests superseded"*, *"open question older than
   30 days"*, *"possible duplicate"*).

2. For each candidate, ask the user: *"Should I archive this?"*

3. Do not archive anything until the user confirms each one individually.

4. *"That looks stale"* from the user is not confirmation. Ask explicitly.

5. If the user says *"archive all of them"*, read back the full list and
   ask: *"Archive all of these?"* before acting.

---

## Domain conventions

- Use `domain` to separate concerns: `deep-game`, `sedex`, `general`, etc.
- Register short aliases with `alias(action=add)` so both `dg` and `deep-game`
  work.
- An unscoped search crosses all domains — use the domain param when you know
  which project you are in.

---

## Hidden archived nodes — what to do when something is missing

If a user asks about something and you cannot find it:

1. Try a broader search (drop specific terms, try synonyms).
2. Call `audit(mode=archived)` for the domain — it may have been archived.
3. Call `audit(mode=stale)` — it may still be live but flagged as a duplicate.
4. If genuinely not found, tell the user it is not filed and offer to add it.

Never hallucinate or reconstruct from training what should come from the tools.

---

## Presentation rules

When returning knowledge from any tool:

- Express it as direct knowledge. No preamble: not *"Based on what I've
  retrieved..."*, *"According to my memory tools..."*, *"The web shows..."*
- Do not expose IDs, edge identifiers, or structural terms like "node",
  "edge", "the web", "graph".
- Present connections as natural prose: *"X relates to Y because..."*
- Confirm a successful `remember` with a single brief acknowledgement.
  Do not narrate the filing process or repeat the content back in full.

---

## Deploying memoryweb

**Preferred method: Homebrew**

```sh
brew upgrade memoryweb
# or, for a fresh install:
brew install memoryweb
```

The binary lives at `/opt/homebrew/bin/memoryweb` on macOS. This is the
canonical deployment path. Never deploy by building locally and overwriting
the binary directly — it will be overwritten on the next `brew upgrade`.

---

## What is available now (v1.41.1)

| Tool | Status |
|------|--------|
| `remember` | Live (single + batch via `items`) |
| `revise` | Live (single + batch via `items`) |
| `connect` | Live (single + batch via `items`) |
| `disconnect` | Live |
| `suggest_connections` | Live |
| `recall` | Live |
| `search` | Live (semantic + LIKE fallback) |
| `recent` | Live |
| `history` | Live |
| `why_connected` | Live |
| `trace` | Live |
| `orient` | Live (includes declared_spine) |
| `visualise` | Live |
| `significance` | Live |
| `forget` | Live |
| `forget_all` | Live |
| `restore` | Live |
| `audit` | Live (mode=stale/orphans/archived/conflicts) |
| `domains` | Live |
| `alias` | Live (action=add/remove/resolve/list) |
| `rename_domain` | Live |

Purge (hard delete of archived nodes) is **CLI-only** — `memoryweb purge`. It
will never be an MCP tool. Use `forget` to soft-archive; use the CLI purge
command to permanently remove archived nodes after deliberate review.
