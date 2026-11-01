package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newImportPair creates two isolated pre-migrated databases for import tests.
// Both databases are closed; callers open them as needed via newStoreAt or
// db.ImportDB.
func newImportPair(t *testing.T) (srcPath, dstPath string) {
	t.Helper()
	srcPath = filepath.Join(t.TempDir(), "src.db")
	dstPath = filepath.Join(t.TempDir(), "dst.db")
	src, err := newStoreAt(t, srcPath)
	if err != nil {
		t.Fatalf("create src: %v", err)
	}
	src.Close()
	dst, err := newStoreAt(t, dstPath)
	if err != nil {
		t.Fatalf("create dst: %v", err)
	}
	dst.Close()
	return srcPath, dstPath
}

// ── round-trip ────────────────────────────────────────────────────────────────

func TestImportDB_RoundTrip(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, err := newStoreAt(t, srcPath)
	if err != nil {
		t.Fatal(err)
	}
	n1, err := src.AddNode("Alpha", "d1", "w1", "general", nil, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	n2, err := src.AddNode("Beta", "d2", "w2", "general", nil, "", "finding")
	if err != nil {
		t.Fatal(err)
	}
	e, err := src.AddEdge(n1.ID, n2.ID, "led_to", "because beta followed alpha")
	if err != nil {
		t.Fatal(err)
	}
	src.Close()

	// Add an unrelated node to destination so it already has content.
	dstPre, err := newStoreAt(t, dstPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dstPre.AddNode("Gamma", "d3", "w3", "other", nil, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	dstPre.Close()

	report, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{})
	if err != nil {
		t.Fatalf("ImportDB: %v", err)
	}
	if report.ImportedNodes != 2 {
		t.Errorf("ImportedNodes = %d, want 2", report.ImportedNodes)
	}
	if report.ImportedEdges != 1 {
		t.Errorf("ImportedEdges = %d, want 1", report.ImportedEdges)
	}
	if len(report.SkippedNodes) != 0 {
		t.Errorf("SkippedNodes = %d, want 0", len(report.SkippedNodes))
	}

	// Verify IDs and node_kind survived in destination.
	dst, err := newStoreAt(t, dstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()

	got1, err := dst.GetNode(n1.ID)
	if err != nil {
		t.Fatalf("GetNode(%s): %v", n1.ID, err)
	}
	if got1.Node.Label != "Alpha" {
		t.Errorf("node label = %q, want Alpha", got1.Node.Label)
	}
	if got1.Node.NodeKind != "decision" {
		t.Errorf("node_kind = %q, want decision", got1.Node.NodeKind)
	}

	// Edge must be present on the imported node.
	found := false
	for _, eg := range got1.Edges {
		if eg.ID == e.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("edge %s not found in destination node", e.ID)
	}
}

// ── idempotency ───────────────────────────────────────────────────────────────

func TestImportDB_Idempotent(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	n1, _ := src.AddNode("Alpha", "", "", "general", nil, "", "decision")
	n2, _ := src.AddNode("Beta", "", "", "general", nil, "", "decision")
	src.AddEdge(n1.ID, n2.ID, "connects_to", "linked") //nolint:errcheck
	src.Close()

	// First import.
	r1, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if r1.ImportedNodes != 2 {
		t.Errorf("first import: ImportedNodes = %d, want 2", r1.ImportedNodes)
	}
	if r1.ImportedEdges != 1 {
		t.Errorf("first import: ImportedEdges = %d, want 1", r1.ImportedEdges)
	}

	// Second import — must be entirely idempotent.
	r2, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if r2.ImportedNodes != 0 {
		t.Errorf("second import: ImportedNodes = %d, want 0", r2.ImportedNodes)
	}
	if r2.ImportedEdges != 0 {
		t.Errorf("second import: ImportedEdges = %d, want 0", r2.ImportedEdges)
	}
	if len(r2.SkippedNodes) != 2 {
		t.Errorf("second import: SkippedNodes = %d, want 2", len(r2.SkippedNodes))
	}

	// Destination must still have exactly 2 nodes (not doubled).
	dst, _ := newStoreAt(t, dstPath)
	defer dst.Close()
	live, _, _ := dst.NodeCounts()
	if live != 2 {
		t.Errorf("NodeCounts live = %d, want 2 after idempotent import", live)
	}
}

// ── conflict handling ─────────────────────────────────────────────────────────

func TestImportDB_Conflict_Error(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	n, _ := src.AddNode("Alpha", "", "", "general", nil, "", "decision")
	src.Close()

	// Pre-insert a node with the same ID but a different label into destination.
	dst, _ := newStoreAt(t, dstPath)
	dst.DB().Exec( //nolint:errcheck
		`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, node_kind)
		 VALUES (?, 'DifferentLabel', '', '', 'general', datetime('now'), datetime('now'), 'decision')`,
		n.ID,
	)
	dst.Close()

	_, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{OnConflict: "error"})
	if err == nil {
		t.Error("expected conflict error, got nil")
	}
}

func TestImportDB_Conflict_Skip(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	n, _ := src.AddNode("Alpha", "", "", "general", nil, "", "decision")
	src.Close()

	dst, _ := newStoreAt(t, dstPath)
	dst.DB().Exec( //nolint:errcheck
		`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, node_kind)
		 VALUES (?, 'DifferentLabel', '', '', 'general', datetime('now'), datetime('now'), 'decision')`,
		n.ID,
	)
	dst.Close()

	report, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{OnConflict: "skip"})
	if err != nil {
		t.Fatalf("ImportDB: %v", err)
	}
	if report.ImportedNodes != 0 {
		t.Errorf("ImportedNodes = %d, want 0", report.ImportedNodes)
	}
	if len(report.ConflictNodes) != 1 {
		t.Errorf("ConflictNodes = %d, want 1", len(report.ConflictNodes))
	}
	if report.ConflictNodes[0].SourceLabel != "Alpha" {
		t.Errorf("ConflictNodes[0].SourceLabel = %q, want Alpha", report.ConflictNodes[0].SourceLabel)
	}
}

// ── skip-archived ─────────────────────────────────────────────────────────────

func TestImportDB_SkipArchived(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	live, _ := src.AddNode("Live", "", "", "general", nil, "", "decision")
	arch, _ := src.AddNode("Archived", "", "", "general", nil, "", "decision")
	// Add edge first (both nodes must be live for AddEdge to accept it),
	// then archive one endpoint — the import with SkipArchived=true will
	// skip the edge because arch won't be in the imported set.
	src.AddEdge(live.ID, arch.ID, "led_to", "linked") //nolint:errcheck
	src.ArchiveNode(arch.ID, "test")                  //nolint:errcheck
	src.Close()

	report, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{SkipArchived: true})
	if err != nil {
		t.Fatalf("ImportDB: %v", err)
	}
	if report.ImportedNodes != 1 {
		t.Errorf("ImportedNodes = %d, want 1", report.ImportedNodes)
	}
	// Edge is skipped because its to-node was excluded by SkipArchived.
	if report.ImportedEdges != 0 {
		t.Errorf("ImportedEdges = %d, want 0 (to-node was excluded)", report.ImportedEdges)
	}
	if len(report.SkippedEdges) != 1 {
		t.Errorf("SkippedEdges = %d, want 1", len(report.SkippedEdges))
	}
}

