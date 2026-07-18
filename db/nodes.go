package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Node struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	WhyMatters  string     `json:"why_matters"`
	Tags        string     `json:"tags,omitempty"`
	Domain      string     `json:"domain"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	OccurredAt  *time.Time `json:"occurred_at,omitempty"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"` // nil = live
	NodeKind    string     `json:"node_kind,omitempty"`
}

type NodeWithEdges struct {
	Node  Node   `json:"node"`
	Edges []Edge `json:"edges"`
}

// NodeInput is the input type for AddNodesBatch.
type NodeInput struct {
	Label       string
	Description string
	WhyMatters  string
	Tags        string
	Domain      string
	OccurredAt  *time.Time
	NodeKind    string
}

func (s *Store) AddNode(label, description, whyMatters, domain string, occurredAt *time.Time, tags string, nodeKind string) (*Node, error) {
	domain = s.ResolveAlias(domain)
	id := slug(label) + "-" + shortID()
	now := time.Now().UTC()

	if nodeKind == "" {
		nodeKind = "decision"
	}

	// Atomically insert the node and (when occurred_at is set) its audit row.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, tags, node_kind)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, label, description, whyMatters, domain, now, now, occurredAt, tags, nodeKind,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if occurredAt != nil {
		provenance := "agent-assigned"
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "occurred_at_set", id, label, provenance, now,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Generate and store an embedding for semantic search (best-effort, after commit).
	if embedding, err := embed(label + " " + description + " " + whyMatters); err == nil {
		s.storeEmbedding(id, embedding)
	}

	return &Node{
		ID:          id,
		Label:       label,
		Description: description,
		WhyMatters:  whyMatters,
		Tags:        tags,
		Domain:      domain,
		CreatedAt:   now,
		UpdatedAt:   now,
		OccurredAt:  occurredAt,
		NodeKind:    nodeKind,
	}, nil
}

func (s *Store) GetNode(id string) (*NodeWithEdges, error) {
	var n Node
	var oa sql.NullTime
	var aa sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("node not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	n.OccurredAt = nullTimeToPtr(oa)
	n.ArchivedAt = nullTimeToPtr(aa)

	rows, err := s.db.Query(
		`SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges
		 WHERE from_node = ? OR to_node = ?`, id, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		rows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt)
		edges = append(edges, e)
	}

	return &NodeWithEdges{Node: n, Edges: edges}, nil
}

// ── update ────────────────────────────────────────────────────────────────────

