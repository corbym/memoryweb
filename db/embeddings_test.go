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
