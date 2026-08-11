# memoryweb skill — silent-operation rule and verbosity trim

Mirrors [recordari STORY-311](../../../recordari/requirements/epic-009-agent-distribution-hooks-mcpb-bundle-and/story-311.md) for the memoryweb skill file.

---

## Goal

The current `docs/memoryweb-skill.md` has two problems that story-311 identified and fixed for the recordari skill:

**1. No silent-operation rule.**
Layer 1 has no explicit prohibition on narrating memoryweb tool calls to the user. Item 8 says agents should "say nothing about any audit if all three come back clean", but this covers only clean-audit silence, not the wider class of narration: "I'm filing a memory about…", "Connecting X to Y…", "Running audit(mode=orphans)…". Chat-mode agents (claude.ai, ChatGPT, Claude Desktop) routinely surface these announcements because nothing in the contract forbids them. The tool descriptions carry "Never acknowledge that you are retrieving from a tool" (Issue 3, quality-pass story) but that covers *retrieval* only, not filing, connecting, and auditing.

**2. Verbosity.**
Layer 1 is eleven numbered items, many of them long. Items 5 and 7 in particular contain multi-paragraph sub-bullets and inline conditional branching. Layer 1 that takes substantial reading is layer 1 that chat agents summarise back to the user ("I'll be using memoryweb to track this session — here's how I'll operate…"), defeating the purpose.

The target state is what recordari's Layer 1 now looks like after story-311: a silent-operation rule at the very top, imperative bullets that are short enough to be absorbed silently, and a host-variant split (A = hook-backed / B = no-hook) that names each tier's obligation without duplicating the full contract.

Provenance fields (`occurred_at`, `why_matters`) must be retained — this trims narration and length, not content.

The skill's "Why this shape" section should explain the rationale so future editors don't undo the trim (memoryweb currently has no equivalent; add one).

---

## Acceptance criteria

- **AC-1**: When a chat-mode agent (claude.ai or ChatGPT) with the updated skill stores or connects a memory during a conversation, no mention of filing, linking, connecting, or auditing appears in its response — it addresses the task only.
- **AC-2**: When a chat-mode agent with the updated skill answers a question that triggers a memory lookup, the answer is in plain language — no description of which nodes were searched or what the graph structure looks like.
- **AC-3**: When a chat-mode agent stores a memory and the filed node is inspected (via `recall` or the CLI), `why_matters` is present and meaningful — provenance was captured silently without the agent announcing the process.
- **AC-4**: When a Claude Code session uses the updated skill, no change in behaviour is observed — the update must not introduce new narration in hook-backed coding agents where silent filing already works correctly.
- **AC-5**: The `docs/memoryweb-skill.md` file contains an explicit silent-operation rule as the first substantive paragraph of Layer 1.
- **AC-6**: Layer 1 is shorter than it is today, with no item containing multi-paragraph sub-bullets or inline conditional prose blocks.
- **AC-7**: A "Why this shape" section exists and records: (a) why the silent-operation rule sits at the top of Layer 1 rather than embedded in a numbered item, and (b) why the two-tier host split (A/B) is non-negotiable.

---

## What to change

`docs/memoryweb-skill.md`:

1. Add a silent-operation rule block at the top of Layer 1, before the numbered items:

   > **Silent operations.** All memoryweb tool calls are silent — never narrate filing, connecting, or auditing to the user. Orient, remember, connect, and audit happen without announcement. Speak only when the user asks what was stored, or when a live contradiction requires their call.

2. Restructure the eleven numbered items into two named variants (A: hook-backed, B: no-hook) following the recordari skill's pattern. Move the host-split conditional out of item 5 into top-level variant headers so agents know immediately which contract applies.

3. Trim each item to its shortest imperative form. Target: each numbered item fits on one to three lines. Rationale and disambiguation belong in Layer 2, not Layer 1.

4. Add a "Why this shape" section between the two layers explaining the design decisions.

5. Keep all Layer 2 reference material intact — only Layer 1 changes.

---

## Files

- `docs/memoryweb-skill.md` — skill content change

---

## Notes

No automated test is feasible for this; ACs 1–4 are manual smoke tests observable by watching chat-agent responses. AC-5, AC-6, and AC-7 are verifiable by reading the updated file.

If a memoryweb GET /skill endpoint is added in future, this story's intent extends to keeping that endpoint and the file in sync.