// UpdateNode merges the provided (non-nil) fields into an existing live node.
// Writes an audit_log entry recording which fields changed and their old values.
// Returns the full updated node. Returns an error if the node does not exist or
// has been archived.
func (s *Store) UpdateNode(id string, label, description, whyMatters, tags *string, occurredAt *time.Time, nodeKind *string, domain *string, moveReason *string) (*Node, error) {
	// Fetch current values for comparison and audit trail.
	var cur Node
	var curOA, curAA sql.NullTime
	if err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&cur.ID, &cur.Label, &cur.Description, &cur.WhyMatters, &cur.Domain,
		&cur.CreatedAt, &cur.UpdatedAt, &curOA, &curAA, &cur.Tags, &cur.NodeKind); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", id)
		}
		return nil, err
	}
	cur.OccurredAt = nullTimeToPtr(curOA)

	now := time.Now().UTC()
	sets := []string{"updated_at = ?"}
	args := []interface{}{now}

	// Build audit reason describing each changed field with its old value.
	var changes []string

	applyStringField(label, cur.Label, "label", "label", &sets, &changes, &args)
	applyStringField(description, cur.Description, "description", "description", &sets, &changes, &args)
	applyStringField(whyMatters, cur.WhyMatters, "why_matters", "why_matters", &sets, &changes, &args)
	applyStringField(tags, cur.Tags, "tags", "tags", &sets, &changes, &args)
	if occurredAt != nil {
		sets = append(sets, "occurred_at = ?")
		args = append(args, *occurredAt)
		oldVal := "(none)"
		if cur.OccurredAt != nil {
			oldVal = cur.OccurredAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		changes = append(changes, fmt.Sprintf("occurred_at (was %s)", oldVal))
	}
	if nodeKind != nil {
		validKinds := map[string]bool{
			"transient": true, "reference": true, "issue": true,
			"decision": true, "option": true, "assumption": true,
			"finding": true, "standing": true, "goal": true,
		}
		if !validKinds[*nodeKind] {
			return nil, fmt.Errorf("invalid node_kind %q: must be one of transient, reference, issue, decision, option, assumption, finding, standing, goal", *nodeKind)
		}
		sets = append(sets, "node_kind = ?")
		args = append(args, *nodeKind)
		if *nodeKind != cur.NodeKind {
			changes = append(changes, fmt.Sprintf("node_kind (was %q)", cur.NodeKind))
		}
	}
	if domain != nil {
		resolved := s.ResolveAlias(*domain)
		if resolved != cur.Domain {
			if moveReason == nil || strings.TrimSpace(*moveReason) == "" {
				return nil, fmt.Errorf("reason is required when changing domain")
			}
			sets = append(sets, "domain = ?")
			args = append(args, resolved)
			changes = append(changes, fmt.Sprintf("domain (was %s → %s): %s", cur.Domain, resolved, strings.TrimSpace(*moveReason)))
		}
	}
	args = append(args, id)

	reason := "no fields changed"
	if len(changes) > 0 {
		reason = "changed: " + strings.Join(changes, "; ")
	}
	var provenance *string
	if occurredAt != nil {
		p := "agent-assigned"
		provenance = &p
	}

	// Atomically update the node and write the audit row in a single transaction.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`UPDATE nodes SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
		args...,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "update", id, cur.Label, reason, provenance, now,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Re-fetch the updated node.
	var n Node
	var oa, aa sql.NullTime
	if err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE id = ?`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind); err != nil {
		return nil, err
	}
	n.OccurredAt = nullTimeToPtr(oa)
	n.ArchivedAt = nullTimeToPtr(aa)
	return &n, nil
}

// NodeUpdateInput is a single entry in an UpdateNodesBatch call.
type NodeUpdateInput struct {
	ID          string
	Label       *string
	Description *string
	WhyMatters  *string
	Tags        *string
	OccurredAt  *time.Time
	NodeKind    *string
	Domain      *string
	Reason      *string
}

// UpdateNodesBatch updates multiple nodes in a single transaction.
// All updates succeed or all are rolled back.
func (s *Store) UpdateNodesBatch(inputs []NodeUpdateInput) ([]*Node, error) {
	if len(inputs) == 0 {
		return []*Node{}, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	nodes := make([]*Node, 0, len(inputs))

	for _, inp := range inputs {
		var cur Node
		var curOA, curAA sql.NullTime
		if err := tx.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
			 FROM nodes WHERE id = ? AND archived_at IS NULL`, inp.ID,
		).Scan(&cur.ID, &cur.Label, &cur.Description, &cur.WhyMatters, &cur.Domain,
			&cur.CreatedAt, &cur.UpdatedAt, &curOA, &curAA, &cur.Tags, &cur.NodeKind); err != nil {
			tx.Rollback()
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("node not found: %s", inp.ID)
			}
			return nil, err
		}
		cur.OccurredAt = nullTimeToPtr(curOA)

		sets := []string{"updated_at = ?"}
		args := []interface{}{now}
		var changes []string

		applyStringField(inp.Label, cur.Label, "label", "label", &sets, &changes, &args)
		applyStringField(inp.Description, cur.Description, "description", "description", &sets, &changes, &args)
		applyStringField(inp.WhyMatters, cur.WhyMatters, "why_matters", "why_matters", &sets, &changes, &args)
		applyStringField(inp.Tags, cur.Tags, "tags", "tags", &sets, &changes, &args)
		if inp.OccurredAt != nil {
			sets = append(sets, "occurred_at = ?")
			args = append(args, *inp.OccurredAt)
			oldVal := "(none)"
			if cur.OccurredAt != nil {
				oldVal = cur.OccurredAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			changes = append(changes, fmt.Sprintf("occurred_at (was %s)", oldVal))
		}
		if inp.NodeKind != nil {
			sets = append(sets, "node_kind = ?")
			args = append(args, *inp.NodeKind)
			if *inp.NodeKind != cur.NodeKind {
				changes = append(changes, fmt.Sprintf("node_kind (was %q)", cur.NodeKind))
			}
		}
		if inp.Domain != nil {
			resolved := s.ResolveAlias(*inp.Domain)
			if resolved != cur.Domain {
				if inp.Reason == nil || strings.TrimSpace(*inp.Reason) == "" {
					tx.Rollback()
					return nil, fmt.Errorf("reason is required when changing domain")
				}
				sets = append(sets, "domain = ?")
				args = append(args, resolved)
				changes = append(changes, fmt.Sprintf("domain (was %s → %s): %s", cur.Domain, resolved, strings.TrimSpace(*inp.Reason)))
			}
		}
		args = append(args, inp.ID)

		if _, err := tx.Exec(
			`UPDATE nodes SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
			args...,
		); err != nil {
			tx.Rollback()
			return nil, err
		}

		reason := "no fields changed"
		if len(changes) > 0 {
			reason = "changed: " + strings.Join(changes, "; ")
		}
		var batchProvenance *string
		if inp.OccurredAt != nil {
			p := "agent-assigned"
			batchProvenance = &p
		}
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, reason, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "update", inp.ID, cur.Label, reason, batchProvenance, now,
		); err != nil {
			tx.Rollback()
			return nil, err
		}

		// Re-fetch within the tx.
		var n Node
		var oa, aa sql.NullTime
		if err := tx.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
			 FROM nodes WHERE id = ?`, inp.ID,
		).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind); err != nil {
			tx.Rollback()
			return nil, err
		}
		n.OccurredAt = nullTimeToPtr(oa)
		n.ArchivedAt = nullTimeToPtr(aa)
		nodes = append(nodes, &n)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nodes, nil
}

