package db_test

import (
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// disableOllamaDB sets MEMORYWEB_OLLAMA_ENDPOINT=disabled for the duration of
// the test. Use this in db-layer tests that exercise keyword-only paths in
// SuggestEdges and SearchNodes so that a running Ollama instance does not
// activate the embedding path and interfere with expected results.
func disableOllamaDB(t *testing.T) {
	t.Helper()
	prev := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT")
	os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	t.Cleanup(func() { os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", prev) })
}

// withFakeEmbeddings starts a local HTTP server that mimics Ollama's
// /api/embed endpoint: for each request it looks up the given marker strings
// against the request's Input field and returns the matching deterministic
// vector. Points MEMORYWEB_OLLAMA_ENDPOINT at the fake server for the
// duration of the test, restoring it on cleanup. Use with BackfillEmbeddings
// to populate node_embeddings deterministically without a live Ollama
// instance, so semantic-search-dependent code paths (SuggestEdges,
// FindConflictCandidates) can be exercised in CI.
func withFakeEmbeddings(t *testing.T, vectors map[string][]float32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
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
		http.Error(w, "no matching marker for input: "+req.Input, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	prev := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT")
	os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)
	t.Cleanup(func() { os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", prev) })
}

// testEmbeddingDim matches node_embeddings' FLOAT[1024] column (migration 9).
const testEmbeddingDim = 1024

// spreadSeed hashes a small/adjacent caller-chosen seed (1, 2, 3, ...) into a
// well-distributed 64-bit value before use. math/rand's default source has
// weak seed diffusion for NormFloat64: nearby or simply-related integer
// seeds (e.g. 1 and 2, or a seed and its small multiple) produce
// highly-correlated output sequences, so two "random" vectors built from such
// seeds can come out ~90% cosine-similar instead of ~0% as expected —
// verified empirically; even multiplying by a large prime did not fully fix
// it. Hashing via FNV-1a gives a proper avalanche effect so callers can keep
// using small sequential seeds while the underlying PRNG seeds stay
// decorrelated.
func spreadSeed(seed int64) int64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(seed))
	h.Write(buf[:])
	return int64(h.Sum64())
}

// makeDenseVector returns a deterministic, densely-populated vector (every
// dimension ~N(0,1), like a real embedding) derived from seed. Two different
// seeds give near-orthogonal vectors (cosine distance ≈1) with overwhelming
// probability in 1024 dimensions.
//
// Deliberately NOT unit-normalised: this build of sqlite-vec's
// vec_distance_cosine loses precision on near-unit-norm vectors (per-
// component magnitude ~1/sqrt(1024)≈0.03) and collapses all non-identical
// pairs into a narrow near-zero range regardless of true similarity.
// Realistic, unnormalised N(0,1)-per-component magnitude (matching what a
// real embedding model outputs) avoids this and gives distances matching
// the textbook cosine-distance formula (verified empirically: independent
// vectors ≈1.0, exact negation ≈2.0, identical =0).
func makeDenseVector(seed int64) []float32 {
	r := rand.New(rand.NewSource(spreadSeed(seed)))
	v := make([]float32, testEmbeddingDim)
	for i := range v {
		v[i] = float32(r.NormFloat64())
	}
	return v
}

// TestSuggestEdges_CrossDomainRejectedWhenNoSameDomainCandidate guards against
// the domain-affinity check silently becoming a no-op when a domain has zero
// same-domain embedded candidates. The cross-domain node here has an
// identical embedding to nodeNew (distance 0, the best possible match) —
// even so, with nothing in nodeNew's own domain to calibrate "normal"
// against, it must be excluded: there is no baseline to judge a cross-domain
// match against, so none should be admitted, no matter how close.
func TestSuggestEdges_CrossDomainRejectedWhenNoSameDomainCandidate(t *testing.T) {
	s := newStore(t)
	nodeA := mustAddNode(t, s, "Reference node marker-aff-a", "aff-domain-a")
	nodeNew := mustAddNode(t, s, "New node marker-aff-new", "aff-domain-b")

	newVec := makeDenseVector(10)
	withFakeEmbeddings(t, map[string][]float32{
		"marker-aff-new": newVec,
		"marker-aff-a":   newVec, // identical — distance 0, the closest possible match
	})
	if _, err := s.BackfillEmbeddings(nil); err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}

	results, err := s.SuggestEdges(nodeNew.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, r := range results {
		if r.ID == nodeA.ID {
			t.Errorf("cross-domain node %s should be rejected: aff-domain-b has no same-domain embedded node to calibrate a baseline against, so no cross-domain candidate should be admitted regardless of distance", nodeA.ID)
		}
	}
}

// Note: a complementary "cross-domain candidate meaningfully closer than the
// best same-domain match is still admitted" case was attempted here and
// dropped. The threshold-admission branch itself (haveSameDomain=true routing
// through the existing crossDomainAffinityBoost comparison) is pre-existing
// logic this fix does not change — only the fallback when NO same-domain
// candidate exists was modified, and that is covered by
// TestSuggestEdges_CrossDomainRejectedWhenNoSameDomainCandidate above.
// Constructing a reliable "distance A meaningfully less than distance B"
// fixture for the admission case proved impractical in this environment:
// this build's vec_distance_cosine does not track perturbation magnitude in
// any usable way for vectors sharing a common base — a 4000x magnitude
// spread (0.005 vs 20.0) around the same base vector produced nearly
// identical measured distances (0.096 vs 0.094), while fully independent or
// exactly-negated vectors measure correctly (~1.0 and ~2.0 respectively).

func TestFindConnections_ReturnsBidirectionalEdge(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "Boot crash", "proj")
	b := mustAddNode(t, s, "ULA fix", "proj")
	s.AddEdge(a.ID, b.ID, "unblocks", "direct writes")

	res, err := s.FindConnections("Boot crash", "ULA fix", "proj")
	if err != nil {
		t.Fatalf("FindConnections: %v", err)
	}
	if res.From == nil || res.To == nil {
		t.Fatal("expected non-nil From and To")
	}
	if len(res.Edges) == 0 {
		t.Error("expected at least one edge")
	}
}

func TestFindConnections_NoMatchReturnsNilNodes(t *testing.T) {
	s := newStore(t)
	res, err := s.FindConnections("ghost-a", "ghost-b", "")
	if err != nil {
		t.Fatalf("FindConnections: %v", err)
	}
	if res.From != nil || res.To != nil {
		t.Error("no match should give nil From/To")
	}
}

func TestFindConnections_ArchivedNodeNotMatched(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Visible node", "proj")
	archived := mustAddNode(t, s, "Archived node", "proj")
	s.ArchiveNode(archived.ID, "reason")
	s.AddEdge(n.ID, archived.ID, "connects_to", "link")

	res, err := s.FindConnections("Visible node", "Archived node", "proj")
	if err != nil {
		t.Fatalf("FindConnections: %v", err)
	}
	if res.To != nil {
		t.Error("archived node should not be matched by FindConnections")
	}
}

func TestSuggestEdges_ReturnsOverlappingTagsNode(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("sprint ticket alpha", "d", "w", "proj", nil, "kotlin testing", "")
	nB, _ := s.AddNode("sprint ticket beta", "d", "w", "proj", nil, "kotlin approval", "")
	mustAddNode(t, s, "completely unrelated thing", "proj") // no overlap

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	found := false
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			found = true
			if !strings.Contains(sg.Reason, "kotlin") {
				t.Errorf("reason should mention 'kotlin'; got %q", sg.Reason)
			}
		}
	}
	if !found {
		t.Errorf("node B (%s) should appear in suggestions for overlapping tag 'kotlin'", nB.ID)
	}
}

