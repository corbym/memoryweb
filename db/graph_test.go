package db_test

import (
	"database/sql"
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

func TestFindConnections_NoMatchErrors(t *testing.T) {
	s := newStore(t)
	_, err := s.FindConnections("ghost-a", "ghost-b", "")
	if err == nil {
		t.Fatal("expected error when labels do not match")
	}
	if !strings.Contains(err.Error(), "ghost-a") && !strings.Contains(err.Error(), "ghost-b") {
		t.Errorf("error should name unmatched label; got: %v", err)
	}
}

func TestFindConnections_ArchivedNodeNotMatched(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Visible node", "proj")
	archived := mustAddNode(t, s, "Archived node", "proj")
	s.ArchiveNode(archived.ID, "reason")
	s.AddEdge(n.ID, archived.ID, "connects_to", "link")

	_, err := s.FindConnections("Visible node", "Archived node", "proj")
	if err == nil {
		t.Fatal("archived node should not be matched by FindConnections")
	}
	if !strings.Contains(err.Error(), "Archived node") {
		t.Errorf("error should name unmatched archived label; got: %v", err)
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

func TestFindMisdomainCandidate_CrossDomainMatch(t *testing.T) {
	s := newStore(t)
	anchor, _ := s.AddNode("Anchor marker-mis-a content", "d", "w", "canonical-domain", nil, "", "decision")
	newNode, _ := s.AddNode("New marker-mis-b content", "d", "w", "brand-new-domain", nil, "", "decision")

	sharedVec := makeDenseVector(50)
	withFakeEmbeddings(t, map[string][]float32{
		"marker-mis-a": sharedVec,
		"marker-mis-b": sharedVec,
	})
	if _, err := s.BackfillEmbeddings(nil); err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}

	candidate, err := s.FindMisdomainCandidate(newNode.ID, "brand-new-domain", nil)
	if err != nil {
		t.Fatalf("FindMisdomainCandidate: %v", err)
	}
	if candidate == nil {
		t.Fatal("expected cross-domain misdomain candidate")
	}
	if candidate.SuggestedDomain != "canonical-domain" {
		t.Errorf("SuggestedDomain: got %q want canonical-domain", candidate.SuggestedDomain)
	}
	if candidate.SuggestedMemoryID != anchor.ID {
		t.Errorf("SuggestedMemoryID: got %q want %s", candidate.SuggestedMemoryID, anchor.ID)
	}
}

// TestFindMisdomainCandidate_NilReturnedWhenMinority: when the requested domain
// has strictly more KNN neighbours than any other domain, FindMisdomainCandidate
// must return nil (majority rule — STORY-015).
func TestFindMisdomainCandidate_NilReturnedWhenMinority(t *testing.T) {
	s := newStore(t)
	// 3 anchors in the majority (requested) domain.
	for i := range 3 {
		_ = i
		s.AddNode("Anchor marker-maj-rule content", "d", "w", "canonical-domain", nil, "", "decision")
	}
	// 1 anchor in a different domain — minority.
	s.AddNode("OtherAnchor marker-maj-rule content", "d", "w", "other-domain", nil, "", "decision")
	// New node filed into canonical-domain (the majority).
	newNode, _ := s.AddNode("New marker-maj-rule content", "d", "w", "canonical-domain", nil, "", "decision")

	sharedVec := makeDenseVector(52)
	withFakeEmbeddings(t, map[string][]float32{"marker-maj-rule": sharedVec})
	if _, err := s.BackfillEmbeddings(nil); err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}

	candidate, err := s.FindMisdomainCandidate(newNode.ID, "canonical-domain", nil)
	if err != nil {
		t.Fatalf("FindMisdomainCandidate: %v", err)
	}
	if candidate != nil {
		t.Errorf("expected nil when majority of KNN are in the requested domain; got %+v", candidate)
	}
}

// TestFindMisdomainCandidate_ExcludeIDs_SuppressesFlag: when the only cross-domain
// KNN anchor is passed in excludeIDs, FindMisdomainCandidate must return nil
// (batch-sibling exclusion — STORY-016).
func TestFindMisdomainCandidate_ExcludeIDs_SuppressesFlag(t *testing.T) {
	s := newStore(t)
	anchor, _ := s.AddNode("Anchor marker-excl-ids content", "d", "w", "canonical-domain", nil, "", "decision")
	newNode, _ := s.AddNode("New marker-excl-ids content", "d", "w", "brand-new-domain", nil, "", "decision")

	sharedVec := makeDenseVector(53)
	withFakeEmbeddings(t, map[string][]float32{"marker-excl-ids": sharedVec})
	if _, err := s.BackfillEmbeddings(nil); err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}

	// Without exclusion the anchor is a cross-domain neighbour — should flag.
	candidate, err := s.FindMisdomainCandidate(newNode.ID, "brand-new-domain", nil)
	if err != nil {
		t.Fatalf("FindMisdomainCandidate (no exclude): %v", err)
	}
	if candidate == nil {
		t.Fatal("expected cross-domain candidate without exclusion")
	}

	// With anchor excluded: no in-floor neighbours remain → no flag.
	candidate, err = s.FindMisdomainCandidate(newNode.ID, "brand-new-domain", []string{anchor.ID})
	if err != nil {
		t.Fatalf("FindMisdomainCandidate (with exclude): %v", err)
	}
	if candidate != nil {
		t.Errorf("expected nil when cross-domain anchor is excluded; got %+v", candidate)
	}
}

