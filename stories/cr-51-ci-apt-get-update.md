# CR-51: Add apt-get update to CI workflows

**Status:** DONE
**Priority:** Low

`ci.yml:29`, `integration.yml:49` — `sudo apt-get install -y gcc` without `apt-get update`.
Package lists can be stale on `ubuntu-latest`.

---

## Acceptance criteria

- [x] Add `sudo apt-get update` before `apt-get install` in all CI jobs
