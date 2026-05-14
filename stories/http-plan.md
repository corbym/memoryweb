# HTTP MCP Bridge — Implementation Plan

> **SHELVED — 2026-05-14**
>
> ChatGPT Desktop requires HTTPS even for local connectors. Plain `http://127.0.0.1`
> is rejected at the connector creation step; tunnelling via ngrok or Cloudflare
> Tunnel is required. This routes personal knowledge-graph data through a
> third-party service (privacy concern) and the free-tier tunnel URL changes on
> every restart, breaking the connector. Since ChatGPT Desktop support was the
> only concrete motivation for this implementation, the plan is deferred.
>
> **Trigger for revisiting:** recordari deployed as a hosted service with real
> HTTPS — at that point, HTTP transport is the natural interface and this plan
> can be picked up as-is. In the meantime, stdio (Claude Desktop / Claude Code)
> is the right local interface.

Adds a `memoryweb serve` subcommand implementing the MCP 2025-03-26 streamable
HTTP transport. Localhost-only by default. Target client: ChatGPT Desktop (and
any other client that speaks streamable HTTP).

---

## ChatGPT Desktop — how connection actually works

**The user is right. There is no config file to write.**

ChatGPT Desktop (and ChatGPT web) adds MCP servers exclusively via the GUI:

1. Settings → Apps & Connectors → Advanced settings → enable **developer mode**
2. Settings → Connectors → **Create** → paste the server URL

The existing `setupWriteMCPServerConfig` + `mcp.json` path in the codebase is
for **stdio** servers only (it writes a `"command"` key). There is no documented
config-file mechanism for HTTP servers in ChatGPT Desktop.

**Important constraint:** ChatGPT requires **HTTPS** for connector URLs, even in
developer mode. For local development, the official recommendation is to tunnel
via ngrok or Cloudflare Tunnel:

```
ngrok http 8765
# Forwarding: https://<subdomain>.ngrok.app -> http://127.0.0.1:8765
```

So "localhost-only" in terms of the server binding (127.0.0.1) is correct and
mandatory per the MCP spec, but the ChatGPT client itself connects via an HTTPS
tunnel URL. Plain `http://127.0.0.1:8765` will not be accepted as a connector URL
in ChatGPT.

The `setup` command does **not** need updating to write a config file for the
HTTP case. The right output is printed instructions.

---

## What needs building

### 1. `serveCmd()` subcommand

New case in the `os.Args` switch in `main.go`:

```go
case "serve":
    serveCmd()
```

Flags:
- `--port` (default `8765`)
- `--host` (default `127.0.0.1`, only localhost accepted — any other value is
  rejected at startup to prevent accidental network exposure)

Starts `net/http.ListenAndServe` with a single handler registered at `/mcp`
supporting POST, GET, and DELETE methods.

No new Go module dependencies — everything needed is in stdlib.

---

### 2. In-memory session registry

```go
type sessionRegistry struct {
    mu       sync.RWMutex
    sessions map[string]time.Time // id → created_at
}
```

- `issue() string` — generates a cryptographically random session ID
  (`crypto/rand`, 16 bytes, hex-encoded)
- `valid(id string) bool` — read-lock check
- `revoke(id string)` — write-lock delete

Session IDs are assigned in the `InitializeResult` response as the
`Mcp-Session-Id` header. All subsequent requests must include it; the server
returns 400 if it is absent, 404 if it is unknown.

This is all that session "state" requires — the `Handler` and `Store` are
already stateless and shared.

---

### 3. SSE response writer helper

The spec says the server MUST respond with either `application/json` or
`text/event-stream` to a POST containing JSON-RPC requests. Single-event SSE
(write one `data:` line then close) satisfies the streaming requirement without
any changes to `tools.go`.

```go
func writeSSE(w http.ResponseWriter, payload []byte) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    fmt.Fprintf(w, "data: %s\n\n", payload)
}
```

When the client's `Accept` header includes `text/event-stream`, use SSE.
Otherwise fall back to `application/json`. Most MCP clients send both.

---

### 4. Origin header validation

Required by the spec to prevent DNS rebinding attacks. For localhost-only mode:

- Absent origin: permitted (curl, MCP Inspector, etc.)
- `http://localhost`, `http://localhost:<port>`, `http://127.0.0.1`,
  `http://127.0.0.1:<port>`: permitted
