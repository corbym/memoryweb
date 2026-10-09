# STORY-004: Remove self-signed cert fallback in release workflow

**Type:** bug

## Goal

`.github/workflows/release.yml:107-153` creates a self-signed certificate with hardcoded password `"memoryweb-selfsigned"`. This creates a false sense of signing and leaves a hardcoded credential in the workflow.

**Deferred** (user decision 2026-09-09): not doing code-signed cert work unless it becomes necessary. Homebrew distribution works fine unsigned. Re-evaluate only if Windows-binary trust/AV warnings become a real problem.

Fix: remove self-signed cert fallback block; Windows builds skip signing when `WINDOWS_CERT_PASSWORD` secret is not set; build log clearly states "Windows binary not signed" when cert is absent.

Refs: `stories/cr-21-release-selfsigned-cert.md`

## Acceptance criteria

- [ ] AC-STORY-004-abf2c406: Remove self-signed cert fallback block from `.github/workflows/release.yml:107-153`
- [ ] AC-STORY-004-f1a6793a: Windows builds skip signing when `WINDOWS_CERT_PASSWORD` secret is not set
- [ ] AC-STORY-004-032e8d37: Build log clearly states 'Windows binary not signed' when cert is absent
- [ ] AC-STORY-004-41c49436: No hardcoded passwords remain in workflow files
- [ ] AC-STORY-004-db8ceb93: `go vet ./...` and `go test ./...` pass
