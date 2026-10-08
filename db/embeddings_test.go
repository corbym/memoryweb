package db_test

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

// TestEmbedFieldsForNode_ReturnsLabelAndWhyMatters verifies the per-field
// embedding input map excludes description.
func TestEmbedFieldsForNode_ReturnsLabelAndWhyMatters(t *testing.T) {
	fields := db.EmbedFieldsForNode("my label", "my description", "why it matters")
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(fields), fields)
	}
	if fields["label"] != "my label" {
		t.Errorf("label field: got %q, want %q", fields["label"], "my label")
	}
	if fields["why_matters"] != "why it matters" {
		t.Errorf("why_matters field: got %q, want %q", fields["why_matters"], "why it matters")
	}
	if _, ok := fields["description"]; ok {
		t.Error("description should not be included in per-field embedding inputs")
	}
}

// TestAddNode_StoresFieldEmbeddings verifies that AddNode writes rows to
// node_label_embeddings and node_whymatters_embeddings when the tables exist.
func TestAddNode_StoresFieldEmbeddings(t *testing.T) {
	var count atomic.Int32
	srv := fakeEmbedServer(t, &count)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	n, err := s.AddNode("my label", "my description", "why it matters", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// AddNode should call embed 3 times: concat + label + why_matters.
	if count.Load() < 3 {
		t.Errorf("expected at least 3 embed requests (concat+label+wm); got %d", count.Load())
	}

	var labelCount, wmCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_label_embeddings WHERE node_id = ?`, n.ID).Scan(&labelCount)
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_whymatters_embeddings WHERE node_id = ?`, n.ID).Scan(&wmCount)

	if labelCount != 1 {
		t.Errorf("node_label_embeddings: expected 1 row, got %d", labelCount)
	}
	if wmCount != 1 {
		t.Errorf("node_whymatters_embeddings: expected 1 row, got %d", wmCount)
	}
}

// TestSearchNodesSemantic_PerFieldMinDistance verifies that per-field search
// finds a node whose why_matters embedding is close to the query, even when
// its label and concatenated embeddings are distant — covering the case where
// only why_matters is the relevant field.
func TestSearchNodesSemantic_PerFieldMinDistance(t *testing.T) {
	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	// queryVec and closeVec will match; farVec is orthogonal to them.
	queryVec := makeDenseVector(1)
	farVec := makeDenseVector(100)

	// Node: label is far from query, why_matters is identical to query vector.
	// The concatenated embed text contains "pf-label-far" which maps to farVec,
	// so the legacy node_embeddings entry is distant.  Only the why_matters
	// entry (mapped to queryVec) is close.
	withFakeEmbeddings(t, map[string][]float32{
		"pf-label-far": farVec,
		"pf-wm-close":  queryVec,
	})

	n, err := s.AddNode("pf-label-far", "desc", "pf-wm-close", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Searching for "pf-wm-close" embeds the query as queryVec (distance 0
	// from node_whymatters_embeddings entry), so the node must appear in results.
	result, err := s.SearchNodes("pf-wm-close", "proj", 10, "", nil)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}

	found := false
	for _, nr := range result.Nodes {
		if nr.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected node %s in results (why_matters embedding matches query)", n.ID)
	}
}

func TestQueryPrefix_ArcticEmbed(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "snowflake-arctic-embed")
	want := "Represent this sentence for searching relevant passages: "
	if got := db.QueryPrefix(); got != want {
		t.Errorf("arctic-embed prefix: got %q, want %q", got, want)
	}
}

func TestQueryPrefix_ArcticEmbedLatest(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "snowflake-arctic-embed:latest")
	want := "Represent this sentence for searching relevant passages: "
	if got := db.QueryPrefix(); got != want {
		t.Errorf("arctic-embed:latest prefix: got %q, want %q", got, want)
	}
}

func TestQueryPrefix_MxbaiEmbedLarge(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "mxbai-embed-large")
	want := "Represent this sentence for searching relevant passages: "
	if got := db.QueryPrefix(); got != want {
		t.Errorf("mxbai-embed-large prefix: got %q, want %q", got, want)
	}
}

func TestQueryPrefix_BGE(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "bge-m3")
	if got := db.QueryPrefix(); got != "" {
		t.Errorf("bge-m3 should have no query prefix, got %q", got)
	}
}

func TestQueryPrefix_Unknown(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "some-unknown-model")
	if got := db.QueryPrefix(); got != "" {
		t.Errorf("unknown model should have no query prefix, got %q", got)
	}
}

