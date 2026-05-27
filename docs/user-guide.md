# memoryweb user guide

A practical guide to getting the most out of memoryweb across the surfaces that support it. This is aimed at beginners — you don't need to understand the internals, just the patterns that make the tool work well.

Covered surfaces: **Claude Code**, **GitHub Copilot (VS Code)**, **Claude Desktop**, **Cowork**.

---

## Contents

1. [What memoryweb does (and doesn't do)](#what-memoryweb-does-and-doesnt-do)
2. [Getting started fast](#getting-started-fast)
3. [Claude Code / GitHub Copilot](#claude-code--github-copilot)
4. [Claude Desktop](#claude-desktop)
5. [Cowork](#cowork)
6. [memoryweb as a personal assistant](#memoryweb-as-a-personal-assistant)
7. [Tips and tricks from real sessions](#tips-and-tricks-from-real-sessions)
8. [Key phrases at a glance](#key-phrases-at-a-glance)
9. [The timeline — significant decisions worth remembering](#the-timeline--significant-decisions-worth-remembering)
10. [Significance — what matters most in a domain](#significance--what-matters-most-in-a-domain)
11. [Visualising the graph](#visualising-the-graph)
12. [Common beginner mistakes](#common-beginner-mistakes)

---

## What memoryweb does (and doesn't do)

memoryweb gives an AI agent **persistent memory that outlasts a single conversation**. Without it, every session starts from scratch. With it, decisions, bugs, design choices, and open questions accumulate over time into a knowledge base the agent can pull from on demand.

It is not a chat history tool. It is a **decision log** — it records *what was learned and why it matters*, not a transcript of what was said.

---

## Getting started fast

You don't need to understand everything to get value on day one. The minimal workflow is two steps:

**Step 1 — Orient at the start of a session:**
> "Before we start, check what you know about `<project>` from memory and tell me where things stand."

**Step 2 — Save at the end:**
> "Before we finish — save anything significant from this conversation to memory. Capture the reasoning behind each decision, not just the outcome."

That's it. Everything else in this guide is about doing those two things better, more automatically, or in richer ways. Start there and add the rest gradually.

---

## Claude Code / GitHub Copilot

Claude Code and GitHub Copilot (VS Code) both support hook events that fire automatically during a session. When memoryweb is installed correctly, most of the work happens without you asking for it. This section explains what is automatic, what you need to do, and what phrases get the best results.

### What happens automatically (when hooks are installed)

Two hooks run in the background:

- **Save hook (Stop event):** every 15 AI responses, the hook pauses the session and asks the agent to save anything significant before continuing. You will see a brief pause while the agent files its findings. The interval is configurable via the `MEMORYWEB_SAVE_INTERVAL` environment variable if 15 is too frequent or too infrequent for your workflow.
- **PreCompact hook:** fires before context compaction and asks the agent to save everything important that hasn't been saved yet. This prevents knowledge loss when the context window is about to be trimmed.

If you have run `memoryweb setup`, these are already active. Verify with:

```bash
memoryweb doctor
```

Look for `[✓] Claude hooks:   Stop and PreCompact hooks installed`. If you see `[✗]`, re-run setup or check the hooks section of the README.

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

For precise lookups by identifier — a ticket number, a branch name, a specific error code — add "use exact matching" to your prompt:

> "Search memory for `PROJ-431` — use exact matching."

Without this, the agent's semantic search may rank conceptually similar results above the exact string you're looking for.

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

**For a coding project:**
```
You have access to a persistent memory tool called memoryweb.
At the start of every conversation, check what you know about <your-project> from memory
and use it to inform your answers before relying on general knowledge.
When we make decisions, discover bugs, or resolve open questions, save them to memory —
always capture the reasoning behind each decision, not just a summary of what was decided.
```

**For personal use (non-coding):**
```
You have access to a persistent memory tool called memoryweb.
At the start of every conversation, check what you know about my current projects and
open questions from memory, and use that context before answering.
When I make decisions or work through something significant, save it to memory with
the reasoning — not just what I decided, but why. At the end of each conversation,
prompt me to save anything we haven't captured yet.
```

Replace `<your-project>` with your project name (e.g. `my-app`, `work`, `research`).

### Searching during a session

The same phrases work as in Claude Code:

> "Search memory for `<topic>`."

> "What do we know about `<topic>`? Check memory before answering."

> "How does `<concept A>` relate to `<concept B>`? Look it up in memory."

> "Look up the history of `<topic>` in memory and summarise it."

For exact identifier lookups (ticket numbers, error codes, branch names), add "use exact matching" to avoid semantic search ranking the wrong result first.

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

## Cowork

Cowork does not support hooks, so saving needs to be prompted manually. The distinctive opportunity with Cowork is **scheduled tasks**: you can set up recurring prompts that run automatically on a schedule and use memoryweb to deliver a briefing or surface open questions without you having to ask.

### Orienting at the start of a session

Use the same opening prompts as Claude Desktop:

**If you know the project name:**
> "You have access to memoryweb. Before we start, check what you know about `<project>` from memory and tell me where things stand."

**For a broader catch-up:**
> "You have access to memoryweb. Show me what's been added to memory recently, grouped by project."

### Scheduled briefings

A scheduled task paired with memoryweb is the closest thing to a proactive memory: an agent that checks in on your behalf and tells you what's outstanding, without waiting to be asked.

To set one up, ask Claude in Cowork:

> "Set up a daily briefing at 8am that checks memory for my `<project>` domain and tells me what's outstanding — open questions, recent decisions I should follow up on, and anything that looks stale."

The agent will create a scheduled task with a prompt something like:

```
Orient in the '<project>' domain of memoryweb.
Report back:
- Any open questions that haven't been resolved
- Significant decisions from the last 7 days
- Anything that looks like it may be stale or needs a follow-up
Keep the summary brief — three to five bullet points maximum.
```

You can tailor the schedule and the prompt to your needs:

> "Run this every Monday morning instead of daily."

> "Add a second briefing at the end of Friday that prompts me to save anything from the week."

> "Make the briefing cover all my active domains, not just one."

The cross-domain variant is particularly useful if you use memoryweb across multiple projects:

> "Set up a weekly Monday morning briefing that checks all my active domains and gives me a headline from each — what's been happening and what's outstanding."

### Saving in Cowork

Without hooks, use the same end-of-session prompt as Claude Desktop:

> "Before we finish — save anything significant from this conversation to memory."

---

## memoryweb as a personal assistant

memoryweb is not just for coding projects. Because it stores *decisions and reasoning* rather than transcripts, it works equally well for anything where you want to remember what you were thinking — career moves, side projects, research, life decisions, creative work.

### The "thinking partner that remembers" pattern

The core pattern for personal use is simple: have conversations with an AI about things that matter, and save the conclusions. Over time, the AI accumulates genuine context about your thinking — not a summary of your life, but the specific decisions, constraints, and open questions that are active right now.

For example:
- Working through a career decision → save the options you considered and why you leaned one way
- Researching a topic → save the key findings and what still needs answering
- Planning a project → save the constraints you identified and the approach you chose
- Reflecting on something that went wrong → save what you learned and what you'd do differently

Six months later, when the topic comes up again:

> "What have I previously thought about `<topic>`? Check memory."

The agent pulls back your actual reasoning, not a generic answer.

### Domain design for personal use

For personal use, a single domain often works well to start — something like `life`, `journal`, or your name. If you find distinct areas growing large, split them:

- `work` — career decisions, professional context, workplace dynamics
- `research` — reading notes, findings, questions in progress
- `projects` — side projects, creative work, personal goals
- `finance` — significant financial decisions and their reasoning

Don't over-engineer this early. Start with one domain, split when searching becomes noisy.

If you're not sure what to call a domain, just ask the agent:

> "I want to start saving things about `<topic>`. What would be a sensible domain name for this? Check what I already have in memory and suggest something that fits."

The agent will look at your existing domains and either suggest a new name or recommend filing under one that already exists. Similarly, when saving something, you don't need to specify a domain — tell the agent what the memory is about and let it work out where it belongs:

> "Save this — figure out which domain makes the most sense and check with me before filing."

The agent searches existing content, infers the most likely domain from what it finds, and confirms before saving. Over time this means your domain structure grows organically from your actual content rather than from an upfront guess about how you'll use it.

### The end-of-week review

A lightweight weekly habit that compounds well: end each week with a five-minute save session.

> "What were the significant decisions or realisations I had this week? Let me tell you, and you save them. Ask me for the reasoning behind each one."

Over a year, this builds a searchable record of how your thinking evolved — far more useful than a journal, because it's queryable by topic and navigable by connection.

### Looking back

One of the most underused features for personal use is pulling historical context:

> "What was I thinking about `<topic>` back in `<month/year>`? Check memory."

> "What open questions have I had about `<topic>` over time? Look them up."

> "What did I decide about `<decision>` and what was the reasoning? It would have been a while ago."

This is particularly useful before making a decision you've faced before — surfacing your previous thinking so you don't re-derive conclusions you've already reached.

---

## Tips and tricks from real sessions

These patterns came out of real use — including sessions where memoryweb was used to build memoryweb itself, which turned out to be a good stress-test of what works and what doesn't.

### Connect immediately after saving

The most common failure mode is saving a memory and forgetting to link it to anything. An unlinked memory can only be found by direct search — if the search terms drift from the label, it's effectively lost. The fix is to connect as part of the same action:

> "Save that decision and link it to anything related in memory."

> "Remember this and then check if there are connections to make — don't leave it hanging."

In practice: any time you save something, your next prompt should either be a connect or an explicit acknowledgement that it stands alone. Don't let linking become a separate task you'll get to later — you won't.

### Orient across all domains at once

If you use memoryweb across multiple projects, you don't always know which domain is most relevant at session start. Instead of guessing:

> "Show me what's been happening across all my memory domains recently — a headline from each."

This gives you a quick cross-project snapshot so you can decide where to go, rather than picking a domain and potentially missing something more active.

### Trace a chain of reasoning

When you need to understand how a conclusion was reached — especially one made weeks or months ago — the trace tool can walk the connection path between two concepts:

> "Trace the connection between `<concept A>` and `<concept B>` in memory. How did we get from one to the other?"

> "How did we end up at `<decision>`? Walk back through the reasoning in memory."

This is particularly useful when you're explaining a past decision to someone else, or when you're about to reverse something and want to make sure you understand what led to it.

### Use memoryweb as a lightweight ADR system

For technical projects, the timeline + significant decisions feature effectively gives you an Architecture Decision Record (ADR) system for free. The pattern is:

1. When you make a significant architectural choice, save it with `occurred_at` set to today
2. Include the alternatives you considered and why you didn't choose them
3. Link it to the components it affects

Later, when someone asks "why did we do it this way?" or you're about to change it:

> "What architectural decisions have been made about `<system/component>`? Show me the timeline."

> "What were the alternatives we considered when we decided on `<approach>`?"

> "What does changing `<thing>` touch? What decisions were made that depend on it?"

The reasoning behind the decision travels with it, and the connections show what it affects.

### The maintenance loop

A periodic cleanup prevents the knowledge base from accumulating noise. Ten minutes, once a week or once a sprint:

> "Run a drift check on `<project>` — flag anything that looks stale, duplicate, or disconnected."

The agent will surface candidates and ask about each one individually. You don't have to archive everything it flags — just use it as a prompt to think about what's still relevant.

For disconnected memories specifically:

> "Are there any memories in `<project>` that aren't connected to anything? Show me them."

Unlinked memories either need connecting or archiving. If you can't remember why something was saved and it has no connections, it's probably safe to archive.

### The "why this mattered" test

Before saving anything, a useful mental test: *if someone read this six months from now with no context, would they understand why it mattered?*

If the answer is no, the description needs more. The most common gap is saving *what* was decided without saving *why* — the constraint that ruled something out, the failure mode that was avoided, the principle that guided the choice. That context is what makes a memory findable and useful later.

Push the agent explicitly when needed:

> "What's the reasoning behind that decision? Make sure that's in the description before saving."

> "Why did we rule out the other approach? Save that too."

### Domain aliases for speed

If you use a domain regularly with a long name, set an alias so you can refer to it naturally:

> "Create an alias 'mw' for the 'memoryweb-meta' domain."

After that, searches and orients recognise the short form. Particularly useful for domains you orient in at the start of every session.

---

## Key phrases at a glance

| What you want | Phrase |
|--------------|--------|
| Orient at session start | `"Before we start, check what you know about <project> from memory and summarise where things stand."` |
| Orient across all projects | `"Show me what's been happening across all my memory domains recently."` |
| Discover what's in memory | `"What projects do you have in memory? Check and tell me."` |
| Search memory | `"Search memory for <topic>."` |
| Prefer memory over training | `"Check memory first."` |
| Trace a relationship | `"How does X relate to Y? Look it up in memory."` |
| Trace a chain of reasoning | `"Trace the connection between X and Y in memory."` |
| Save a decision | `"Save that decision to memory — capture the reasoning, not just what we decided."` |
| Save and link immediately | `"Save that and link it to anything related in memory."` |
| Save an open question | `"Save this as an open question for the <project> project."` |
| Save a bug or fix | `"Remember this bug and its fix. Link it to the affected component."` |
| Save a short-lived note | `"Save this as a short-lived note — it's only relevant for the current sprint."` |
| Link two memories | `"Link those two — they're related because <reason>."` |
| Review stale knowledge | `"Look through memory for <project> and flag anything that looks outdated."` |
| Find disconnected memories | `"Are there any memories in <project> that aren't connected to anything?"` |
| See what's most important | `"What's most significant in memory for <project>? Show me the top decisions and most-referenced concepts."` |
| Visualise connections | `"Draw a map of the connections in the <project> domain."` |
| See a concept's neighbourhood | `"Show me a diagram of everything connected to <concept>."` |
| Look up historical thinking | `"What was I thinking about <topic> back in <month/year>? Check memory."` |
| End-of-session saving (Desktop/Cowork) | `"Before we finish — save anything significant from this session to memory."` |

---

## The timeline — significant decisions worth remembering

### What the timeline is for

memoryweb keeps a curated timeline of decisions and events that shaped a project. Placing something on the timeline is a deliberate act — it means "this mattered enough to remember when it happened." It is not a log of everything that gets saved.

A good rule of thumb:

- **Worth adding to the timeline:** architecture decisions, technology choices, key constraints identified, significant bugs and their resolutions, turning points in the project.
- **Not worth it:** routine findings, build notes, work-in-progress thoughts, short-lived state, anything you would be fine not seeing in a historical summary six months from now.

The goal is a record you can look back on and understand *what shaped the project* — not a transcript of everything that was done.

### How it works — in-session vs back-dated

How the agent handles the date depends on whether it witnessed the decision directly:

**In-session:** if a decision happened during the current conversation and the agent was there for it, it can place it on the timeline using today's date without asking. You were both present — confirmation is redundant.

**Inferred or back-dated:** if the agent is reconstructing something from context, guessing when it happened, or you're asking it to file a decision from the past, it must propose the date and wait for your confirmation before saving. It should never silently infer a date it didn't observe.

If you confirm but don't specify a date, the agent uses today's date.

If an agent silently sets a date it couldn't have witnessed, that's a mistake — not intended behaviour. See "Correcting mistakes" below.

To explicitly put something on the timeline yourself:

> "File that as a significant decision on the timeline. It happened on `<date>`."

> "That's important enough for the timeline — check today's date and save it."

> "Save that, and mark it as significant."

### Reasoning is required

You cannot mark something as significant without explaining why it mattered. If the agent tries to place something on the timeline without a reason, it will be rejected. This is by design — a date with no explanation is not useful history.

If the agent gets stuck or asks for more context, tell it why the decision mattered:

> "Save that — it matters because it rules out the alternative approach we'd been considering."

### Viewing the timeline

To see what's on the timeline:

> "Show me the timeline for `<project>`."

To see only the curated significant decisions, not everything:

> "Show me only the significant decisions for `<project>`."

You can also filter by date range or topic:

> "Show me what happened in `<project>` between January and March."

> "Show me the timeline for `<project>`, filtered to architecture decisions."

### Correcting mistakes

If something was added to the timeline with the wrong date:

> "Correct the date on that decision — it actually happened on `<correct date>`."

> "The date on `<memory label>` is wrong — it should be `<date>`. Fix it."

If something was added to the timeline that shouldn't have been at all, the best option is to archive it and re-save it without placing it on the timeline:

> "Archive `<memory label>` — it shouldn't be on the timeline. I'll tell you what to save instead."

---

## Significance — what matters most in a domain

### What significance analysis does

The `significance` tool gives you a dual-signal view of what is most important in a domain:

- **Declared** — decisions you (or the agent) have explicitly marked as significant by placing them on the timeline. These are curated.
- **Structural** — nodes that other nodes link to most frequently, weighted by how recently those links were created. High structural score = many current threads depend on this concept.
- **Uncurated** — structurally important nodes that haven't been declared significant yet. These are curation candidates: the graph thinks they matter, but they haven't been formally acknowledged.
- **Potentially stale** — nodes that were declared significant but now have low structural score. Declared important once, but nothing current seems to depend on them anymore.

The most actionable gap is between uncurated and potentially stale: things the graph thinks are important but you haven't curated, and things you once curated but the graph has moved on from.

### When to use it

- When starting a new phase of a project: find out what the current graph thinks is central before diving in
- When curating the timeline: use uncurated results to find missed significant decisions
- When cleaning up: use potentially_stale to find decisions that may no longer be relevant

### How to ask for it

> "What's most significant in memory for `<project>`? Give me the top decisions and the most-referenced concepts."

> "Which memories in `<project>` are structurally important but haven't been marked as significant yet?"

> "Are there any decisions marked as important that seem to have become irrelevant? Check memory for `<project>`."

---

## Visualising the graph

### Domain map

To see all the connections in a project as a diagram:

> "Draw a map of the connections in the `<project>` domain."

> "Show me the graph for `<project>` as a diagram."

The agent will output a Mermaid flowchart. In Claude Code and Copilot, this renders as a visual diagram in the response. In Claude Desktop, rendering depends on your version — recent versions display the diagram inline; older versions show a code block you can paste into a Mermaid viewer such as [mermaid.live](https://mermaid.live).

If the domain is large, the diagram will be truncated to the most recent nodes. The response includes the total node and edge count so you know how much is visible.

### Neighbourhood view

To see how a specific concept connects to everything around it:

> "Show me a diagram of everything connected to `<concept>`."

> "Draw the neighbourhood of `<memory label>` — what links to and from it."

This gives a focused view of one node and its immediate connections, regardless of domain size.

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
A memory with no links can only be found by direct search. After saving, ask the agent to link the new entry to anything related. This is the single most common cause of a knowledge base that feels "full but useless" — lots of entries, few connections.

**Over-engineering domains early**  
Don't create five domains before you've saved five memories. Start with one, let it grow, and split only when searching in that domain returns too much noise. Domain structure should follow your actual usage, not an upfront plan.
