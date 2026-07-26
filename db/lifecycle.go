package db

import (
	"database/sql"
	"strings"
)

// LifecycleState is a derived graph signal for lean/digest rendering — not stored.
type LifecycleState string

const (
	LifecycleContested  LifecycleState = "contested"
	LifecycleResolved   LifecycleState = "resolved"
	LifecycleSuperseded LifecycleState = "superseded"
)

var resolutionRelationships = map[string]bool{
	"resolved":    true,
	"resolved_by": true,
	"supersedes":  true,
}

type lifecycleNode struct {
	id, kind, label string
}

// LifecycleStates returns derived lifecycle markers for live nodes. Missing keys
// mean no marker. Priority: contested > superseded > resolved.
func (s *Store) LifecycleStates(nodeIDs []string) (map[string]LifecycleState, error) {
	out := make(map[string]LifecycleState)
	if len(nodeIDs) == 0 {
		return out, nil
	}

	ph, args := inClause(nodeIDs)
	rows, err := s.db.Query(
		`SELECT id, node_kind, label FROM nodes WHERE archived_at IS NULL AND id IN (`+ph+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	nodes, err := scanRows(rows, func(r *sql.Rows) (lifecycleNode, error) {
		var n lifecycleNode
		err := r.Scan(&n.id, &n.kind, &n.label)
		return n, err
	})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return out, nil
	}

	kindByID := make(map[string]string, len(nodes))
	labelByID := make(map[string]string, len(nodes))
	liveIDs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		kindByID[n.id] = n.kind
		labelByID[n.id] = n.label
		liveIDs[n.id] = true
	}

	edgeRows, err := s.db.Query(
		`SELECT e.from_node, e.to_node, e.relationship FROM edges e
		 INNER JOIN nodes nf ON nf.id = e.from_node AND nf.archived_at IS NULL
		 INNER JOIN nodes nt ON nt.id = e.to_node AND nt.archived_at IS NULL
		 WHERE e.from_node IN (`+ph+`) OR e.to_node IN (`+ph+`)`,
		append(args, args...)...,
	)
	if err != nil {
		return nil, err
	}
	type edgeLite struct{ from, to, rel string }
	edges, err := scanRows(edgeRows, func(r *sql.Rows) (edgeLite, error) {
		var e edgeLite
		err := r.Scan(&e.from, &e.to, &e.rel)
		return e, err
	})
	if err != nil {
		return nil, err
	}

	hasResolutionBetween := func(a, b string) bool {
		for _, e := range edges {
			if !resolutionRelationships[e.rel] {
				continue
			}
			if (e.from == a && e.to == b) || (e.from == b && e.to == a) {
				return true
			}
		}
		return false
	}

	contested := make(map[string]bool)
	for _, e := range edges {
		if e.rel != "contradicts" {
			continue
		}
		if hasResolutionBetween(e.from, e.to) {
			continue
		}
		if liveIDs[e.from] {
			contested[e.from] = true
		}
		if liveIDs[e.to] {
			contested[e.to] = true
		}
	}

	superseded := make(map[string]bool)
	for _, e := range edges {
		if e.rel == "supersedes" && liveIDs[e.to] {
			superseded[e.to] = true
		}
	}

	resolved := make(map[string]bool)
	for id := range liveIDs {
		if contested[id] {
			continue
		}
		if kindByID[id] == "issue" {
			for _, e := range edges {
				if e.from == id && resolutionRelationships[e.rel] {
					resolved[id] = true
					break
				}
			}
		}
		if resolved[id] {
			continue
		}
		for _, e := range edges {
			if e.rel != "contradicts" {
				continue
			}
			partner := ""
			if e.from == id {
				partner = e.to
			} else if e.to == id {
				partner = e.from
			} else {
				continue
			}
			if hasResolutionBetween(id, partner) {
				resolved[id] = true
				break
			}
		}
		if resolved[id] {
			continue
		}
		if legacyResolvedLabel(labelByID[id]) {
			resolved[id] = true
		}
	}

	for id := range liveIDs {
		switch {
		case contested[id]:
			out[id] = LifecycleContested
		case superseded[id]:
			out[id] = LifecycleSuperseded
		case resolved[id]:
			out[id] = LifecycleResolved
		}
	}
	return out, nil
}

func legacyResolvedLabel(label string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(label)), "RESOLVED")
}