- `null` (file-protocol clients): permitted
- Anything else: **403 Forbidden**

Implemented as a middleware function wrapping the `/mcp` handler.

---

### 5. Protocol version `2025-03-26` in HTTP initialize response

The streamable HTTP transport is only defined in `2025-03-26`. The current
`handleInitialize()` hardcodes `"2024-11-05"`.

Options:
- Pass the version string as a parameter to `handleInitialize` — HTTP path
  passes `"2025-03-26"`, stdio path keeps `"2024-11-05"`.
- Or bump it globally — stdio clients (Claude Desktop, Claude Code) do not
  enforce the version they receive and will not break.

Decision: bump globally to `2025-03-26` — simpler, no divergence to maintain,
and the tool surface is identical between versions.

---

### 6. GET /mcp — 405 response

The spec requires the MCP endpoint to support GET (for server-push SSE). We
return `405 Method Not Allowed` to signal we don't offer it. Compliant.

---

### 7. DELETE /mcp — 405 response

The spec allows clients to terminate sessions with a DELETE. We return `405` to
indicate we don't support explicit session termination. Compliant per spec.

---

### 8. Stats in HTTP mode

`stats.Recorder.Record()` is already safe for concurrent callers (has
`sync.Mutex`). `Flush()` is designed for a single call at process exit.

Decision: share one `Recorder` across all HTTP sessions; flush on SIGTERM/SIGINT
(the signal handler already exists in the stdio path). Stats cover whole-server
uptime, not individual sessions. Per-session granularity is deferred.

---

### 9. `setup` command — printed instructions only

For HTTP mode, `setup` (or a new `--http` flag on setup) should print:

```
memoryweb HTTP MCP server setup:

1. Start the server:
   memoryweb serve --port 8765

2. Expose it over HTTPS (ChatGPT requires HTTPS):
   ngrok http 8765
   # or: cloudflared tunnel --url http://127.0.0.1:8765

3. Add to ChatGPT Desktop:
   Settings → Apps & Connectors → Advanced settings → enable developer mode
   Settings → Connectors → Create → paste the ngrok HTTPS URL

Note: memoryweb serve binds to 127.0.0.1 only and cannot be exposed directly.
A tunnel is required for ChatGPT clients.
```

No config file is written. The existing `setupWriteMCPServerConfig` (stdio,
`mcp.json`) is unchanged.

---

## Tests

Following the established convention: all tests in the same package as the code.

New tests in `main_test.go` (package `main_test`):
- `TestServeHTTP_Initialize_ReturnsMcpSessionId`
- `TestServeHTTP_ToolsList_RequiresSessionId`
- `TestServeHTTP_ToolsList_Returns400WithNoSessionId`
- `TestServeHTTP_ToolCall_Remember_ReturnsSSE`
- `TestServeHTTP_Origin_RejectsExternalOrigin`
- `TestServeHTTP_Origin_AllowsLocalhost`
- `TestServeHTTP_GET_Returns405`
- `TestServeHTTP_DELETE_Returns405`
- `TestServeHTTP_UnknownSession_Returns404`

All use `httptest.NewServer` with a real `db.Store` (temp-file SQLite). No
mocking of the handler layer.

---

## Sequence — what a ChatGPT session looks like

```
POST /mcp  { initialize request }
← 200  Content-Type: text/event-stream
       Mcp-Session-Id: <uuid>
       data: { InitializeResult, protocolVersion: "2025-03-26" }

POST /mcp  { InitializedNotification }  (no id)
           Mcp-Session-Id: <uuid>
← 202 Accepted

POST /mcp  { tools/list request }
           Mcp-Session-Id: <uuid>
← 200  text/event-stream
       data: { tools/list result }

POST /mcp  { tools/call remember ... }
           Mcp-Session-Id: <uuid>
← 200  text/event-stream
       data: { tools/call result }
```

---

## What this does NOT include

- TLS termination (handled by the tunnel — ngrok/Cloudflare)
- Authentication (not needed for localhost; tunnel URL obscurity + developer
  mode is sufficient for local dev)
- Stream resumability (`id` fields on SSE events, `Last-Event-ID` replay) — spec
  marks these as MAY
- Server-push SSE (GET /mcp) — returns 405
- Per-session stats granularity — deferred
