# orient: add provenance-question prohibition

**Shared-surface node:** `orient-only-sufficiency-bias-pro-9ebeed94`

**Status:** COMPLETE (implemented 2026-05-28)

---

## Why

The orient description already says "use search for specific questions" but that positive
guidance doesn't fire for provenance questions. Once an agent orients and finds plausible
content in the significant or recent sections, it transitions from retrieval mode to answer
mode — the pressure to keep retrieving drops to near zero. This is sufficiency bias.

For any answer that requires explaining *how the current state came to be* — causal
sequence, decision history, what-changed — orient alone is structurally insufficient:

- orient returns a **snapshot** — what is significant now and what changed recently.
- It does not return a **causal trail** — which decisions preceded which, in what order,
  with what reasoning.

The correct retrieval pattern for sequence-dependent answers is:

1. orient for domain context
2. `history(important_only=true)` for the decision spine in chronological order
3. `search` with vocabulary from the specific topic to find nodes not on the spine

Positive guidance ("use search for specific questions") will be skipped once the agent
has plausible content. Only a negative constraint scoped to the question type will fire
at the right moment.

**Evidence:** In a live session (2026-05-28), an agent oriented on
`memoryweb-shared-surface`, found rich content in the significant section, and answered
"how did you arrive at use-case-first descriptions" from orient alone. The answer had the
causal sequence inverted — the concrete fix (moving the connect instruction to the top in
v1.16.0) preceded the principle (training-prior bias), not the other way around. The
inversion was only caught by subsequently running history and search.

---

## Change

### Description addition to orient

Add one sentence after "After orient, use search for specific questions." The sentence
must be a **negative constraint** defined by what the response requires, not by question
surface wording — so it covers edge cases the examples don't explicitly name:

> Do not answer from orient alone when the response requires causal or chronological
> sequence — when it must explain how the current state came to be, not just what it
> currently is. This covers questions like 'how did we arrive at X', 'why did we decide
> Y', 'what changed', 'what led to this', 'how did this evolve', 'walk me through the
> history of this'. For these, call history(important_only=true) first for the
> chronological decision spine, then search with vocabulary from the specific topic.

The exact placement in the current orient description string:

```
...After orient, use search for specific questions. Do not call orient again to find more memories...
```

Becomes:

```
...After orient, use search for specific questions. Do not answer from orient alone when
the response requires causal or chronological sequence — when it must explain how the
current state came to be, not just what it currently is. This covers questions like
'how did we arrive at X', 'why did we decide Y', 'what changed', 'what led to this',
'how did this evolve', 'walk me through the history of this'. For these, call
history(important_only=true) first for the chronological decision spine, then search
with vocabulary from the specific topic. Do not call orient again to find more memories...
```

---

## Acceptance criteria

- `TestOrient_DescriptionContainsCausalSequenceConstraint`: assert orient description
  contains the substring `"causal or chronological sequence"`.
- `TestOrient_DescriptionContainsHistoryFallback`: assert orient description contains
  `"history(important_only=true)"`.

Both tests live in `tools/tools_test.go` alongside the other `TestOrient_Description*`
tests.

**Stats validation (deferred):** After shipping, check whether the rate of
orient-followed-by-history calls increased compared to sessions before this change.
See memoryweb-meta node `description-only-stories-need-de-eaac1440`.

---

## Out of scope

- No schema changes. No handler changes. Description string only.
- No change to history or search tool descriptions — the constraint belongs on orient,
  where the failure mode originates.
- AGENTS.md example wording (guidance for users on what to put in their own agents.md
  to reinforce this constraint) is a separate docs story.