func TestEmbedTextForNode_MatchesRememberConcatenation(t *testing.T) {
	got := db.EmbedTextForNode("my label", "my desc", "why it matters")
	want := "my label my desc why it matters"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// fakeEmbedServer starts a httptest server that returns a fake 1024-dim embedding
// and counts requests. Caller must call Close() on the server.
func fakeEmbedServer(t *testing.T, requestCount *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		embedding := make([]float32, 1024)
		resp := map[string]any{"embeddings": []any{embedding}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestUpdateNode_LabelChangeTriggersReEmbed(t *testing.T) {
	var count atomic.Int32
	srv := fakeEmbedServer(t, &count)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	n, err := s.AddNode("original label", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	addCount := count.Load()

	label := "new label"
	_, err = s.UpdateNode(n.ID, &label, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	if count.Load() <= addCount {
		t.Error("expected embed request after label change; got none")
	}
}

func TestUpdateNode_DescriptionChangeTriggersReEmbed(t *testing.T) {
	var count atomic.Int32
	srv := fakeEmbedServer(t, &count)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	n, err := s.AddNode("label", "original desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	addCount := count.Load()

	desc := "new description"
	_, err = s.UpdateNode(n.ID, nil, &desc, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	if count.Load() <= addCount {
		t.Error("expected embed request after description change; got none")
	}
}

func TestUpdateNode_TagsOnlyChangeDoesNotReEmbed(t *testing.T) {
	var count atomic.Int32
	srv := fakeEmbedServer(t, &count)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	n, err := s.AddNode("label", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	addCount := count.Load()

	tags := "new-tag"
	_, err = s.UpdateNode(n.ID, nil, nil, nil, &tags, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	if count.Load() > addCount {
		t.Errorf("expected no embed request for tags-only update; got %d extra requests", count.Load()-addCount)
	}
}

func TestUpdateNode_OccurredAtOnlyChangeDoesNotReEmbed(t *testing.T) {
	var count atomic.Int32
	srv := fakeEmbedServer(t, &count)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	n, err := s.AddNode("label", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	addCount := count.Load()

	oa := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	_, err = s.UpdateNode(n.ID, nil, nil, nil, nil, &oa, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	if count.Load() > addCount {
		t.Errorf("expected no embed request for occurred_at-only update; got %d extra requests", count.Load()-addCount)
	}
}

func TestEmbeddingModel_Default(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "")
	got := db.EmbeddingModel()
	if got != "snowflake-arctic-embed" {
		t.Errorf("default model: got %q, want snowflake-arctic-embed", got)
	}
}

func TestEmbeddingModel_EnvOverride(t *testing.T) {
	t.Setenv("MEMORYWEB_EMBED_MODEL", "bge-m3")
	got := db.EmbeddingModel()
	if got != "bge-m3" {
		t.Errorf("env override: got %q, want bge-m3", got)
	}
}

// TestBackfillEmbeddings_OllamaDownFieldCountIsZero verifies that when Ollama
// becomes unavailable before the supplementary per-field pass, the returned
// count is 0 — not inflated by the vacuous success of storeFieldEmbeddings
// called with an empty embedding map.
func TestBackfillEmbeddings_OllamaDownFieldCountIsZero(t *testing.T) {
	var reqCount atomic.Int32
	srv := fakeEmbedServer(t, &reqCount)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}
	if _, err := s.AddNode("test", "desc", "why", "proj", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := s.ClearFieldEmbeddings(); err != nil {
		t.Fatalf("ClearFieldEmbeddings: %v", err)
	}
	// Disable Ollama so embed calls in the supplementary pass all fail.
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")

	n, err := s.BackfillEmbeddings(nil)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}
	if n != 0 {
		t.Errorf("expected count 0 when Ollama unavailable during field pass; got %d", n)
	}
}

// TestBackfillEmbeddings_PartialFieldSuccessNoDuplicateCount verifies that when
// one per-field embed call fails in the main loop (partial success), the node is
// not double-counted when the supplementary pass later completes the missing field.
// This covers the path where embedAndStoreFields stores one field but not the
// other — the node appears in the supplementary pass, but the count must stay at 1.
func TestBackfillEmbeddings_PartialFieldSuccessNoDuplicateCount(t *testing.T) {
	var callCount atomic.Int32
	// Call 1 = probe, call 2 = legacy concat; both succeed.
	// Call 3 = first per-field embed in the main loop — fail to simulate partial
	// success (whether label or why_matters depends on map iteration; either way
	// one field is stored and one is missing, so the node appears in the supp pass).
	// Calls 4+ (second per-field in main loop, both fields in supp pass) succeed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) == 3 {
			http.Error(w, "simulated partial per-field failure", http.StatusInternalServerError)
			return
		}
		embedding := make([]float32, 1024)
		resp := map[string]any{"embeddings": []any{embedding}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	// Add node without embeddings so the main loop has one candidate.
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	if _, err := s.AddNode("test", "desc", "why", "proj", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	n, err := s.BackfillEmbeddings(nil)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}
	// Main loop wrote the legacy embedding (count 1). The supplementary pass
	// completes the missing field embedding for the same node — but because the
	// node was already counted by the main loop, count must remain 1.
	if n != 1 {
		t.Errorf("expected count 1 (no double-count on partial-success path); got %d", n)
	}
}

// TestBackfillEmbeddings_NoDuplicateCountWhenFieldFails verifies that a node
// processed by the main loop (legacy embedding stored) but whose field
// embeddings fail is not double-counted — the supplementary pass must exclude
// nodes already handled by the main loop.
func TestBackfillEmbeddings_NoDuplicateCountWhenFieldFails(t *testing.T) {
	var callCount atomic.Int32
	// BackfillEmbeddings probes with label first (call 1), then embeds the
	// legacy concat text in the main loop (call 2). Succeed for both of those,
	// then fail all subsequent calls (the per-field label + why_matters).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) > 2 {
			http.Error(w, "simulated per-field failure", http.StatusInternalServerError)
			return
		}
		embedding := make([]float32, 1024)
		resp := map[string]any{"embeddings": []any{embedding}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	// Add node without embeddings so the main loop has one candidate.
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	if _, err := s.AddNode("test", "desc", "why", "proj", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	n, err := s.BackfillEmbeddings(nil)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}
	// Main loop wrote the legacy embedding (count 1). Supplementary pass must
	// exclude this node so count stays at 1, not 2.
	if n != 1 {
		t.Errorf("expected count 1 (legacy only, no field double-count); got %d", n)
	}
}

// TestBackfillEmbeddings_DimensionMismatch verifies that BackfillEmbeddings
// returns an error (not silent failure) when the model returns wrong-dim vectors.
func TestBackfillEmbeddings_DimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		embedding := make([]float32, 768) // wrong dimension
		resp := map[string]any{"embeddings": []any{embedding}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	_, _ = s.AddNode("dim test", "desc", "why", "proj", nil, "", "decision")

	_, err := s.BackfillEmbeddings(nil)
	if err == nil {
		t.Fatal("expected error for dimension mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "768") {
		t.Errorf("error should mention wrong dimension; got: %v", err)
	}
}

// TestBackfillEmbeddings_AutoClearOnModelChange verifies that BackfillEmbeddings
// clears existing embeddings when the stored model differs from the current model.
func TestBackfillEmbeddings_AutoClearOnModelChange(t *testing.T) {
	var reqCount atomic.Int32
	srv := fakeEmbedServer(t, &reqCount)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	n, err := s.AddNode("test node", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	// AddNode stored an embedding; verify it's there.
	var embCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&embCount)
	if embCount == 0 {
		t.Skip("sqlite-vec not available; skipping auto-clear test")
	}

	// Record 'old-model' as the stored model in config.
	s.DB().Exec(`INSERT INTO config(key, value) VALUES('embedding_model', 'old-model')`)

	// Switch to a new model, disable Ollama so no new embeddings can be written.
	t.Setenv("MEMORYWEB_EMBED_MODEL", "new-model")
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")

	s.BackfillEmbeddings(nil)

	// Embeddings should be cleared because the model changed.
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&embCount)
	if embCount != 0 {
		t.Errorf("expected node_embeddings cleared after model change; got %d rows", embCount)
	}

	// Config should NOT be updated (run failed — no embeddings written).
	var storedModel string
	s.DB().QueryRow(`SELECT value FROM config WHERE key = 'embedding_model'`).Scan(&storedModel)
	if storedModel != "old-model" {
		t.Errorf("config should remain 'old-model' after failed run; got %q", storedModel)
	}
}

// TestBackfillEmbeddings_WritesModelToConfig verifies that a successful backfill
// (including a no-op when no candidates exist) writes the model to config.
func TestBackfillEmbeddings_WritesModelToConfig(t *testing.T) {
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	t.Setenv("MEMORYWEB_EMBED_MODEL", "")

	s := newStore(t)
	// No nodes — backfill is a no-op but should still record the model.
	_, err := s.BackfillEmbeddings(nil)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}

	var storedModel string
	s.DB().QueryRow(`SELECT value FROM config WHERE key = 'embedding_model'`).Scan(&storedModel)
	if storedModel != "snowflake-arctic-embed" {
		t.Errorf("expected config to record snowflake-arctic-embed; got %q", storedModel)
	}
}

// TestBackfillEmbeddings_SupplementaryPassReturnsCount verifies that when nodes
// already have legacy embeddings but no per-field embeddings (e.g. after upgrading
// from pre-v16), BackfillEmbeddings returns count > 0 and the per-field tables
// are populated. Previously the supplementary pass ran silently and the count
// stayed 0, causing the CLI to print "No nodes needed backfilling."
func TestBackfillEmbeddings_SupplementaryPassReturnsCount(t *testing.T) {
	var reqCount atomic.Int32
	srv := fakeEmbedServer(t, &reqCount)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	_, err := s.AddNode("test node", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Simulate pre-v16: clear per-field tables, keeping legacy intact.
	if err := s.ClearFieldEmbeddings(); err != nil {
		t.Fatalf("ClearFieldEmbeddings: %v", err)
	}

	n, err := s.BackfillEmbeddings(nil)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}
	if n == 0 {
		t.Error("expected BackfillEmbeddings count > 0 for supplementary per-field pass; got 0")
	}

	var labelCount, wmCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_label_embeddings`).Scan(&labelCount)
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_whymatters_embeddings`).Scan(&wmCount)
	if labelCount == 0 {
		t.Error("node_label_embeddings: expected rows after supplementary pass; got 0")
	}
	if wmCount == 0 {
		t.Error("node_whymatters_embeddings: expected rows after supplementary pass; got 0")
	}
}

// TestClearFieldEmbeddings_PreservesLegacy verifies that ClearFieldEmbeddings
// removes per-field vectors but leaves node_embeddings intact.
func TestClearFieldEmbeddings_PreservesLegacy(t *testing.T) {
	var reqCount atomic.Int32
	srv := fakeEmbedServer(t, &reqCount)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	n, err := s.AddNode("test node", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var legacyCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&legacyCount)
	if legacyCount == 0 {
		t.Skip("sqlite-vec not available")
	}

	if err := s.ClearFieldEmbeddings(); err != nil {
		t.Fatalf("ClearFieldEmbeddings: %v", err)
	}

	var labelCount, wmCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_label_embeddings WHERE node_id = ?`, n.ID).Scan(&labelCount)
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_whymatters_embeddings WHERE node_id = ?`, n.ID).Scan(&wmCount)
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&legacyCount)

	if labelCount != 0 {
		t.Errorf("node_label_embeddings: expected 0 after clear, got %d", labelCount)
	}
	if wmCount != 0 {
		t.Errorf("node_whymatters_embeddings: expected 0 after clear, got %d", wmCount)
	}
	if legacyCount != 1 {
		t.Errorf("node_embeddings: expected 1 (preserved), got %d", legacyCount)
	}
}

// makeConstantVector returns a 1024-dim vector where every element equals val.
// Used to produce two clearly distinct blobs so we can tell which generation
// was stored by reading back the first float32.
func makeConstantVector(val float32) []float32 {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = val
	}
	return v
}

// decodeFirstFloat32 interprets the first 4 bytes of a vec0 serialised blob
// as a little-endian IEEE-754 float32. Returns 0 when the slice is too short.
func decodeFirstFloat32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
}

// TestUpdateNode_ReEmbedsReplacesPreviousEmbedding is the regression test for
// the "stale embeddings after revise" bug. Before the fix, sqlite-vec's vec0
// tables rejected INSERT OR REPLACE with a UNIQUE constraint error, so revise
// silently left the old blob in place. After the fix (DELETE + INSERT in a tx)
// the updated embedding must replace the old one in all three tables.
func TestUpdateNode_ReEmbedsReplacesPreviousEmbedding(t *testing.T) {
	oldVec := makeConstantVector(1.0)
	newVec := makeConstantVector(2.0)
	withFakeEmbeddings(t, map[string][]float32{
		"stale-embed-old-label": oldVec,
		"fresh-embed-new-label": newVec,
	})

	s := newStore(t)

	n, err := s.AddNode("stale-embed-old-label", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var embCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&embCount)
	if embCount == 0 {
		t.Skip("sqlite-vec not available; skipping stale-embedding regression test")
	}

	// Sanity: blob should be all-1.0.
	var blob []byte
	s.DB().QueryRow(`SELECT embedding FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&blob)
	if got := decodeFirstFloat32(blob); got != 1.0 {
		t.Errorf("before update: expected first dim 1.0, got %v", got)
	}

	newLabel := "fresh-embed-new-label"
	if _, err := s.UpdateNode(n.ID, &newLabel, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	// After update: blob must be the new embedding (all-2.0), not the stale one.
	s.DB().QueryRow(`SELECT embedding FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&blob)
	if got := decodeFirstFloat32(blob); got != 2.0 {
		t.Errorf("after update: expected first dim 2.0 (new embedding), got %v — stale embedding not replaced", got)
	}

	// Count must remain exactly 1.
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&embCount)
	if embCount != 1 {
		t.Errorf("expected exactly 1 embedding row after update; got %d", embCount)
	}
}

// TestUpdateNode_ReEmbedsReplacesPreviousFieldEmbeddings verifies the same fix
// for the per-field tables (node_label_embeddings, node_whymatters_embeddings).
func TestUpdateNode_ReEmbedsReplacesPreviousFieldEmbeddings(t *testing.T) {
	oldVec := makeConstantVector(3.0)
	newVec := makeConstantVector(4.0)
	withFakeEmbeddings(t, map[string][]float32{
		"field-stale-old": oldVec,
		"field-fresh-new": newVec,
	})

	s := newStore(t)
	if !s.VecFieldsAvailable() {
		t.Skip("per-field embedding tables not available")
	}

	n, err := s.AddNode("field-stale-old", "desc", "field-stale-old", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var labelCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_label_embeddings WHERE node_id = ?`, n.ID).Scan(&labelCount)
	if labelCount == 0 {
		t.Skip("sqlite-vec not available; skipping field stale-embedding regression test")
	}

	// Sanity: label blob should be all-3.0.
	var blob []byte
	s.DB().QueryRow(`SELECT embedding FROM node_label_embeddings WHERE node_id = ?`, n.ID).Scan(&blob)
	if got := decodeFirstFloat32(blob); got != 3.0 {
		t.Errorf("before update: label first dim expected 3.0, got %v", got)
	}

	newLabel := "field-fresh-new"
	wm := "field-fresh-new"
	if _, err := s.UpdateNode(n.ID, &newLabel, nil, &wm, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	// label blob must now be all-4.0.
	s.DB().QueryRow(`SELECT embedding FROM node_label_embeddings WHERE node_id = ?`, n.ID).Scan(&blob)
	if got := decodeFirstFloat32(blob); got != 4.0 {
		t.Errorf("after update: label first dim expected 4.0 (new embedding), got %v — stale label embedding not replaced", got)
	}

	// why_matters blob must also be all-4.0.
	s.DB().QueryRow(`SELECT embedding FROM node_whymatters_embeddings WHERE node_id = ?`, n.ID).Scan(&blob)
	if got := decodeFirstFloat32(blob); got != 4.0 {
		t.Errorf("after update: why_matters first dim expected 4.0 (new embedding), got %v — stale wm embedding not replaced", got)
	}
}

// TestBackfillEmbeddings_NoAutoClearWhenModelUnchanged verifies that existing
// embeddings are preserved when the stored model matches the current model.
func TestBackfillEmbeddings_NoAutoClearWhenModelUnchanged(t *testing.T) {
	var reqCount atomic.Int32
	srv := fakeEmbedServer(t, &reqCount)
	defer srv.Close()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", srv.URL)

	s := newStore(t)
	n, err := s.AddNode("test node", "desc", "why", "proj", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	var embCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&embCount)
	if embCount == 0 {
		t.Skip("sqlite-vec not available; skipping no-clear test")
	}

	// Record the SAME model as current.
	t.Setenv("MEMORYWEB_EMBED_MODEL", "")
	s.DB().Exec(`INSERT INTO config(key, value) VALUES('embedding_model', 'snowflake-arctic-embed')`)

	// Disable Ollama so backfill can't write new embeddings.
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	s.BackfillEmbeddings(nil)

	// Existing embedding must NOT be cleared.
	s.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`, n.ID).Scan(&embCount)
	if embCount != 1 {
		t.Errorf("expected embedding preserved when model unchanged; got %d rows", embCount)
	}
}