func TestSuggestEdges_ExcludesSelf(t *testing.T) {
	s := newStore(t)
	n, _ := s.AddNode("self test node kotlin", "d", "w", "proj", nil, "kotlin", "")
	mustAddNode(t, s, "kotlin partner node", "proj") // gives a keyword match

	suggestions, err := s.SuggestEdges(n.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, sg := range suggestions {
		if sg.ID == n.ID {
			t.Error("SuggestEdges should not include the source node itself")
		}
	}
}

func TestSuggestEdges_DomainScoping_DB(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("kotlin build system", "d", "w", "domain-a", nil, "kotlin gradle", "")
	s.AddNode("kotlin build tool", "d", "w", "domain-b", nil, "kotlin gradle", "") // different domain
	nC, _ := s.AddNode("kotlin runner", "d", "w", "domain-a", nil, "kotlin testing", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	ids := make([]string, len(suggestions))
	for i, sg := range suggestions {
		ids[i] = sg.ID
	}
	// domain-a node should appear
	foundC := false
	for _, id := range ids {
		if id == nC.ID {
			foundC = true
		}
	}
	if !foundC {
		t.Errorf("same-domain node (%s) should appear in suggestions; got %v", nC.ID, ids)
	}
}

// TestSuggestEdges_EmDashNotMatchedAsSharedWord: two labels whose only common
// character is an em-dash must not be suggested as connected — the em-dash is
// not a word. Ollama is disabled so only the keyword path runs.
func TestSuggestEdges_EmDashNotMatchedAsSharedWord(t *testing.T) {
	disableOllamaDB(t)
	s := newStore(t)
	nA, _ := s.AddNode("Alpha — gizmo", "d", "w", "proj", nil, "", "")
	nB, _ := s.AddNode("Beta — widget", "d", "w", "proj", nil, "", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			t.Errorf("em-dash must not be treated as a shared word; got suggestion %+v", sg)
		}
	}
}

// TestSuggestEdges_EnDashAndEllipsisNotMatchedAsSharedWord: same shape as the
// em-dash case, using en-dash and ellipsis — any standalone punctuation/symbol
// token must be excluded, not just the em-dash. Ollama is disabled so only the
// keyword path runs.
func TestSuggestEdges_EnDashAndEllipsisNotMatchedAsSharedWord(t *testing.T) {
	disableOllamaDB(t)
	s := newStore(t)
	nA, _ := s.AddNode("Alpha – gizmo…", "d", "w", "proj", nil, "", "")
	nB, _ := s.AddNode("Beta – widget…", "d", "w", "proj", nil, "", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			t.Errorf("en-dash/ellipsis must not be treated as shared words; got suggestion %+v", sg)
		}
	}
}

// TestSuggestEdges_RealWordsStillMatch: regression guard — a genuine shared
// label word must still produce a suggestion with that word in the reason.
func TestSuggestEdges_RealWordsStillMatch(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("Alpha gizmo widget", "d", "w", "proj", nil, "", "")
	nB, _ := s.AddNode("Beta gizmo thing", "d", "w", "proj", nil, "", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	found := false
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			found = true
			if !strings.Contains(sg.Reason, "gizmo") {
				t.Errorf("reason should mention 'gizmo'; got %q", sg.Reason)
			}
		}
	}
	if !found {
		t.Errorf("node B (%s) sharing a real word should appear in suggestions", nB.ID)
	}
}
