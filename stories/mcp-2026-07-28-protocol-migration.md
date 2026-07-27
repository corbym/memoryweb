# MCP 2026-07-28 protocol migration — assessment for memoryweb

> **CLOSED — 2026-07-27, no action taken.**
>
> The assessment below already showed 6 of 8 RC areas don't apply to memoryweb's
> architecture (stdio-only, hand-rolled, no HTTP transport, no JSON-RPC-level tool
> errors). On review, the remaining two loose ends aren't worth acting on either:
> - The hardcoded `protocolVersion: "2024-11-05"` stays as-is — no client behaviour
>   depends on it and there's no concrete trigger to change it.
> - The `stories/http-plan.md` pointer note is skipped — that plan is already
>   shelved with no revival trigger of its own; a note on a dead plan documenting
>   how a different dead plan would need to change is not worth carrying.
>
> Closing outright rather than leaving it open on a technicality. If HTTP transport
> work is ever un-shelved, re-derive the 2026-07-28 implications fresh at that point
> — this assessment will be stale by then anyway.

**Status:** CLOSED — moot given memoryweb's current shape. No action item required
or wanted.

**Recordari nodes (context, not memoryweb's to edit):**
- `goal-assess-and-plan-mcp-2026-07-28-migration-...-6a9757ba` (domain: `recordari`) — the
  originating goal, filed 2026-07-12.
- `finding-clientinfo-name-version-is-required-in-the-mcp-initialize-request-...-8226bc4a`
  (domain: `recordari`) — the spec-verification finding it's `caused_by`.

**Domain-routing note:** both of the above were filed only in the `recordari` domain.
This is an MCP protocol version change — it affects any MCP server, including
memoryweb — so per the shared-surface standing rule
(`file-cross-cutting-stories-in-th-c1e7e900`: "cross-cutting — affects both memoryweb
and Recordari ... → memoryweb-shared-surface first") it should have been filed in
`memoryweb-shared-surface`, not `recordari` only. That's why this repo had no story
for it as of 2026-07-27 despite the goal existing since 2026-07-12 — nothing pointed
memoryweb's coding agent at it. This story is the handoff-gap fix; a pointer node has
been filed in Recordari to close the loop (see "References" below).

---

## What's actually happening

MCP's 2026-07-28 spec release (RC locked 2026-05-21) is the largest protocol
revision since launch and is explicitly breaking, but **no day-one break** is
expected: protocol version negotiation means existing hosts keep speaking whatever
version they already speak to whatever version a server declares. The real
deadline is soft — host deprecation timelines and MCP directory certification
requirements (not applicable to memoryweb, which isn't listed in a connector
directory) — not 2026-07-28 itself.

The recordari goal lists eight assessment areas. Below is what each one means for
memoryweb specifically, given memoryweb's actual architecture: a single stdio
process per session, hand-rolled JSON-RPC dispatch in `main.go` (not an MCP SDK),
no HTTP transport (`stories/http-plan.md` is shelved), and tool-level failures
returned as `ToolResult{IsError: true}` rather than JSON-RPC errors (per
`CLAUDE.md`'s wire-protocol section) — memoryweb has no JSON-RPC error codes to
migrate in the first place.

| # | RC area | Applies to memoryweb? | Notes |
|---|---------|------------------------|-------|
| 1 | Stateless core (no `initialize`, self-contained requests) | Not directly | This is a Streamable HTTP concern; memoryweb's stdio transport does a one-time `initialize` per process already, which is not the thing being deprecated (that's the *session* concept tied to `Mcp-Session-Id`, which memoryweb's stdio path never had). |
| 2 | Mandatory `Mcp-Method`/`Mcp-Name` headers | No | HTTP-only requirement. memoryweb has no HTTP transport currently. |
| 3 | Error code change `-32002` → `-32602` | No | Verified: `-32002` does not appear anywhere in this codebase (memoryweb never adopted the old resource-not-found code). No migration needed. |
| 4 | Session removal (`Mcp-Session-Id`) | No (today) — **flag for `http-plan.md`** | memoryweb's stdio path has no session ID. But the shelved `stories/http-plan.md` design *does* — its `sessionRegistry` and `Mcp-Session-Id` header design predates this RC and would need a rewrite (explicit-handle pattern instead of a session ID) if that plan is ever revived. See action item below. |
| 5 | Elicitation (`InputRequiredResult`/`inputResponses`) | No | memoryweb has no server-initiated elicitation feature today. |
| 6 | Auth SEPs (OAuth `iss`, DCR, issuer-bound credentials) | No | memoryweb is a local stdio process with no auth surface. (Recordari's Supabase-hosted OAuth path is the thing at risk here, not memoryweb.) |
| 7 | SDK vs hand-rolled | Confirmed hand-rolled | `main.go`'s `dispatch()`/`handleInitialize()` is a manual JSON-RPC loop, not built on an official MCP Go SDK. No upstream fix will land automatically — any future spec-compliance work here is manual, same as this assessment. |
| 8 | `ttlMs`/`cacheScope` on `tools/list` | Optional, not urgent | Would let clients cache `tools/list` responses. memoryweb's tool list changes rarely enough per session that this is a nice-to-have, not a gap. |

## The one live loose end: hardcoded `protocolVersion`

`handleInitialize()` in `main.go` hardcodes `"protocolVersion": "2024-11-05"` —
already one full revision behind `2025-03-26`, and now two behind the 2026-07-28
release. `stories/http-plan.md` (shelved) already flagged that stdio clients
(Claude Desktop, Claude Code) don't enforce the version they receive and won't
break — so this has not been an active bug. Whether to update the declared
version string (and to what) is a decision to make deliberately rather than by
inertia; see acceptance criteria.

## Action items — all resolved to "no action," none taken

1. ~~Decide whether to bump the hardcoded `protocolVersion` string.~~ **Resolved:
   leave as-is.** No client behaviour depends on it; nothing to gain by changing
   it pre-emptively.
2. ~~Add a pointer note to `stories/http-plan.md`.~~ **Resolved: skip.** That plan
   is already shelved with its own dormant status; annotating a shelved plan with
   implications for a spec revision isn't worth the upkeep. Re-derive fresh if it's
   ever revived.
3. No action needed on error codes or auth SEPs — confirmed not applicable, nothing
   further to do.
4. `_meta` `clientInfo`-per-request capture — confirmed out of scope for
   memoryweb (it's a recordari-specific actor-decomposition feature). No action.

## Acceptance criteria

- N/A — closed without implementation. No test or code changes accompany this
  story.

## Files

- None changed.

## References

- Recordari goal: `goal-assess-and-plan-mcp-2026-07-28-migration-...-6a9757ba`
  (domain `recordari`, filed 2026-07-12).
- Recordari finding: `finding-clientinfo-name-version-is-required-in-the-mcp-initialize-request-...-8226bc4a`
  (domain `recordari`).
- Shared-surface standing rule: `file-cross-cutting-stories-in-th-c1e7e900`
  ("File cross-cutting stories in memoryweb-shared-surface first").
