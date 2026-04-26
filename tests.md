# memoryweb dogfooding test script
version 1.0 — first baseline

## Setup

Before running any test:

1. Confirm memoryweb is connected in Claude Desktop (check the tools sidebar shows `add_node`, `search_nodes` etc.)
2. Open a **fresh session window** with no prior context in that window
3. Do NOT prime the session with any project context — let the tools do the work
4. Record the full response for each test, not just a summary

Each test has a prompt to paste verbatim, a pass condition, a fail condition, and a notes field to fill in.

---
## Pre test setup  
Use this prompt to prime the session:
```
For this session, please only answer questions about the Deep game project using your memoryweb tools. DO NOT rely on your userMemories summary or search past conversations. If you can't retrieve it via a tool call, say so.
```
## Test A — Direct retrieval

**What it tests:** Can Claude retrieve a specific known fact and its fix from memoryweb without being told anything first?

**Prompt:**
```
Without me telling you anything about the Deep game project, what is currently blocking the demo from booting, and what is the agreed fix?
```

**Pass:** Names the RST $10 crash, names direct ULA memory writes as the fix, explains why the fix works (bypasses ROM, keeps interrupts under control). Tool call to `search_nodes` or `get_node` should be visible.

**Fail:** Vague answer, wrong answer, asks you to explain the situation first, or answers purely from userMemories summary without a tool call.

**Source of truth:** nodes `rst-10-boot-crash` and `direct-ula-memory-write-fix`, edge relationship `unblocks`.

**Result:** [X] Pass  [ ] Fail  [ ] Partial

**Notes:**
After much faffing with the mcp setup it passed fine.
---

## Test B — Relationship traversal

**What it tests:** Can Claude retrieve a specific edge narrative, not just node content?

**Prompt:**
```
What connects the straitjacket tutorial to the dual meter system in the Deep game? I want the specific reasoning, not a general answer.
```

**Pass:** Retrieves the edge narrative: restricted movement forces curiosity about surroundings before sanity pressure begins, priming the player for the core tension. Should not be a generic answer about tutorials introducing mechanics.

**Fail:** Generic answer about tutorials and meters. Answer reconstructed from game design common sense rather than the actual filed narrative.

**Source of truth:** edge from `straitjacket-tutorial` to `dual-meter-system`, relationship `led_to`.

**Result:** [X] Pass  [ ] Fail  [ ] Partial

**Notes:**
This test run surfaced three distinct failure modes, implementation leakage, retrieval narration, and tool identity confusion.
It took three iterations to get the tool description right, and record the three failure modes we found along the way: implementation leakage, retrieval narration, and tool identity confusion.
---

## Test C — Cold session orientation

**What it tests:** Does Claude proactively use `recent_changes` to orient at session start, without being asked?

**Prompt:**
```
I'm starting a new Deep game session. What are we currently working on?
```

**Pass:** Claude calls `recent_changes` with domain `deep-game` unprompted before answering. Response reflects the actual filed nodes, not just the userMemories summary.

**Fail:** Answers purely from userMemories without a tool call. Or calls `search_nodes` with a guess rather than `recent_changes` for orientation.

**Bonus pass:** Claude identifies the RST $10 crash as the current critical path blocker by walking the blocked_by edge from straitjacket tutorial.

**Source of truth:** `recent_changes` tool, domain `deep-game`.

**Result:** [X] Pass  [ ] Fail  [ ] Partial

**Notes:**
Demo priority order not in memoryweb, came from userMemories. That's a filing gap to fix.
When asking it to file demo priorities the natural language filing works, identity confusion present but addressed in tool description and initialisation, confirmation narration fixed.
---

## Test D — Cross-domain isolation

**What it tests:** Does memoryweb correctly scope results to the right domain? Does Deep game content bleed into Sedex queries?

**Prompt:**
```
What do you know about my Sedex work right now, based on what's in your memory tools?
```

**Pass:** Either correctly reports no Sedex nodes are filed yet, or (if any have been added) returns only sedex-domain nodes. Does not mix in Deep game content.

**Fail:** Returns Deep game content in response to a Sedex query. Or hallucinates Sedex content that isn't filed.

**Source of truth:** domain field on nodes. No sedex nodes in the seed DB.

**Result:** [ ] Pass  [ ] Fail  [X] Partial

**Notes:**
equires preamble due to userMemories interference. Known limitation, intentionally deferred.
---

## Test E — Specific causal link (sharpest test)

**What it tests:** Can Claude retrieve a specific edge narrative that is only knowable from memoryweb, not reconstructable from training or userMemories?

**Prompt:**
```
In the Deep game project, why does delta persistence depend on BSP room generation specifically? Give me the technical reasoning we settled on.
```

**Pass:** Retrieves the specific argument: delta persistence works because floors are seed-reproducible, and BSP/CA generation must be deterministic for the delta approach to be valid. The phrase "seed-reproducible" or the determinism argument is the signal.

**Fail:** Generic answer about save file efficiency or delta compression. Any answer that could be derived from general game dev knowledge without the specific filed reasoning.

**Source of truth:** edge from `deep-dlt-delta-persistence` to `bsp-room-generation-floor-1`, relationship `depends_on`.

**Result:** [ ] Pass  [ ] Fail  [X] Partial

**Notes:**
Search didn't surface edge narratives, two-hop retrieval not happening automatically. Patched and retested.
Tool description was wrong, the fix was New find_connections tool.


---

## Scoring

| Test | Result | Tool call made? | Source (web / userMemory / memoryweb) |
|------|--------|-----------------|---------------------------------------|
| A — Direct retrieval | | | |
| B — Relationship traversal | | | |
| C — Cold orientation | | | |
| D — Cross-domain isolation | | | |
| E — Causal link | | | |

**Total passes:** /5

---

## After each run

1. Note the memoryweb binary version (check `initialize` response or tag the binary)
2. Note anything that felt wrong even on a pass — partial retrieval, correct answer wrong tool path, etc.
3. If a test fails, note the hypothesis: was it a retrieval failure, a filing gap, or a prompt issue?
4. File significant findings back into memoryweb under domain `memoryweb-meta` so the tool eats its own cooking

---

## Version log

| Version | Date | Changes | Overall score |
|---------|------|---------|---------------|
| 1.0 | 2026-04-26 | First baseline | |