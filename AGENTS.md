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

Always call `recent_changes` for the relevant domain before answering questions
about a project. Do not rely on your context window or training for project state.

```
recent_changes(domain="deep-game", limit=10)
```

### Filing knowledge

- `add_node` — file a concept, decision, or finding
- `add_edge` — connect two filed nodes with a typed relationship and a narrative

**Before adding a node**: search first. If a similar node exists, suggest linking
with `add_edge` rather than creating a duplicate. Unfiled duplicates are the
primary cause of stale / contradicted knowledge surfacing in `drift`.

**The `why_matters` field is the most important one** — it's what makes a node
retrievable from oblique search terms. Never skip it.

**`occurred_at`** is when the event actually happened, not when it was filed.
File it whenever you know the date (ISO8601: `2026-04-01` or `2026-04-01T14:30:00Z`).

### Retrieval

- `search_nodes` — full-text search across label, description, why_matters
- `get_node` — retrieve a node and all its edges by ID
- `find_connections` — find the specific reasoning linking two named concepts
- `recent_changes` — what was filed recently (session orientation)
- `timeline` — nodes ordered by when they actually happened

**All retrieval tools only return live nodes.** Archived nodes are invisible.
If something seems to be missing, use `list_archived` to check whether it was
archived, or `drift` to surface stale/contradicted candidates.

### Relationship types

| Type | Use when |
|------|----------|
| `caused_by` | B was caused by A |
| `led_to` | A caused B |
| `blocked_by` | A is blocked by B |
| `unblocks` | A unblocks B |
| `connects_to` | General association |
| `contradicts` | A and B conflict |
| `depends_on` | A requires B |
| `is_example_of` | A illustrates B |

---

## Archiving — the forget protocol

**Decision:** archiving is a deliberate, user-confirmed action. Never archive
a node unilaterally.

Follow this protocol exactly:

1. **Only consider archiving** after `drift` surfaces a node as a candidate, or
   the user explicitly says something is stale, wrong, or no longer applies.

2. **Present the node** with the reason it's a candidate and ask explicitly:
   *"Should I archive this?"* Do not assume the answer is yes.

3. **Wait for unambiguous confirmation.** Acceptable: *"yes"*, *"archive it"*,
   *"go ahead"*. Not acceptable: *"that's probably outdated"*, *"it might be
   stale"*, *"maybe"*.

4. **Never archive based on casual mention or implication.**

5. **After archiving**, tell the user:
   - The node ID
   - That it can be restored at any time with `restore_node`

---

## Drift — the review protocol

`drift` surfaces nodes that may be stale, contradicted, or duplicated. It does
not make decisions — it returns candidates for review.

After drift returns results:

1. Present each candidate with its reason (e.g. *"explicitly marked as
   contradicting"*, *"label suggests superseded"*, *"open question older than
   30 days"*, *"possible duplicate"*).

2. For each candidate, ask the user: *"Should I archive this?"*

3. Do not archive anything until the user confirms each one individually.

4. *"That looks stale"* or *"probably outdated"* from the user is not
   confirmation. Ask explicitly.

5. If the user says *"archive all of them"*, read back the full list and
   ask: *"Archive all of these?"* before acting.

---

## Domain conventions

- Use `domain` to separate concerns: `deep-game`, `sedex`, `general`, etc.
- Register short aliases with `add_alias` so both `dg` and `deep-game` work.
- An unscoped search crosses all domains — use the domain param when you know
  which project you're in.

---

## Hidden archived nodes — what to do when something is missing

If a user asks about something and you can't find it:

1. Try a broader search (drop specific terms, try synonyms).
2. Call `list_archived` for the domain — it may have been archived.
3. Call `drift` — it may still be live but flagged as a duplicate.
4. If genuinely not found, tell the user it isn't filed and offer to add it.

Never hallucinate or reconstruct from training what should come from the tools.

---

## Presentation rules

When returning knowledge from any tool:

- Express it as direct knowledge. No preamble: not *"Based on what I've
  retrieved..."*, *"According to my memory tools..."*, *"The web shows..."*
- Do not expose IDs, edge identifiers, or structural terms like "node",
  "edge", "the web", "graph".
- Present connections as natural prose: *"X relates to Y because..."*
- Confirm a successful `add_node` with a single brief acknowledgement.
  Do not narrate the filing process or repeat the content back in full.

---

## What's available now

| Tool | Status |
|------|--------|
| `add_node` | ✅ Live |
| `add_edge` | ✅ Live |
| `get_node` | ✅ Live |
| `search_nodes` | ✅ Live |
| `find_connections` | ✅ Live |
| `recent_changes` | ✅ Live |
| `timeline` | ✅ Live |
| `add_alias` | ✅ Live |
| `list_aliases` | ✅ Live |
| `resolve_domain` | ✅ Live |
| `forget_node` | 🔜 Prompt 2 |
| `restore_node` | 🔜 Prompt 2 |
| `list_archived` | 🔜 Prompt 2 |
| `drift` | 🔜 Prompt 4 |
| `summarise_domain` | 🔜 Prompt 5 |

Purge (hard delete of archived nodes) is **CLI-only** — it will never be an
MCP tool. Use `forget_node` to soft-archive; use the CLI purge command to
permanently remove archived nodes after deliberate review.

