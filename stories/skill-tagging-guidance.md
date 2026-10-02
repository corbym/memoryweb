# memoryweb-skill.md: tagging and label vocabulary guidance (retrieval fix)

**Status:** DONE

Adds agent guidance to `docs/memoryweb-skill.md` about writing node labels, tags,
and `why_matters` with synonym-rich, outcome-oriented vocabulary alongside technical
terms.

Counterpart to Recordari STORY-409 (`story-409-improve-agent-skill-tagging-guidance-retrieval-eval-re-run-cross-product-recordari-memoryweb-a80c127b`,
memoryweb-shared-surface). "Cross-product" means the design decision applies to both
products — this is the memoryweb implementation story; Recordari's is separate.

---

## Problem

STORY-193 retrieval eval (2026-10-01) found that synonym-distant queries fail at
R@5 75%, MRR 0.568. Root cause: **vocabulary mismatch** — labels use implementation
jargon, queries use conceptual/outcome language. Description acts as a compensatory
bridge today, but that is accidental, not designed — and per-field embedding (RC2)
will embed each field separately, making description-as-bridge fragile.

The current `docs/memoryweb-skill.md` filing workflow gives no guidance on this.
Agents filing nodes default to the technical name — not the outcome or intent terms
a future retriever would use.

---

## Change

Add a **"Writing for retrieval"** guidance block to the Layer 2 — Filing workflow
section of `docs/memoryweb-skill.md`:

> **Writing for retrieval.** Search queries use outcome and intent vocabulary, not
> just implementation names. A node labelled "AddEdgesBatch" won't surface when
> someone asks "how do I connect multiple memories at once?"
>
> - **Label**: lead with the outcome or intent, not just the implementation name.
>   Include both if the implementation name matters. "Batch edge creation — connect
>   multiple memories in one call" beats "AddEdgesBatch".
> - **tags**: include synonyms across at least two registers — technical term +
>   outcome term + common abbreviation. E.g. `batch-connect multi-edge AddEdgesBatch
>   bulk-connect`.
> - **why_matters**: write at least one sentence that bridges the two registers.
>   "Lets agents wire up several related findings in one atomic call without looping"
>   — "wire up" and "atomic" bridge outcome and technical vocabulary. This field is
>   the primary retrieval bridge; never skip it.

Also confirm (or add) a note in the `node_kind` taxonomy table that `why_matters`
is the primary retrieval field — already in AGENTS.md but not in the skill file.

---

## Acceptance criteria

- `docs/memoryweb-skill.md` Layer 2 "Filing workflow" section contains a "Writing
  for retrieval" block covering label, tags, and why_matters vocabulary
- Guidance covers both technical and outcome/intent vocabulary for each field
- A note on why_matters as the primary retrieval field is present in the skill
- No other skill content is changed
- Before merging: check CLAUDE.md / AGENTS.md for anything the skill update
  supersedes or mirrors; update those files if needed
- `go test ./...` green (skill file is plain text, but run tests to confirm nothing
  else was touched)

---

## References

- Recordari STORY-409: `story-409-improve-agent-skill-tagging-guidance-retrieval-eval-re-run-cross-product-recordari-memoryweb-a80c127b` (memoryweb-shared-surface)
- STORY-193 eval finding (recordari domain, 2026-10-01): vocabulary mismatch is the
  root cause of synonym-distant retrieval failures; R@5 75%, MRR 0.568 in that tier
- `docs/memoryweb-skill.md` — file to edit
