# STORY-014: orient(topic=X) without domain runs workspace-wide semantic search

**Type:** feature

## Goal

When orient is called with a topic but no domain and no domains array, instead of returning an error (STORY-010 Option A), run a workspace-wide semantic search for the topic across all domains and return a relevant section unscoped to any single domain.

This requires Ollama embeddings to be running. When embeddings are unavailable, fall back to the STORY-010 error ("topic requires a domain — pass domain=X or call domains() first") so behaviour degrades gracefully.

Option A (STORY-010) ships first as the safe baseline. This story replaces the error with the cross-domain relevant section once embeddings are confirmed available.

Refs: tools/orient.go orientCrossDomain (~line 62), orientWithTopic (~line 167), summariseDomain (~line 441).

## Acceptance criteria

- [ ] Define acceptance criteria
