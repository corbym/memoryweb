# Standing rules — persistent constraints agents see every session

memoryweb v1.27.0 introduced `decision_type=standing`. This page explains what it
is, why it's useful, and how to set it up for different kinds of work.

---

## Contents

1. [What a standing rule is](#what-a-standing-rule-is)
2. [How the orient rules section works](#how-the-orient-rules-section-works)
3. [Writing labels agents can act on immediately](#writing-labels-agents-can-act-on-immediately)
4. [Templates by role](#templates-by-role)
   - [Solo developer](#solo-developer)
   - [Team tech lead](#team-tech-lead)
   - [Research / writing](#research--writing)
   - [Product / PM](#product--pm)
5. [Instructing an agent to file rules on your behalf](#instructing-an-agent-to-file-rules-on-your-behalf)
6. [How rules strengthen over time](#how-rules-strengthen-over-time)
7. [Worked example — orient with and without rules](#worked-example--orient-with-and-without-rules)

---

## What a standing rule is

A **standing rule** is a decision you've made that should hold until you explicitly
change it — not just for this session, but for every future session in that domain.

Examples:
- "All tests must be written before production code."
- "Decisions about authentication belong in the `auth-decisions` domain."
- "Meeting notes are transient — archive them after 7 days."
- "Every research finding needs a source linked as `is_example_of`."

In memoryweb, a standing rule is just a memory filed with `decision_type: standing`:

```
"File this as a standing rule: all code reviews must link to the ticket they review."
```

Standing memories appear in the **rules section** at the top of every `orient`
response for that domain — before `declared_spine`, before `significant`, before
`recent`. An agent that orients will see your rules without being asked.

The three `decision_type` values:

| Value | Lifespan | Example |
|---|---|---|
| `decision` (default) | Indefinite — a fact or finding | "Chose PostgreSQL over MySQL for audit log" |
| `transient` | Days — archived after 7 days | "Sprint 42 ticket notes" |
| `standing` | Until explicitly revised | "All DB migrations must be append-only" |

---

## How the orient rules section works

When you call `orient` on a domain, the response now contains:

```json
{
  "rules": [...],
  "declared_spine": [...],
  "significant": [...],
  "recent": [...]
}
```

`rules` is omitted entirely when no standing memories exist in the domain.

Each entry is in lean format: **id**, **label**, and a short excerpt of
`why_matters`. Rules are ordered by total inbound edge count — the more other
memories reference a rule, the higher it ranks. A freshly filed rule with zero
references appears last; as work connects back to it, it rises.

This means **you don't need to keep reminding the agent of your constraints** —
they are embedded in the structure of the domain and surface automatically.

---

## Writing labels agents can act on immediately

The orient rules section only shows the **label** (truncated to ~150 chars) plus a
short `why_matters` excerpt. An agent reading your rules at session start decides
what to do from this alone.

**Write labels as imperative directives, not noun phrases.**

| Weak (noun phrase) | Strong (imperative directive) |
|---|---|
| "TDD requirement" | "Write the failing test before any production code — always" |
| "Commit message standard" | "Commit messages must include the story ID; never commit without one" |
| "Domain naming" | "New domains need a confirm from me before filing — propose, don't assume" |
| "Code review link policy" | "Every code review memory must link to its ticket using `is_example_of`" |

**Add a self-referencing directive** so agents know to feed back into the rule:

```
"Follow TDD on every change — when a story ships, connect it back here
with is_example_of so this rule stays at the top of orient"
```

The phrase "connect it back here" is what makes the rule self-reinforcing: each
session that ships something and connects back increases the inbound edge count,
which keeps the rule ranked at the top of orient permanently.

---

## Templates by role

Use these as starting points. File each with `decision_type: standing` in the
relevant domain.

### Solo developer

```
"Write the failing test first, never production code — connect each shipped
 change back here as is_example_of to keep this rule visible"

"All schema migrations are append-only — never edit an existing migration entry"

"Before starting any feature, orient in the domain and check for related
 standing rules — file this as governed_by on the feature memory"

"Commit messages must reference the story or issue ID — bare commits are not allowed"

"Archive sprint/ticket notes after the sprint closes — they are transient by default"
```

### Team tech lead

```
"All architectural decisions must be filed in the 'architecture' domain with
 occurred_at set — they belong on the spine, not in ephemeral notes"

"Any decision that affects more than one service must be cross-linked to
 both service domains using connects_to"

"Onboarding docs I own live in the 'onboarding' domain — connect back here
 whenever I update them so this rule stays ranked"

"Breaking changes must be flagged with tags='breaking-change' so audit can
 surface them — always apply this tag to any memory that removes public behaviour"

"When a decision is reversed, file the reversal as a new memory and connect
 it to the original with contradicts — never archive the original without a replacement"
```

### Research / writing

```
"Every finding needs a source — use is_example_of to link findings to their
 source memory; unlinked findings cannot be cited"

"Competing interpretations of the same evidence are connected with contradicts
 — never merge them into one memory, preserve the tension"

"Working notes are transient — mark them as decision_type=transient when filed
 so they surface for archiving after 7 days"

"The 'spine' of this domain is the argument, not the bibliography — file key
 thesis decisions with occurred_at so they appear in the declared_spine"

"Before writing a synthesis, call history(important_only=true) on the domain —
 do not summarise from orient alone; connect back here when you do"
```

### Product / PM

```
"User research findings live in 'research' domain — product decisions that
 are caused_by research must be connected using caused_by"

"Every shipped feature has a memory with occurred_at set — if it's not on
 the spine, it didn't ship"

"Decisions about what NOT to build are as important as what to build — file
 them with tags='deferred' and a clear why_matters explaining the tradeoff"

"Before a planning session, orient in the product domain and run
 significance — the highest-ranked memories are the constraints; respect them"

"OKRs and goals are transient by the end of the quarter — set decision_type=transient
 when filing quarterly notes so they surface for archiving after the quarter ends"
```

---

## Instructing an agent to file rules on your behalf

You don't have to write these yourself. Tell the agent what constraint you want to
enforce and ask it to file it correctly:

> "File a standing rule for this domain: all DB queries must use parameterised
> statements, no string concatenation. Write the label as a directive agents
> can act on, add a self-referencing connect instruction in the description,
> and use decision_type=standing."

> "I always want agents to connect completed work back to the TDD rule in this
> domain. File that as a standing rule with a label that makes it obvious."

> "Create a standing rule that any memory about external APIs must include
> the API version in the tags field."

The agent will choose a label, write a `why_matters`, and file it. Review the
label — if it reads as a noun phrase, ask the agent to rewrite it as an imperative.

---

## How rules strengthen over time

A newly filed rule has zero inbound edges and ranks last in the rules section.
As sessions complete work that references the rule, edges accumulate:

```
session 1: "shipped auth story — connected back to TDD rule"  →  1 inbound edge
session 2: "shipped migration story — connected back to TDD rule"  →  2 inbound edges
session N: TDD rule is at the top of every orient, permanently
```

You don't need to maintain this manually. As long as your rules carry the
self-referencing directive ("connect back here when you ship"), agents accumulate
the signal automatically across sessions.

**A standing rule with zero connections is visible but fragile** — it can be
pushed down by new rules filing later. Seed it with a connection or two immediately:

```
"Connect the TDD rule to the test architecture memory using connects_to —
 they are part of the same constraint."
```

---

## Worked example — orient with and without rules

**Without standing rules:**

```json
{
  "declared_spine": [...],
  "significant": [...],
  "recent": [...]
}
```

The agent has to infer constraints from the spine and recent history. It may miss
something or apply a default it wouldn't use if it knew your preferences.

**With standing rules:**

```json
{
  "rules": [
    {
      "id": "tdd-is-mandatory-...",
      "label": "Write the failing test first — connect each shipped story back here as is_example_of",
      "why_matters": "Agents treat this as an in-the-moment instruction without it. With it, the constraint is structural."
    },
    {
      "id": "migrations-append-only-...",
      "label": "All schema migrations are append-only — never edit an existing entry",
      "why_matters": "Editing a migration that has already been applied will corrupt databases that have already run it."
    }
  ],
  "declared_spine": [...],
  "significant": [...],
  "recent": [...]
}
```

The agent reads the rules before anything else. It doesn't need to be told to follow
TDD or to avoid editing migrations — it knows before the conversation starts.

**The rules section is the difference between an agent that happens to follow your
conventions and one that always follows them.**
