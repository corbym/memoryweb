package tools_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestRecentChanges_ReturnsNodes(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Event Alpha", "proj", nil)
	id2 := addNode(t, h, "Event Beta", "proj", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	nodes := resp.Nodes
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("recent_changes missing expected nodes; got %v", ids)
	}
}

func TestRecentChanges_ArchivedNodeExcluded(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Recent archived node", "proj", nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "recent", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	nodes := resp.Nodes
	for _, n := range nodes {
		if n.ID == id {
			t.Error("archived node should not appear in recent_changes")
		}
	}
}

func TestRecentChanges_DomainIsolation(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Alpha event", "domain-a", nil)
	addNode(t, h, "Beta event", "domain-b", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "domain-a"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	nodes := resp.Nodes
	for _, n := range nodes {
		if n.ID != idA {
			t.Errorf("domain-a recent_changes returned node from wrong domain: %s", n.ID)
		}
	}
}

func TestRecentChanges_EmptyDB(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "recent", map[string]any{})
	mustNotError(t, tr)
}

func TestRecentChanges_GroupByDomain_MultipleDomains(t *testing.T) {
	_, h := newEnv(t)
	// Add nodes across three domains.
	idA1 := addNode(t, h, "Alpha one", "domain-a", nil)
	idA2 := addNode(t, h, "Alpha two", "domain-a", nil)
	idB1 := addNode(t, h, "Beta one", "domain-b", nil)
	idC1 := addNode(t, h, "Gamma one", "domain-c", nil)

	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"limit":           5,
	})
	mustNotError(t, tr)

	// Response is {groups, results_truncated}.
	var resp struct {
		Groups []struct {
			Domain string `json:"domain"`
			Nodes  []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"groups"`
		ResultsTruncated bool `json:"results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse grouped response: %v", err)
	}
	groups := resp.Groups

	// Build a flat map of domain → IDs for easy assertion.
	byDomain := map[string][]string{}
	for _, g := range groups {
		for _, n := range g.Nodes {
			byDomain[g.Domain] = append(byDomain[g.Domain], n.ID)
		}
	}

	if !contains(byDomain["domain-a"], idA1) || !contains(byDomain["domain-a"], idA2) {
		t.Errorf("domain-a missing expected nodes; got %v", byDomain["domain-a"])
	}
	if !contains(byDomain["domain-b"], idB1) {
		t.Errorf("domain-b missing expected node; got %v", byDomain["domain-b"])
	}
	if !contains(byDomain["domain-c"], idC1) {
		t.Errorf("domain-c missing expected node; got %v", byDomain["domain-c"])
	}
	if len(groups) < 3 {
		t.Errorf("expected at least 3 domain groups, got %d", len(groups))
	}
}

func TestRecentChanges_GroupByDomain_PerDomainLimit(t *testing.T) {
	_, h := newEnv(t)
	// Add 4 nodes in the same domain.
	for i := 0; i < 4; i++ {
		addNode(t, h, fmt.Sprintf("Node %d", i), "limit-domain", nil)
	}

	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"limit":           2, // per-domain cap
	})
	mustNotError(t, tr)

	var resp struct {
		Groups []struct {
			Domain string `json:"domain"`
			Nodes  []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"groups"`
		ResultsTruncated bool `json:"results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse grouped response: %v", err)
	}

	for _, g := range resp.Groups {
		if g.Domain == "limit-domain" && len(g.Nodes) > 2 {
			t.Errorf("per-domain limit of 2 exceeded: got %d nodes", len(g.Nodes))
		}
	}
	if !resp.ResultsTruncated {
		t.Error("results_truncated should be true when domain has more than limit entries")
	}
}

func TestRecentChanges_GroupByDomain_WithDomainSpecified_BehavesNormal(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Node A", "domain-a", nil)
	addNode(t, h, "Node B", "domain-b", nil)

	// group_by_domain=true but domain is specified → behaves as normal (flat list).
	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"domain":          "domain-a",
	})
	mustNotError(t, tr)

	// Response should be a flat list of nodes (normal mode), not grouped.
	var resp struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("expected wrapped node list when domain is specified: %v\nbody: %s", err, text(t, tr))
	}
	nodes := resp.Nodes
	if len(nodes) != 1 || nodes[0].ID != idA {
		t.Errorf("expected only domain-a node; got %+v", nodes)
	}
}

func TestRecentChanges_GroupByDomain_False_BehavesAsNormal(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Node X", "domain-x", nil)
	id2 := addNode(t, h, "Node Y", "domain-y", nil)

	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": false,
	})
	mustNotError(t, tr)

	// Should be a flat list.
	var resp struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("expected wrapped node list: %v", err)
	}
	nodes := resp.Nodes
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("flat recent_changes missing expected nodes; got %v", ids)
	}
}

// ── timeline ──────────────────────────────────────────────────────────────────

func TestRecent_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	taggedID := addNode(t, h, "TDD story", "proj", map[string]any{"tags": "TDD testing"})
	addNode(t, h, "untagged story", "proj", nil)

	tr := call(t, h, "recent", map[string]any{
		"domain": "proj",
		"tags":   "TDD",
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &resp); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	nodes := resp.Nodes
	if len(nodes) != 1 {
		t.Errorf("expected 1 result, got %d", len(nodes))
	}
	if len(nodes) > 0 && nodes[0]["id"] != taggedID {
		t.Errorf("expected tagged node %q, got %q", taggedID, nodes[0]["id"])
	}
}

func TestRecent_MemoryID_ScopesNeighbourhood(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor", "proj", nil)
	neighbourID := addNode(t, h, "neighbour", "proj", nil)
	unrelatedID := addNode(t, h, "unrelated", "proj", nil)

	call(t, h, "connect", map[string]any{
		"from_memory":  anchorID,
		"to_memory":    neighbourID,
		"relationship": "connects_to",
	})

	tr := call(t, h, "recent", map[string]any{
		"memory_id": anchorID,
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &resp); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	nodes := resp.Nodes
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if id == unrelatedID {
			t.Error("unrelated node should be excluded when memory_id is set")
		}
	}
	if !contains(ids, anchorID) || !contains(ids, neighbourID) {
		t.Errorf("anchor and neighbour should be included, got %v", ids)
	}
}

func TestRecent_TagsAndMemoryID_Combined(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor", "proj", nil)
	taggedNeighbourID := addNode(t, h, "tagged neighbour", "proj", map[string]any{"tags": "TDD"})
	untaggedNeighbourID := addNode(t, h, "untagged neighbour", "proj", nil)

	call(t, h, "connect", map[string]any{"from_memory": anchorID, "to_memory": taggedNeighbourID, "relationship": "connects_to"})
	call(t, h, "connect", map[string]any{"from_memory": anchorID, "to_memory": untaggedNeighbourID, "relationship": "connects_to"})

	tr := call(t, h, "recent", map[string]any{
		"memory_id": anchorID,
		"tags":      "TDD",
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &resp); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	nodes := resp.Nodes
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if contains(ids, untaggedNeighbourID) {
		t.Error("untagged neighbour should be excluded when tags filter is applied")
	}
	if !contains(ids, taggedNeighbourID) {
		t.Error("tagged neighbour should be included")
	}
}

func TestRecent_ExistingBehaviourUnchanged(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	id1 := addNode(t, h, "alpha recent", "proj", nil)
	id2 := addNode(t, h, "beta recent", "proj", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &resp); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	nodes := resp.Nodes
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("both nodes should appear with no scoping, got %v", ids)
	}
}

// TestRecent_SchemaHasTagsAndMemoryID: the recent tool must expose both new
// properties in its input schema.