func TestImportDB_IncludeArchived(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	src.AddNode("Live", "", "", "general", nil, "", "decision") //nolint:errcheck
	arch, _ := src.AddNode("Archived", "", "", "general", nil, "", "decision")
	src.ArchiveNode(arch.ID, "test") //nolint:errcheck
	src.Close()

	// Default (SkipArchived=false): archived nodes are included.
	report, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{})
	if err != nil {
		t.Fatalf("ImportDB: %v", err)
	}
	if report.ImportedNodes != 2 {
		t.Errorf("ImportedNodes = %d, want 2 (archived included by default)", report.ImportedNodes)
	}
}

// ── dry-run ───────────────────────────────────────────────────────────────────

func TestImportDB_DryRun(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	src.AddNode("Alpha", "", "", "general", nil, "", "decision") //nolint:errcheck
	src.Close()

	report, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportDB dry-run: %v", err)
	}
	if !report.DryRun {
		t.Error("DryRun flag not set in report")
	}
	if report.ImportedNodes != 1 {
		t.Errorf("DryRun ImportedNodes = %d, want 1", report.ImportedNodes)
	}

	// Nothing must have been written to the destination.
	dst, _ := newStoreAt(t, dstPath)
	defer dst.Close()
	live, _, _ := dst.NodeCounts()
	if live != 0 {
		t.Errorf("dry-run wrote %d nodes, want 0", live)
	}
}