// TestFindMisdomainCandidate_TieBreaking_Deterministic: when two non-requested
// domains each have exactly the same number of in-floor KNN neighbours, the
// domain whose nearest neighbour is closest (lower distance) must win — and the
// result must be the same on every call (no map-order non-determinism).
func TestFindMisdomainCandidate_TieBreaking_Deterministic(t *testing.T) {
	s := newStore(t)
	if !s.VecAvailable() {
		t.Skip("sqlite-vec not available")
	}
	// Requested domain: 1 anchor — fewer than either rival.
	reqAnchor, _ := s.AddNode("Requested anchor content", "d", "w", "tiebreak-req", nil, "", "decision")
	// Domain A: 2 anchors — will get nearVec (identical to newNode) → cos_dist=0.
	anchorA1, _ := s.AddNode("DomA1 anchor content", "d", "w", "tiebreak-a", nil, "", "decision")
	anchorA2, _ := s.AddNode("DomA2 anchor content", "d", "w", "tiebreak-a", nil, "", "decision")
	// Domain B: 2 anchors — will get domainBVec → cos_dist ≈ 0.293 (within floor 0.5).
	anchorB1, _ := s.AddNode("DomB1 anchor content", "d", "w", "tiebreak-b", nil, "", "decision")
	anchorB2, _ := s.AddNode("DomB2 anchor content", "d", "w", "tiebreak-b", nil, "", "decision")
	// New node filed into requested domain.
	newNode, _ := s.AddNode("New node content", "d", "w", "tiebreak-req", nil, "", "decision")

	// nearVec (seed 55) for domain-a and newNode: cos_dist(nearVec, nearVec) = 0.
	// domainBVec = nearVec + orthVec (seed 56): cos_dist ≈ 0.293, within floor 0.5.
	//   Both rivals end up with count=2; requested has count=1. The tie-break must
	//   pick domain-a (nearer first anchor, dist=0) over domain-b (dist≈0.293).
	nearVec := makeDenseVector(55)
	orthVec := makeDenseVector(56)
	domainBVec := make([]float32, len(nearVec))
	for i := range nearVec {
		domainBVec[i] = nearVec[i] + orthVec[i]
	}
	// Use insertEmbeddingsDirect for precision: bypasses HTTP matching and assigns
	// each vector to its node ID exactly, so distances are unambiguous.
	insertEmbeddingsDirect(t, s, map[string][]float32{
		reqAnchor.ID: nearVec,
		anchorA1.ID:  nearVec,
		anchorA2.ID:  nearVec,
		anchorB1.ID:  domainBVec,
		anchorB2.ID:  domainBVec,
		newNode.ID:   nearVec,
	})

	// Run 20 times; Go's map iteration is random so non-determinism shows quickly.
	// NOTE: We assert only SuggestedDomain stability, not SuggestedMemoryID — when
	// two nodes in the winning domain have equal distances (both nearVec here), the
	// specific node returned depends on SQLite row order, which is implementation-
	// defined. The equal-nearestDist edge case (two rival domains at exactly dist=0)
	// is not covered by this test; that case remains order-dependent.
	var firstDomain string
	for i := range 20 {
		candidate, err := s.FindMisdomainCandidate(newNode.ID, "tiebreak-req", nil)
		if err != nil {
			t.Fatalf("FindMisdomainCandidate iter %d: %v", i, err)
		}
		if candidate == nil {
			t.Fatalf("iter %d: expected misdomain candidate (count-a=2, count-b=2 > requested=1)", i)
		}
		if i == 0 {
			firstDomain = candidate.SuggestedDomain
			if firstDomain != "tiebreak-a" {
				t.Errorf("tiebreak-a should win (nearer anchor %s); got domain=%q", anchorA1.ID, firstDomain)
			}
		} else if candidate.SuggestedDomain != firstDomain {
			t.Errorf("non-deterministic tie-break: iter 0 gave domain=%s, iter %d gave domain=%s",
				firstDomain, i, candidate.SuggestedDomain)
		}
	}
}

func TestLogMisdomainFlagged_WritesAuditRow(t *testing.T) {
	dbPath, s := newStoreAtPath(t)
	n := mustAddNode(t, s, "new domain seed", "fresh-domain")
	if err := s.LogMisdomainFlagged(n.ID, n.Label, "suggested domain \"other\""); err != nil {
		t.Fatalf("LogMisdomainFlagged: %v", err)
	}
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open rawDB: %v", err)
	}
	defer rawDB.Close()
	var action string
	if err := rawDB.QueryRow(`SELECT action FROM audit_log WHERE node_id = ?`, n.ID).Scan(&action); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if action != "misdomain_flagged" {
		t.Errorf("action: got %q", action)
	}
}