// ── batch insert ──────────────────────────────────────────────────────────────

// AddNodesBatch inserts all nodes in a single transaction.
// If any node fails validation or insertion the transaction is rolled back.
func (s *Store) AddNodesBatch(inputs []NodeInput) ([]*Node, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	nodes := make([]*Node, 0, len(inputs))
	for i, inp := range inputs {
		if inp.Label == "" {
			tx.Rollback()
			return nil, fmt.Errorf("node %d: label is required", i)
		}
		if inp.Domain == "" {
			tx.Rollback()
			return nil, fmt.Errorf("node %d: domain is required", i)
		}
		domain := s.ResolveAlias(inp.Domain)
		id := slug(inp.Label) + "-" + shortID()
		nodeKind := inp.NodeKind
		if nodeKind == "" {
			nodeKind = "decision"
		}
		if _, err := tx.Exec(
			`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, tags, node_kind)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, inp.Label, inp.Description, inp.WhyMatters, domain, now, now, inp.OccurredAt, inp.Tags, nodeKind,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
		nodes = append(nodes, &Node{
			ID:          id,
			Label:       inp.Label,
			Description: inp.Description,
			WhyMatters:  inp.WhyMatters,
			Tags:        inp.Tags,
			Domain:      domain,
			CreatedAt:   now,
			UpdatedAt:   now,
			OccurredAt:  inp.OccurredAt,
			NodeKind:    nodeKind,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Audit occurred_at provenance for any nodes where it was set (best-effort, after commit).
	for _, n := range nodes {
		if n.OccurredAt != nil {
			now2 := time.Now().UTC()
			provenance := "agent-assigned"
			_, _ = s.db.Exec(
				`INSERT INTO audit_log (id, action, node_id, node_label, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
				"auditlog-"+shortID(), "occurred_at_set", n.ID, n.Label, provenance, now2,
			)
		}
	}

	// Generate and store embeddings for each node (best-effort, after commit).
	for _, n := range nodes {
		text := n.Label + " " + n.Description + " " + n.WhyMatters
		if embedding, err := embed(text); err == nil {
			s.storeEmbedding(n.ID, embedding)
		}
	}

	return nodes, nil
}

// ── archive / restore ─────────────────────────────────────────────────────────

// ArchiveNode soft-deletes a node by setting archived_at and records an audit_log entry.
func (s *Store) ArchiveNode(id, reason string) error {
	now := time.Now().UTC()

	var label string
	if err := s.db.QueryRow(`SELECT label FROM nodes WHERE id = ?`, id).Scan(&label); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("node not found: %s", id)
		}
		return err
	}

	if _, err := s.db.Exec(`UPDATE nodes SET archived_at = ? WHERE id = ?`, now, id); err != nil {
		return err
	}

	_, err := s.db.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "archive", id, label, reason, now,
	)
	return err
}

