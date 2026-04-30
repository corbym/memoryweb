# memoryweb user guide

A practical guide to getting the most out of memoryweb across the surfaces that support it. This is aimed at beginners — you don't need to understand the internals, just the patterns that make the tool work well.

---

## What memoryweb does (and doesn't do)

memoryweb gives an AI agent **persistent memory that outlasts a single conversation**. Without it, every session starts from scratch. With it, decisions, bugs, design choices, and open questions accumulate over time into a knowledge base the agent can pull from on demand.

It is not a chat history tool. It is a **decision log** — it records *what was learned and why it matters*, not a transcript of what was said.

---

## Claude Code / GitHub Copilot

Claude Code and GitHub Copilot (VS Code) both support hook events that fire automatically during a session. When memoryweb is installed correctly, most of the work happens without you asking for it. This section explains what is automatic, what you need to do, and what phrases get the best results.

### What happens automatically (when hooks are installed)

Two hooks run in the background:

- **Save hook (Stop event):** every 15 AI responses, the hook pauses the session and asks the agent to save anything significant before continuing. You will see a brief pause while the agent files its findings.
- **PreCompact hook:** fires before context compaction and asks the agent to save everything important that hasn't been saved yet. This prevents knowledge loss when the context window is about to be trimmed.

If you have run `memoryweb setup`, these are already active. Verify with:

```bash
memoryweb doctor
```

Look for `[✓] Claude hooks: Stop and PreCompact hooks installed`. If you see `[✗]`, re-run setup or check the hooks section of the README.

### Orienting the agent at the start of a session

Even with hooks installed, **start each session by orienting the agent**. The agent needs to know what project you are working on and what was saved in previous sessions. Don't assume it remembers.

**Opening prompt — general:**
> "Before we start, check what you know about `<project>` from memory and give me a summary of where things stand."

**Opening prompt — if you don't know what projects exist in memory:**
> "What projects do you have in memory? Check and tell me, then summarise the most relevant one."

**Opening prompt — to see recent activity:**
> "Show me what's been added to memory recently, grouped by project."

These phrases direct the agent to pull from memoryweb *first*, before relying on its training or your description. This is important — without them, the agent may answer from general knowledge rather than your specific project history.

### Searching during a session

When you want the agent to look something up rather than guess:

> "Search memory for `<topic>` and tell me what you find."

> "What do we know about `<topic>`? Check memory first."

> "Is there anything in memory about why we decided to `<description>`?"

> "How does `<concept A>` relate to `<concept B>`? Look it up in memory."

The phrase **"check memory first"** is useful as a general habit when you notice the agent answering from training rather than from your project's history.

### Saving knowledge during a session

The hooks handle periodic saving, but you can prompt manually at any time — especially after a significant decision, discovery, or completed piece of work:

> "Save that decision to memory. Make sure the reasoning is captured, not just what we decided."

> "Remember this bug and its fix. Link it to the affected component."

> "We just figured out why `<thing>` was broken. Save that as a finding for the `<project>` project."

> "Save a note that this is an open question — we haven't decided yet."

Capturing the *why* is the difference between a memory that can be found later and one that can't. Push the agent to include the reasoning:

> "Make sure you capture why this decision was made, not just what was decided."

### Linking related memories

Links between memories are what make the knowledge base navigable. Prompt the agent to connect new entries to related ones:

> "Link that to `<related topic>` — they're connected because `<reason>`."

> "After saving, check whether anything related is already in memory and link it."

> "Those two things are related — save a connection between them explaining why."

### Cleaning up stale knowledge

Over time, some saved knowledge becomes outdated. Run a review periodically:

> "Look through what's in memory for `<project>` and flag anything that looks stale or outdated."

The agent will present each candidate and ask whether to archive it. You confirm each one individually — the agent will not remove anything without your explicit approval.

To see what has already been archived:

> "Show me what's been archived or forgotten for `<project>`."

### Short-lived notes

For temporary state — sprint tickets, current blockers, branch names that will change — tell the agent the note is short-lived so it gets flagged for cleanup automatically:

> "Save this as a short-lived note — it's only relevant for the current sprint."

---

## Claude Desktop

