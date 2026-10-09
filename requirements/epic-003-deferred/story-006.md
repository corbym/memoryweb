# STORY-006: HTTP MCP Bridge — memoryweb serve subcommand

**Type:** feature

## Goal

Adds `memoryweb serve` implementing MCP 2025-03-26 streamable HTTP transport on localhost (default port 8765).

**Shelved** (2026-05-14): ChatGPT Desktop requires HTTPS even for local connectors. Plain `http://127.0.0.1` is rejected. Tunnelling via ngrok/Cloudflare routes personal knowledge-graph data through a third-party service (privacy concern), and free-tier tunnel URLs change on restart.

**Reopen trigger:** recordari deployed as a hosted service with real HTTPS — at that point HTTP transport is the natural interface and this plan can be picked up as-is.

Key design decisions already made:
- Session registry via `crypto/rand` 16-byte hex IDs in `Mcp-Session-Id` header
- Origin validation middleware (DNS rebinding prevention)
- Single-event SSE response writer
- Protocol version bumped globally to `2025-03-26`
- GET/DELETE `/mcp` return 405 (compliant)
- `setup --http` prints instructions only, no config file written

Refs: `stories/http-plan.md`

## Acceptance criteria

- [ ] Define acceptance criteria
