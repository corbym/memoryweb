# memoryweb user guide

A practical guide to getting the most out of memoryweb across the surfaces that support it. This is aimed at beginners — you don't need to understand the internals, just the patterns that make the tool work well.

---

## What memoryweb does (and doesn't do)

memoryweb gives an AI agent **persistent memory that outlasts a single conversation**. Without it, every session starts from scratch. With it, decisions, bugs, design choices, and open questions accumulate over time into a knowledge graph the agent can pull from on demand.

It is not a chat history tool. It is a **decision log** — it records *what was learned and why it matters*, not a transcript of what was said.

---

## Claude Code / GitHub Copilot

Claude Code and GitHub Copilot (VS Code) both support hook events that fire automatically during a session. When memoryweb is installed correctly, most of the work happens without you asking for it. This section explains what is automatic, what you need to do, and what phrases get the best results.

### What happens automatically (when hooks are installed)

Two hooks run in the background:

- **Save hook (Stop event):** every 15 AI responses, the hook blocks the next reply and asks the agent to file anything significant before continuing. You will see a brief pause while the agent calls `remember_all` and `connect_all`.
- **PreCompact hook:** fires before context compaction and asks the agent to file everything important that hasn't been filed yet. This prevents knowledge loss when the context window is about to be trimmed.

If you have run `memoryweb setup`, these are already active. Verify with:

```bash
memoryweb doctor
```

Look for `[✓] Claude hooks: Stop and PreCompact hooks installed`. If you see `[✗]`, re-run setup or check the hooks section of the README.

### Orienting the agent at the start of a session

Even with hooks installed, **start each session by orienting the agent**. The agent needs to know what project you are working on and what was filed in previous sessions. Don't assume it remembers.

**Opening prompt — general:**
> "Before we start: call `orient` for the `<your-domain>` domain and tell me where things stand."

**Opening prompt — if you don't know what domains exist:**
> "Call `list_domains` to see what's in memory, then call `orient` on the most relevant one and summarise it for me."

**Opening prompt — to see recent activity:**
> "Call `recent` and show me what was filed in the last few days, grouped by domain."

These phrases trigger the agent to pull from memoryweb *first*, before relying on its training or your description. This is important — without them, the agent may answer from general knowledge rather than your specific project history.

### Searching during a session

When you want the agent to look something up rather than guess:

> "Search memoryweb for `<topic>` and tell me what you find."

> "What do we know about `<topic>`? Check memory first."

> "Is there anything in memory about why we made the decision to `<description>`?"

> "Use `why_connected` to explain how `<concept A>` relates to `<concept B>`."

The phrase **"check memory first"** is useful as a general habit when you notice the agent answering from training rather than from your project's history.

### Filing knowledge during a session

The hooks handle periodic filing, but you can prompt manually at any time — especially after a significant decision, discovery, or completed piece of work:

> "File that decision to memoryweb. Make sure `why_matters` explains the reasoning, not just what we decided."

> "Remember this bug and its fix. Connect it to the affected component."

> "We just figured out why `<thing>` was broken. File that as a finding in the `<domain>` domain."

> "File a note that this is an open question — we haven't decided yet."

Good `why_matters` content is the difference between a node that can be found later and one that can't. Push the agent to include the reasoning:

> "The `why_matters` should explain *why this decision was made*, not just repeat the label."

### Connecting related memories

Connections are what make the graph navigable. Prompt the agent to connect new nodes to existing ones:

> "Connect that to the `<related topic>` node — the relationship is `depends_on` because `<reason>`."

> "After filing, check `suggest_connections` and add any that look right."

> "Those two things are related — file a connection between them explaining why."

### Cleaning up stale knowledge

Over time, some nodes become outdated. Run a drift review periodically:

> "Run `whats_stale` on the `<domain>` domain and show me the candidates."

The agent will present each candidate and ask whether to archive it. You confirm each one individually — the agent will not archive anything without your explicit approval.

To see what has already been archived:

> "Show me what's been forgotten in the `<domain>` domain."

### Transient notes

For short-lived state — sprint tickets, temporary blockers, current branch names — use the `transient` flag so drift surfaces them automatically when they go stale:

> "File this as a transient note — it's only relevant for the current sprint."

---

## Claude Desktop

