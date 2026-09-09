# CR-07: Escape Mermaid labels for brackets

**Status:** READY
**Priority:** High

`tools/graph.go:66-76` — `sanitiseMermaidLabel` escapes `"` and newlines but not `[` or `]`.
Labels containing these characters break Mermaid diagram syntax.

---

## Acceptance criteria

- [ ] `sanitiseMermaidLabel` escapes `[` → `\\[` and `]` → `\\]`
- [ ] New test: `TestSanitiseMermaidLabel_Brackets` — label `Task [v2] done` renders correctly
- [ ] Existing `TestVisualise*` tests pass
