# CR-53: Remove stale FormulSHA256 hashes

**Status:** READY
**Priority:** Low

`Formula/memoryweb.rb:13-31` — SHA256 hashes are for v1.4.3. Anyone installing from
this formula gets the wrong version. The Homebrew tap update workflow handles this
automatically, but the source-of-truth formula in the repo is stale.

---

## Acceptance criteria

- [ ] Either update hashes to match latest release, or remove the formula from the repo
  and rely solely on the tap