Claude Desktop does not support hooks. There is no automatic filing — you need to prompt the agent explicitly. The patterns below are designed to be lightweight: a couple of sentences at the start and end of each session covers the essential workflow.

### Orienting the agent at the start of a session

Add one of these as your **first message** in every conversation:

**If you know the domain:**
> "You have access to memoryweb. Call `orient` for the `<your-domain>` domain before we start and tell me what you know."

**If you're not sure what domains exist:**
> "You have access to memoryweb. Call `list_domains` to see what's there, then call `orient` on the most relevant domain and summarise the current state."

**For a broader catch-up:**
> "You have access to memoryweb. Call `recent` with `group_by_domain=true` and tell me what's been filed recently."

Without this, Claude Desktop has no way to know memoryweb exists or that you want it consulted. **The first message sets the frame for the whole conversation.**

### System prompt (optional but recommended)

If you use the same domain regularly, add a system prompt so the agent orients automatically without you having to ask. In Claude Desktop, go to **Settings → Profile** and add something like:

```
You have access to a persistent memory tool called memoryweb.
At the start of every conversation, call orient(domain="<your-domain>")
and use what you find to inform your answers before relying on general knowledge.
When we make decisions, discover bugs, or resolve open questions, file them
with remember() — always fill in why_matters with the reasoning, not just a summary.
```

Replace `<your-domain>` with your project name (e.g. `my-app`, `work`, `research`).

### Searching during a session

The same phrases work as in Claude Code:

> "Search memoryweb for `<topic>`."

> "What do we know about `<topic>`? Check memory before answering."

> "Use `why_connected` to explain how `<concept A>` relates to `<concept B>`."

> "Look up the history of `<topic>` in memory and summarise it."

### Filing at the end of a session

Without hooks, you need to prompt filing manually. A good habit is to end every session with one of these:

> "Before we finish — file anything significant from this conversation to memoryweb. Focus on decisions made, bugs found, and open questions. Make sure each node has a `why_matters` that explains the reasoning."

> "We've covered a lot. File the key findings and connect any related nodes. Use the `<domain>` domain."

> "File this decision to memory. Connect it to anything relevant."

If the session was short or nothing important was decided:

> "Nothing to file from this session."

Getting into the habit of ending sessions this way means the knowledge graph accumulates steadily rather than only during sessions where you remembered to prompt at the start.

### Checking what was filed

After filing, you can verify what was stored:

> "Show me what was just filed — call `recent` and summarise."

> "Call `recall` on the node you just created and confirm the `why_matters` is correct."

---

## Key phrases at a glance

| What you want | Phrase |
|--------------|--------|
| Orient at session start | `"Call orient for the <domain> domain before we start."` |
| Discover available domains | `"Call list_domains and tell me what's there."` |
| Search memory | `"Search memoryweb for <topic>."` |
| Prefer memory over training | `"Check memory first."` |
| Trace a relationship | `"Use why_connected to explain how X relates to Y."` |
| File a decision | `"File that decision to memoryweb with why_matters explaining the reasoning."` |
| File an open question | `"File this as an open question in the <domain> domain."` |
| File a bug or fix | `"Remember this bug and its fix. Connect it to the affected component."` |
| Connect two nodes | `"Connect those two — relationship is <type> because <reason>."` |
| Review stale knowledge | `"Run whats_stale on the <domain> domain."` |
| End-of-session filing (Desktop) | `"Before we finish — file anything significant from this session."` |

---

## Common beginner mistakes

**Letting the agent answer from training instead of memory**  
If you ask "what did we decide about X?" without directing the agent to check memory, it may answer from general knowledge or say it doesn't know. Always include "check memory" or "search memoryweb" when you want a project-specific answer.

**Skipping `why_matters`**  
A node without a `why_matters` is nearly impossible to find later from an oblique search. Push the agent to fill it in every time. The `why_matters` should answer: *if someone reads this six months from now with no context, why would they care?*

**Not orienting at session start**  
The graph only helps if the agent is told to consult it. Without an explicit orientation step, the agent has no reason to pull context from memoryweb.

**Filing everything as one flat blob**  
One decision or finding per node. Multiple connected nodes are far more useful than one large node trying to capture a whole session. The connections between nodes are where the value is.

**Forgetting to connect**  
A node with no connections is an island — it can only be found by direct search. After filing, ask the agent to connect the new node to related ones.
