# CR-43: Fix Significance tags variable shadowing

**Status:** READY
**Priority:** Low

`db/significance.go:116,309` — Variable `tags` shadows the function parameter `tags []string`.
The struct scan variable is `sql.NullString` — confusing and error-prone.

---

## Acceptance criteria

- [ ] Rename local variable to `tagsNull` or `tagsCol`
- [ ] No behavioral change