// ── timestamp preservation ────────────────────────────────────────────────────

func TestImportDB_PreservesTimestamps(t *testing.T) {
	past := time.Date(2024, 3, 10, 14, 30, 0, 0, time.UTC)

	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	n, err := src.AddNode("Alpha", "", "", "general", &past, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	srcCreated := n.CreatedAt.Truncate(time.Second)
	src.Close()

	_, err = db.ImportDB(srcPath, dstPath, db.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	dst, _ := newStoreAt(t, dstPath)
	defer dst.Close()
	got, err := dst.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Node.OccurredAt == nil || !got.Node.OccurredAt.Equal(past) {
		t.Errorf("occurred_at = %v, want %v", got.Node.OccurredAt, past)
	}
	if !got.Node.CreatedAt.Truncate(time.Second).Equal(srcCreated) {
		t.Errorf("created_at not preserved: got %v, want %v",
			got.Node.CreatedAt.Truncate(time.Second), srcCreated)
	}
}

// ── edge cases ────────────────────────────────────────────────────────────────

func TestImportDB_SameFile_Error(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.db")
	s, _ := newStoreAt(t, path)
	s.Close()

	_, err := db.ImportDB(path, path, db.ImportOptions{})
	if err == nil {
		t.Error("expected error for same source/destination, got nil")
	}
}

func TestImportDB_NonExistentSource_Error(t *testing.T) {
	dstPath := filepath.Join(t.TempDir(), "dst.db")
	s, _ := newStoreAt(t, dstPath)
	s.Close()

	_, err := db.ImportDB("/nonexistent/path/src.db", dstPath, db.ImportOptions{})
	if err == nil {
		t.Error("expected error for nonexistent source, got nil")
	}
}

func TestImportDB_NonStandardRelationship(t *testing.T) {
	srcPath, dstPath := newImportPair(t)

	src, _ := newStoreAt(t, srcPath)
	n1, _ := src.AddNode("A", "", "", "g", nil, "", "decision")
	n2, _ := src.AddNode("B", "", "", "g", nil, "", "decision")
	// Insert an edge with a non-enum relationship type directly at the storage layer.
	src.DB().Exec( //nolint:errcheck
		`INSERT INTO edges (id, from_node, to_node, relationship, narrative, created_at)
		 VALUES ('edge-custom01', ?, ?, 'custom_type', 'custom relationship', datetime('now'))`,
		n1.ID, n2.ID,
	)
	src.Close()

	report, err := db.ImportDB(srcPath, dstPath, db.ImportOptions{})
	if err != nil {
		t.Fatalf("ImportDB: %v", err)
	}
	if report.ImportedEdges != 1 {
		t.Errorf("ImportedEdges = %d, want 1 (custom relationship type)", report.ImportedEdges)
	}

	dst, _ := newStoreAt(t, dstPath)
	defer dst.Close()
	var rel string
	dst.DB().QueryRow(`SELECT relationship FROM edges WHERE id = 'edge-custom01'`).Scan(&rel) //nolint:errcheck
	if rel != "custom_type" {
		t.Errorf("relationship = %q, want custom_type", rel)
	}
}
