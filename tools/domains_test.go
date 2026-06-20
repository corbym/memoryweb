package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestListDomains_ReturnsDistinctDomains(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node A", "domain-alpha", nil)
	addNode(t, h, "Node B", "domain-beta", nil)
	addNode(t, h, "Node C", "domain-alpha", nil) // duplicate domain

	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)

	var resp struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse domains response: %v", err)
	}
	if len(resp.Domains) != 2 {
		t.Errorf("expected 2 distinct domains, got %d: %v", len(resp.Domains), resp.Domains)
	}
	if !contains(resp.Domains, "domain-alpha") {
		t.Error("expected domain-alpha in result")
	}
	if !contains(resp.Domains, "domain-beta") {
		t.Error("expected domain-beta in result")
	}
}

func TestListDomains_ExcludesArchivedOnlyDomains(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Ghost node", "dead-domain", nil)
	store.ArchiveNode(id, "test")
	addNode(t, h, "Live node", "live-domain", nil)

	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)

	var resp struct {
		Domains []string `json:"domains"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if contains(resp.Domains, "dead-domain") {
		t.Error("dead-domain should not appear: all its nodes are archived")
	}
	if !contains(resp.Domains, "live-domain") {
		t.Error("live-domain should appear")
	}
}

func TestListDomains_EmptyDB(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)
	var resp struct {
		Domains []string `json:"domains"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if len(resp.Domains) != 0 {
		t.Errorf("expected empty list, got %v", resp.Domains)
	}
}

// ── aliases ───────────────────────────────────────────────────────────────────

func TestAddAlias_SearchResolvesAlias(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Engine node", "deep-engine", nil)

	call(t, h, "alias", map[string]any{"action": "add", "alias": "engine", "domain": "deep-engine"})

	tr := call(t, h, "search", map[string]any{"query": "Engine", "domain": "engine"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("alias should resolve to canonical domain in search")
	}
}

func TestResolveDomain_ReturnsCanonical(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "dg", "domain": "deep-game"})

	tr := call(t, h, "alias", map[string]any{"action": "resolve", "name": "dg"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "deep-game") {
		t.Errorf("resolve_domain should return canonical; got: %s", text(t, tr))
	}
}

func TestResolveDomain_UnknownAliasReturnsItself(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "resolve", "name": "unknown-domain"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "unknown-domain") {
		t.Errorf("unregistered name should resolve to itself; got: %s", text(t, tr))
	}
}

func TestListAliases_ReturnsRegisteredAliases(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "dg", "domain": "deep-game"})
	call(t, h, "alias", map[string]any{"action": "add", "alias": "sx", "domain": "sedex"})

	tr := call(t, h, "alias", map[string]any{"action": "list"})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "dg") || !strings.Contains(body, "sx") {
		t.Errorf("list_aliases missing registered aliases; got: %s", body)
	}
}

// ── remove_alias ──────────────────────────────────────────────────────────────

func TestRemoveAlias_RemovesExistingAlias(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "dg", "domain": "deep-game"})

	tr := call(t, h, "alias", map[string]any{"action": "remove", "alias": "dg"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "dg") {
		t.Errorf("expected confirmation mentioning alias; got: %s", text(t, tr))
	}

	// list_aliases should no longer contain it
	listTr := call(t, h, "alias", map[string]any{"action": "list"})
	mustNotError(t, listTr)
	if strings.Contains(text(t, listTr), `"dg"`) {
		t.Error("alias 'dg' should not appear in list_aliases after removal")
	}
}

func TestRemoveAlias_NonExistentReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "remove", "alias": "ghost-alias"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("expected 'not found' error; got: %s", text(t, tr))
	}
}

func TestRemoveAlias_SearchNoLongerResolvesRemovedAlias(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Engine node", "deep-engine", nil)

	call(t, h, "alias", map[string]any{"action": "add", "alias": "engine", "domain": "deep-engine"})

	// confirm alias resolves while it exists
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "Engine", "domain": "engine",
	})), id) {
		t.Fatal("alias should resolve before removal")
	}

	mustNotError(t, call(t, h, "alias", map[string]any{"action": "remove", "alias": "engine"}))

	// after removal, searching under the alias should return nothing
	tr := call(t, h, "search", map[string]any{
		"query": "Engine", "domain": "engine",
	})
	mustNotError(t, tr)
	if contains(searchIDs(t, tr), id) {
		t.Error("removed alias should no longer resolve to canonical domain in search")
	}
}

// ── unknown tool ──────────────────────────────────────────────────────────────