// ArchiveNodesBatch archives multiple nodes in a single transaction.
// If any node ID does not exist, the whole transaction is rolled back and an
// error is returned — no nodes are archived on partial failure.
func (s *Store) ArchiveNodesBatch(items []struct{ ID, Reason string }) error {
	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, item := range items {
		var label string
		if err := tx.QueryRow(`SELECT label FROM nodes WHERE id = ?`, item.ID).Scan(&label); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("node not found: %s", item.ID)
			}
			return err
		}
		if _, err := tx.Exec(`UPDATE nodes SET archived_at = ? WHERE id = ?`, now, item.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "archive", item.ID, label, item.Reason, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RestoreNode clears archived_at on a node and records an audit_log entry.
func (s *Store) RestoreNode(id string) error {
	now := time.Now().UTC()

	var label string
	if err := s.db.QueryRow(`SELECT label FROM nodes WHERE id = ?`, id).Scan(&label); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("node not found: %s", id)
		}
		return err
	}

	if _, err := s.db.Exec(`UPDATE nodes SET archived_at = NULL WHERE id = ?`, id); err != nil {
		return err
	}

	_, err := s.db.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "restore", id, label, nil, now,
	)
	return err
}

// ListArchived returns all archived nodes, optionally filtered by domain.
func (s *Store) ListArchived(domain string, tags, nodeKinds []string) ([]Node, error) {
	domain = s.ResolveAlias(domain)

	conds := []string{"archived_at IS NOT NULL"}
	args := []interface{}{}

	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	conds, args = tagFilter("tags", tags, conds, args)
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY archived_at DESC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodeRows(rows)
}

// CountNodes returns the number of live (non-archived) nodes in a domain.
func (s *Store) CountNodes(domain string) (int, error) {
	domain = s.ResolveAlias(domain)
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`,
		domain,
	).Scan(&count)
	return count, err
}

// CountArchived returns the number of archived nodes in a domain.
func (s *Store) CountArchived(domain string) (int, error) {
	domain = s.ResolveAlias(domain)
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NOT NULL`,
		domain,
	).Scan(&count)
	return count, err
}

// ── possible duplicates ───────────────────────────────────────────────────────

// FindPossibleDuplicates returns live nodes in the same domain whose normalised
// label closely matches the given label (lowercased, punctuation stripped).
// The node with the given excludeID is excluded (used to avoid self-match).
func (s *Store) FindPossibleDuplicates(label, domain, excludeID string) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	norm := normaliseLabel(label)
	if norm == "" {
		return []Node{}, nil
	}
	rows, err := s.db.Query(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE domain = ? AND archived_at IS NULL AND id != ?`,
		domain, excludeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanNodeRows(rows)
	if err != nil {
		return nil, err
	}
	results := filter(all, func(n Node) bool { return normaliseLabel(n.Label) == norm })
	if results == nil {
		results = []Node{}
	}
	return results, nil
}

// normaliseLabel lowercases a label and strips non-alphanumeric characters
// (except spaces) so "Boot Crash!" and "boot crash" compare equal.
func normaliseLabel(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// GetStandingNodes returns live nodes with node_kind = 'standing' for the
// given domain, ordered by inbound edge count descending, capped at 20.
func (s *Store) GetStandingNodes(domain string) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	rows, err := s.db.Query(`
		SELECT n.id, n.label, n.description, n.why_matters, n.domain,
		       n.created_at, n.updated_at, n.occurred_at, n.archived_at,
		       n.tags, n.node_kind,
		       COUNT(e.id) AS inbound_count
		FROM nodes n
		LEFT JOIN edges e ON e.to_node = n.id
		WHERE n.domain = ? AND n.archived_at IS NULL AND n.node_kind = 'standing'
		GROUP BY n.id
		ORDER BY inbound_count DESC
		LIMIT 20`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows, func(r *sql.Rows) (Node, error) {
		var n Node
		var oa, aa sql.NullTime
		var inboundCount int64
		if err := r.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind, &inboundCount); err != nil {
			return Node{}, err
		}
		n.OccurredAt = nullTimeToPtr(oa)
		n.ArchivedAt = nullTimeToPtr(aa)
		return n, nil
	})
}
