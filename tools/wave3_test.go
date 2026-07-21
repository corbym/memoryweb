package tools_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOrient_SignificantLowTrustAnnotation(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "orient-trust-annot"
	assumption := addNode(t, h, "assumption anchor", domain, map[string]any{"node_kind": "assumption"})
	hub := addNode(t, h, "load bearing hub", domain, map[string]any{"node_kind": "decision", "why_matters": "central"})
	for i := 0; i < 3; i++ {
		linker := addNode(t, h, "assumption linker", domain, map[string]any{"node_kind": "assumption"})
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": hub, "relationship": "connects_to",
		}))
	}
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": hub, "to_memory": assumption, "relationship": "depends_on",
	}))

	tr := call(t, h, "orient", map[string]any{"domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, `"trust"`) {
		t.Fatalf("expected trust annotation on low-trust significant node; got:\n%s", body)
	}
	if !strings.Contains(body, "low —") {
		t.Errorf("trust value should use low — prefix; got:\n%s", body)
	}
}

func TestRemember_TrustNudgeOnLowTrustDependency(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "remember-trust-nudge"
	assumption := addNode(t, h, "weak premise", domain, map[string]any{"node_kind": "assumption"})
	for i := 0; i < 2; i++ {
		a := addNode(t, h, "extra assumption", domain, map[string]any{"node_kind": "assumption"})
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": a, "to_memory": assumption, "relationship": "connects_to",
		}))
	}

	tr := call(t, h, "remember", map[string]any{
		"label": "new decision", "domain": domain, "why_matters": "depends on weak base",
		"related_to": []any{assumption},
	})
	mustNotError(t, tr)
	var resp struct {
		TrustNudge string `json:"trust_nudge"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.TrustNudge == "" {
		t.Errorf("expected trust_nudge when filing onto low-trust dependency; got:\n%s", text(t, tr))
	}
}

func TestRemember_NoTrustNudgeWhenDependencyHasNoInbound(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "remember-trust-skip"
	dep := addNode(t, h, "orphan assumption", domain, map[string]any{"node_kind": "assumption"})

	tr := call(t, h, "remember", map[string]any{
		"label": "new decision", "domain": domain, "why_matters": "depends on orphan",
		"related_to": []any{dep},
	})
	mustNotError(t, tr)
	var resp struct {
		TrustNudge string `json:"trust_nudge"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.TrustNudge != "" {
		t.Errorf("expected no trust_nudge for dependency without inbound edges; got %q", resp.TrustNudge)
	}
}

