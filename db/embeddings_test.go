package db_test

import (
	"encoding/json"
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
