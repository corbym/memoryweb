# CR-07: Escape Mermaid labels for brackets

**Status:** DONE — implemented this session. `sanitiseMermaidLabel` now escapes `[` → `\[` and `]` → `\]` (before `"\n` handling so the order doesn't matter for brackets). New outside-in test `TestVisualise_MermaidLabelBrackets` (label `Task [v2] done`) failed red on the old code, passes green; `TestVisualiseLabelSanitisation` and all other visualise tests still pass.
**Priority:** High

`tools/graph.go:66-76` — `sanitiseMermaidLabel` escapes `"` and newlines but not `[` or `]`.
Labels containing these characters break Mermaid diagram syntax.

---

## Acceptance criteria

- [ ] `sanitiseMermaidLabel` escapes `[` → `\\[` and `]` → `\\]`
- [ ] New test: `TestSanitiseMermaidLabel_Brackets` — label `Task [v2] done` renders correctly
- [ ] Existing `TestVisualise*` tests pass