func TestRemember_BatchTrustNudgeAndMisdomainFields(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "remember-batch-trust"
	assumption := addNode(t, h, "batch weak premise", domain, map[string]any{"node_kind": "assumption"})
	support := addNode(t, h, "batch support assumption", domain, map[string]any{"node_kind": "assumption"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": support, "to_memory": assumption, "relationship": "connects_to",
	}))

	tr := call(t, h, "remember", map[string]any{
		"items": []any{
			map[string]any{
				"label": "batch decision", "domain": domain, "why_matters": "batch",
				"related_to": []any{assumption},
			},
		},
	})
	mustNotError(t, tr)
	var resp struct {
		Nodes []struct {
			TrustNudge string `json:"trust_nudge"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Nodes))
	}
	if resp.Nodes[0].TrustNudge == "" {
		t.Errorf("expected batch trust_nudge; got:\n%s", text(t, tr))
	}
}
func TestConnect_ResolvedVerdictStored(t *testing.T) {
	_, h := newEnv(t)
	domain := "connect-verdict"
	a := addNode(t, h, "side A", domain, nil)
	b := addNode(t, h, "side B", domain, nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory": a, "to_memory": b, "relationship": "resolved",
		"verdict": "reconciled", "narrative": "both can stand",
	})
	mustNotError(t, tr)
	var edge struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &edge); err != nil {
		t.Fatalf("parse edge: %v", err)
	}
	if edge.Verdict != "reconciled" {
		t.Errorf("verdict: got %q", edge.Verdict)
	}

	recallTr := call(t, h, "recall", map[string]any{"id": a})
	mustNotError(t, recallTr)
	if !strings.Contains(text(t, recallTr), `"verdict": "reconciled"`) {
		t.Errorf("recall should include verdict; got:\n%s", text(t, recallTr))
	}
}

func TestConnect_VerdictIgnoredOnNonResolved(t *testing.T) {
	_, h := newEnv(t)
	domain := "connect-verdict-ignore"
	a := addNode(t, h, "A", domain, nil)
	b := addNode(t, h, "B", domain, nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory": a, "to_memory": b, "relationship": "depends_on",
		"verdict": "reconciled", "narrative": "not a resolution edge",
	})
	mustNotError(t, tr)
	var edge struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &edge); err != nil {
		t.Fatalf("parse edge: %v", err)
	}
	if edge.Verdict != "" {
		t.Errorf("verdict should not be stored on non-resolved edge; got %q", edge.Verdict)
	}
}

func TestConnect_BatchVerdictStored(t *testing.T) {
	_, h := newEnv(t)
	domain := "connect-batch-verdict"
	a := addNode(t, h, "A batch", domain, nil)
	b := addNode(t, h, "B batch", domain, nil)

	tr := call(t, h, "connect", map[string]any{
		"items": []any{
			map[string]any{
				"from_memory": a, "to_memory": b, "relationship": "resolved",
				"verdict": "false_positive", "narrative": "batch resolution",
			},
		},
	})
	mustNotError(t, tr)

	recallTr := call(t, h, "recall", map[string]any{"id": a})
	mustNotError(t, recallTr)
	if !strings.Contains(text(t, recallTr), `"verdict": "false_positive"`) {
		t.Errorf("batch connect should store verdict on edge; got:\n%s", text(t, recallTr))
	}
}

func TestConnect_InvalidVerdictRejected(t *testing.T) {
	_, h := newEnv(t)
	domain := "connect-verdict-bad"
	a := addNode(t, h, "A", domain, nil)
	b := addNode(t, h, "B", domain, nil)
	tr := call(t, h, "connect", map[string]any{
		"from_memory": a, "to_memory": b, "relationship": "resolved", "verdict": "not-a-real-value",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "false_positive") {
		t.Errorf("error should name valid verdict values; got: %s", text(t, tr))
	}
}

func TestRevise_TrustNudgeOnConnectsToFromRelatedTo(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "revise-trust-connects"
	assumption := addNode(t, h, "weak premise connects", domain, map[string]any{"node_kind": "assumption"})
	for i := 0; i < 2; i++ {
		a := addNode(t, h, "extra assumption", domain, map[string]any{"node_kind": "assumption"})
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": a, "to_memory": assumption, "relationship": "connects_to",
		}))
	}

	tr := call(t, h, "remember", map[string]any{
		"label": "decision from related_to", "domain": domain, "why_matters": "rests on weak base",
		"related_to": []any{assumption},
	})
	mustNotError(t, tr)
	var filed struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &filed); err != nil {
		t.Fatalf("parse remember: %v", err)
	}

	tr = call(t, h, "revise", map[string]any{
		"id": filed.Node.ID, "description": "updated after filing via related_to",
	})
	mustNotError(t, tr)
	var resp struct {
		TrustNudge string `json:"trust_nudge"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.TrustNudge == "" {
		t.Errorf("expected trust_nudge on content-changing revise via connects_to from related_to; got:\n%s", text(t, tr))
	}
}

func TestRevise_TrustNudgeOnContentChange(t *testing.T) {
	disableOllama(t)
	_, store, h := newEnvWithPath(t)
	domain := "revise-trust-nudge"
	assumption := addNode(t, h, "revise weak premise", domain, map[string]any{"node_kind": "assumption"})
	support := addNode(t, h, "revise support", domain, map[string]any{"node_kind": "assumption"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": support, "to_memory": assumption, "relationship": "connects_to",
	}))
	decision := addNode(t, h, "revise hub", domain, map[string]any{"node_kind": "decision"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": decision, "to_memory": assumption, "relationship": "depends_on",
	}))

	nwe, err := store.GetNode(decision)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if len(nwe.Edges) == 0 {
		t.Fatal("expected outbound dependency edge on decision before revise")
	}
	foundDep := false
	for _, e := range nwe.Edges {
		if e.FromNode == decision && e.Relationship == "depends_on" && e.ToNode == assumption {
			foundDep = true
		}
	}
	if !foundDep {
		t.Fatalf("depends_on edge missing; edges=%+v", nwe.Edges)
	}
	assess, err := store.AssessTrustForNodeIDs([]string{assumption}, 90, decision)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	if !assess[assumption].IsLowTrust {
		t.Fatalf("assumption should be low trust before revise: %+v", assess[assumption])
	}

	tr := call(t, h, "revise", map[string]any{
		"id": decision, "description": "updated to reference the weak premise",
	})
	mustNotError(t, tr)
	var resp struct {
		TrustNudge string `json:"trust_nudge"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.TrustNudge == "" {
		t.Errorf("expected trust_nudge on content-changing revise; got:\n%s", text(t, tr))
	}
}

func TestRevise_NoTrustNudgeOnTagsOnly(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "revise-trust-skip"
	assumption := addNode(t, h, "skip weak premise", domain, map[string]any{"node_kind": "assumption"})
	support := addNode(t, h, "skip support", domain, map[string]any{"node_kind": "assumption"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": support, "to_memory": assumption, "relationship": "connects_to",
	}))
	decision := addNode(t, h, "skip hub", domain, map[string]any{"node_kind": "decision"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": decision, "to_memory": assumption, "relationship": "depends_on",
	}))

	tr := call(t, h, "revise", map[string]any{"id": decision, "tags": "meta-only"})
	mustNotError(t, tr)
	var resp struct {
		TrustNudge string `json:"trust_nudge"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.TrustNudge != "" {
		t.Errorf("tags-only revise should not emit trust_nudge; got %q", resp.TrustNudge)
	}
}

