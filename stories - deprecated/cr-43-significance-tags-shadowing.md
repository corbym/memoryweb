# CR-43: Fix Significance tags variable shadowing

**Status:** DONE
**Priority:** Low

`db/significance.go:116,309` — Variable `tags` shadows the function parameter `tags []string`.
The struct scan variable is `sql.NullString` — confusing and error-prone.

---

## Acceptance criteria

- [x] Rename local variable to `tagsNull` or `tagsCol`
- [x] No behavioral change