func TestRenameDomain_RenamesNodesAndCreatesAlias(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha", "old-dom", nil)
	addNode(t, h, "Beta", "old-dom", nil)

	tr := call(t, h, "rename_domain", map[string]any{
		"old_domain": "old-dom",
		"new_domain": "new-dom",
	})
	mustNotError(t, tr)
	if !strings.Contains(tr.Content[0].Text, `"nodes_renamed": 2`) {
		t.Errorf("unexpected response: %s", tr.Content[0].Text)
	}
	if !strings.Contains(tr.Content[0].Text, "old-dom → new-dom") {
		t.Errorf("alias_created missing: %s", tr.Content[0].Text)
	}

	// Old domain should resolve to new domain via alias.
	resolve := call(t, h, "alias", map[string]any{"action": "resolve", "name": "old-dom"})
	mustNotError(t, resolve)
	if !strings.Contains(resolve.Content[0].Text, "new-dom") {
		t.Errorf("alias did not resolve: %s", resolve.Content[0].Text)
	}

	// Nodes should now be searchable under new domain.
	search := call(t, h, "search", map[string]any{"query": "Alpha", "domain": "new-dom"})
	mustNotError(t, search)
	if !strings.Contains(search.Content[0].Text, "Alpha") {
		t.Errorf("node not found in new domain: %s", search.Content[0].Text)
	}
}

func TestRenameDomain_OldDomainNotFound_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "rename_domain", map[string]any{
		"old_domain": "nonexistent",
		"new_domain": "anything",
	})
	mustError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "no live nodes") {
		t.Errorf("unexpected error text: %s", tr.Content[0].Text)
	}
}

func TestRenameDomain_NewDomainAlreadyExists_DirectsToMerge(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha", "domain-a", nil)
	addNode(t, h, "Beta", "domain-b", nil)

	tr := call(t, h, "rename_domain", map[string]any{
		"old_domain": "domain-a",
		"new_domain": "domain-b",
	})
	mustError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "merge_domains") {
		t.Errorf("error should mention merge_domains: %s", tr.Content[0].Text)
	}
}

// TestDomains_ReturnsDomainsAndAliases: domains must return a combined
// response containing domain list and alias list.
func TestDomains_ReturnsDomainsAndAliases(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "A", "alpha", nil)
	addNode(t, h, "B", "beta", nil)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "al", "domain": "alpha"}))
	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)
	out := text(t, tr)
	if !strings.Contains(out, "alpha") {
		t.Error("expected 'alpha' in domains response")
	}
	if !strings.Contains(out, "al") {
		t.Error("expected alias 'al' in domains response")
	}
}

// TestListDomains_IsUnknownTool: list_domains must return an error after consolidation.

// TestListDomains_IsUnknownTool: list_domains must return an error after consolidation.
func TestListDomains_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "list_domains", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestListAliases_IsUnknownTool: list_aliases must return an error after consolidation.

// TestListAliases_IsUnknownTool: list_aliases must return an error after consolidation.
func TestListAliases_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "list_aliases", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// ── alias tool (slice 4) ──────────────────────────────────────────────────────

// TestAlias_Add_RegistersAlias: action=add must register the alias.

// TestAlias_Add_RegistersAlias: action=add must register the alias.
func TestAlias_Add_RegistersAlias(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "mw") {
		t.Errorf("expected alias name in response; got: %s", text(t, tr))
	}
}

// TestAlias_Remove_RemovesAlias: action=remove must remove a registered alias.

// TestAlias_Remove_RemovesAlias: action=remove must remove a registered alias.
func TestAlias_Remove_RemovesAlias(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"}))
	tr := call(t, h, "alias", map[string]any{"action": "remove", "alias": "mw"})
	mustNotError(t, tr)
}

// TestAlias_Resolve_ReturnsCanonical: action=resolve must return the canonical domain.

// TestAlias_Resolve_ReturnsCanonical: action=resolve must return the canonical domain.
func TestAlias_Resolve_ReturnsCanonical(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"}))
	tr := call(t, h, "alias", map[string]any{"action": "resolve", "name": "mw"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "memoryweb") {
		t.Errorf("expected 'memoryweb' in resolve response; got: %s", text(t, tr))
	}
}

// TestAlias_List_ReturnsAllAliases: action=list must return all registered aliases.

// TestAlias_List_ReturnsAllAliases: action=list must return all registered aliases.
func TestAlias_List_ReturnsAllAliases(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"}))
	tr := call(t, h, "alias", map[string]any{"action": "list"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "mw") {
		t.Errorf("expected alias 'mw' in list response; got: %s", text(t, tr))
	}
}

// TestAlias_InvalidAction_ReturnsError: an unknown action must return an error.

// TestAlias_InvalidAction_ReturnsError: an unknown action must return an error.
func TestAlias_InvalidAction_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "badaction"})
	mustError(t, tr)
}

// TestAliasDomain_IsUnknownTool: alias_domain must return an error after consolidation.

// TestAliasDomain_IsUnknownTool: alias_domain must return an error after consolidation.
func TestAliasDomain_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias_domain", map[string]any{"alias": "x", "domain": "y"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestRemoveAlias_IsUnknownTool: remove_alias must return an error after consolidation.

// TestRemoveAlias_IsUnknownTool: remove_alias must return an error after consolidation.
func TestRemoveAlias_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remove_alias", map[string]any{"alias": "x"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestResolveDomain_IsUnknownTool: resolve_domain must return an error after consolidation.

// TestResolveDomain_IsUnknownTool: resolve_domain must return an error after consolidation.
func TestResolveDomain_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "resolve_domain", map[string]any{"name": "x"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// ── forget_all tool ───────────────────────────────────────────────────────────

// TestForgetAll_ArchivesMultipleNodes: forget_all must archive all provided
// IDs in a single transaction; they must no longer appear in search.