func TestRevise_BatchTrustNudge(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "revise-batch-trust"
	assumption := addNode(t, h, "batch revise premise", domain, map[string]any{"node_kind": "assumption"})
	support := addNode(t, h, "batch revise support", domain, map[string]any{"node_kind": "assumption"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": support, "to_memory": assumption, "relationship": "connects_to",
	}))
	decision := addNode(t, h, "batch revise hub", domain, map[string]any{"node_kind": "decision"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": decision, "to_memory": assumption, "relationship": "depends_on",
	}))

	tr := call(t, h, "revise", map[string]any{
		"items": []any{
			map[string]any{"id": decision, "why_matters": "revised significance"},
		},
	})
	mustNotError(t, tr)
	var resp struct {
		Updated []struct {
			TrustNudge string `json:"trust_nudge"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(resp.Updated) != 1 || resp.Updated[0].TrustNudge == "" {
		t.Errorf("expected batch trust_nudge; got:\n%s", text(t, tr))
	}
}

func withFakeEmbeddingsTools(t *testing.T, vectors map[string][]float32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for marker, v := range vectors {
			if strings.Contains(req.Input, marker) {
				_ = json.NewEncoder(w).Encode(map[string][][]float32{"embeddings": {v}})
				return
			}
		}
		http.Error(w, "no matching marker", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	prev := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT")
	os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)
	t.Cleanup(func() { os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", prev) })
}

func TestRemember_PossibleMisdomainWithEmbeddings(t *testing.T) {
	shared := makeDenseVectorTools(77)
	withFakeEmbeddingsTools(t, map[string][]float32{
		"marker-tool-mis-a": shared,
		"marker-tool-mis-b": shared,
	})

	dbPath, store, h := newEnvWithPath(t)
	anchorDomain := "misdomain-canonical"
	newDomain := "misdomain-brand-new"

	addNode(t, h, "Anchor marker-tool-mis-a", anchorDomain, nil)
	if _, err := store.BackfillEmbeddings(nil); err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}

	tr := call(t, h, "remember", map[string]any{
		"label": "New marker-tool-mis-b", "domain": newDomain, "why_matters": "maybe wrong domain",
	})
	mustNotError(t, tr)
	var resp struct {
		PossibleMisdomain bool   `json:"possible_misdomain"`
		SuggestedDomain   string `json:"suggested_domain"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !resp.PossibleMisdomain {
		t.Errorf("expected possible_misdomain after embeddings backfill; got:\n%s", text(t, tr))
	}
	if resp.SuggestedDomain != anchorDomain {
		t.Errorf("SuggestedDomain: got %q want %q", resp.SuggestedDomain, anchorDomain)
	}

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer rawDB.Close()
	var flagged int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'domain_creation_flagged'`).Scan(&flagged); err != nil {
		t.Fatalf("audit_log: %v", err)
	}
	if flagged == 0 {
		t.Error("expected domain_creation_flagged audit row")
	}
}

func makeDenseVectorTools(seed int64) []float32 {
	const dim = 1024
	v := make([]float32, dim)
	// Identical vectors for matched markers — distance 0 for cross-domain KNN.
	for i := range v {
		v[i] = float32((int(seed)*131+i)%500+1) / 100
	}
	return v
}

func TestAudit_KindCoverage(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "kind-coverage-audit"
	addNode(t, h, "We found that the API returns 404", domain, map[string]any{
		"description": "confirmed in integration tests",
	})
	addNode(t, h, "ordinary decision", domain, nil)

	tr := call(t, h, "audit", map[string]any{"mode": "kind_coverage", "domain": domain})
	mustNotError(t, tr)
	var resp struct {
		TotalNodes          int            `json:"total_nodes"`
		ByKind              map[string]int `json:"by_kind"`
		MigrationCandidates []struct {
			Label string `json:"label"`
		} `json:"migration_candidates"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v\n%s", err, text(t, tr))
	}
	if resp.TotalNodes != 2 {
		t.Errorf("total_nodes: got %d", resp.TotalNodes)
	}
	if len(resp.MigrationCandidates) == 0 {
		t.Errorf("expected migration candidate for finding-like decision text; got:\n%s", text(t, tr))
	}
	body := text(t, tr)
	if strings.Contains(body, `"description"`) {
		t.Errorf("migration_candidates must be lean (no description); got:\n%s", body)
	}
}

func TestAudit_KindCoverageTruncation(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	domain := "kind-coverage-trunc-tools"
	for i := 0; i < 3; i++ {
		addNode(t, h, "We found that API issue", domain, map[string]any{
			"description": "confirmed repeatedly",
		})
	}

	tr := call(t, h, "audit", map[string]any{"mode": "kind_coverage", "domain": domain, "limit": 1})
	mustNotError(t, tr)
	var resp struct {
		ResultsTruncated bool `json:"results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !resp.ResultsTruncated {
		t.Errorf("expected results_truncated with limit=1 and multiple candidates; got:\n%s", text(t, tr))
	}
}