Claude Desktop does not support hooks. There is no automatic saving — you need to prompt the agent explicitly. The patterns below are designed to be lightweight: a couple of sentences at the start and end of each session covers the essential workflow.

### Orienting the agent at the start of a session

Add one of these as your **first message** in every conversation:

**If you know the project name:**
> "You have access to memoryweb. Before we start, check what you know about `<project>` from memory and tell me where things stand."

**If you're not sure what's in memory:**
> "You have access to memoryweb. Check what projects you have in memory, then summarise the most relevant one."

**For a broader catch-up:**
> "You have access to memoryweb. Show me what's been added to memory recently, grouped by project."

Without this, Claude Desktop has no way to know memoryweb exists or that you want it consulted. **The first message sets the frame for the whole conversation.**

### System prompt (optional but recommended)

If you use the same project regularly, add a system prompt so the agent orients automatically without you having to ask. In Claude Desktop, go to **Settings → Profile** and add something like:

```
You have access to a persistent memory tool called memoryweb.
At the start of every conversation, check what you know about <your-project> from memory
and use it to inform your answers before relying on general knowledge.
When we make decisions, discover bugs, or resolve open questions, save them to memory —
always capture the reasoning behind each decision, not just a summary of what was decided.
```

Replace `<your-project>` with your project name (e.g. `my-app`, `work`, `research`).

### Searching during a session

The same phrases work as in Claude Code:

> "Search memory for `<topic>`."

> "What do we know about `<topic>`? Check memory before answering."

> "How does `<concept A>` relate to `<concept B>`? Look it up in memory."

> "Look up the history of `<topic>` in memory and summarise it."

### Saving at the end of a session

Without hooks, you need to prompt saving manually. A good habit is to end every session with one of these:

> "Before we finish — save anything significant from this conversation to memory. Focus on decisions made, bugs found, and open questions. Make sure the reasoning behind each decision is captured, not just the outcome."

> "We've covered a lot. Save the key findings and link any related ones. Use the `<project>` project."

> "Save this decision to memory. Link it to anything relevant."

If the session was short or nothing important was decided:

> "Nothing to save from this session."

Getting into the habit of ending sessions this way means the knowledge base accumulates steadily rather than only during sessions where you remembered to prompt at the start.

### Checking what was saved

After saving, you can verify what was stored:

> "Show me what was just saved to memory and summarise it."

> "Is the reasoning captured correctly for that last decision? Show me what you saved."

---

## Key phrases at a glance

| What you want | Phrase |
|--------------|--------|
| Orient at session start | `"Before we start, check what you know about <project> from memory and summarise where things stand."` |
| Discover what's in memory | `"What projects do you have in memory? Check and tell me."` |
| Search memory | `"Search memory for <topic>."` |
| Prefer memory over training | `"Check memory first."` |
| Trace a relationship | `"How does X relate to Y? Look it up in memory."` |
| Save a decision | `"Save that decision to memory — capture the reasoning, not just what we decided."` |
| Save an open question | `"Save this as an open question for the <project> project."` |
| Save a bug or fix | `"Remember this bug and its fix. Link it to the affected component."` |
| Link two memories | `"Link those two — they're related because <reason>."` |
| Review stale knowledge | `"Look through memory for <project> and flag anything that looks outdated."` |
| End-of-session saving (Desktop) | `"Before we finish — save anything significant from this session to memory."` |

---

## Common beginner mistakes

**Letting the agent answer from training instead of memory**  
If you ask "what did we decide about X?" without directing the agent to check memory, it may answer from general knowledge or say it doesn't know. Always include "check memory" or "search memory" when you want a project-specific answer.

**Skipping the reasoning**  
A memory without the reasoning behind it is nearly impossible to find later from an oblique search. Push the agent to capture *why* every time. The reasoning should answer: *if someone reads this six months from now with no context, why would they care?*

**Not orienting at session start**  
Memory only helps if the agent is told to consult it. Without an explicit orientation step, the agent has no reason to pull context from memoryweb.

**Saving everything as one large entry**  
One decision or finding per memory entry. Multiple linked entries are far more useful than one large entry trying to capture a whole session. The links between entries are where the value is.

**Forgetting to link**  
A memory with no links can only be found by direct search. After saving, ask the agent to link the new entry to anything related.
