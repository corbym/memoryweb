# CR-03: Harden HTTP client in embeddings.go

**Status:** DONE — implemented this session. Shared `ollamaHTTPClient` (30s timeout) + `maxEmbeddingBodySize` 1MB `io.LimitReader`. All tests pass.

`db/embeddings.go:63,69` — No HTTP timeout on the Ollama client and unbounded
`io.ReadAll` on the response body. A hung or malicious endpoint blocks the goroutine
forever and can OOM.

---

## Current code

```go
resp, err := http.Post(ollamaEndpoint+"/api/embeddings", ...)  // no timeout
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)  // unbounded
```

---

## Acceptance criteria

- [ ] Create a package-level `http.Client{Timeout: 30 * time.Second}` in `embeddings.go`
- [ ] Replace `http.Post` with `client.Post`
- [ ] Wrap `resp.Body` with `io.LimitReader(resp.Body, 1<<20)` (1 MB max)
- [ ] Return a descriptive error if the body is truncated
- [ ] `TestEmbed_*` tests pass (require Ollama — skip if unavailable)

---

## Notes

The Ollama endpoint is localhost-only, so the threat model is a hung Ollama process,
not a remote attacker. But defensive coding costs nothing here.
