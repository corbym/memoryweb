# CR-21: Remove self-signed cert fallback in release workflow

**Status:** READY
**Priority:** Medium

`.github/workflows/release.yml:107-153` — Self-signed certificate with hardcoded
password `"memoryweb-selfsigned"`. Creates false sense of signing. Either skip signing
entirely when no real cert is available, or fail the build.

---

## Acceptance criteria

- [ ] Remove self-signed cert fallback block
- [ ] Windows builds skip signing when `WINDOWS_CERT_PASSWORD` secret is not set
- [ ] Build log clearly states "Windows binary not signed" when cert is absent
- [ ] No hardcoded passwords in workflow files
