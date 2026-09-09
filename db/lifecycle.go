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
func (st *Store) LifecycleStates(nodeIDs []string) (map[string]LifecycleState, error) {
	out := make(map[string]LifecycleState)
	if len(nodeIDs) == 0 {
		return out, nil
	}

	ph, args := inClause(nodeIDs)
	rows, err := st.db.Query(
		`SELECT id, node_kind, label FROM nodes WHERE archived_at IS NULL AND id IN (`+ph+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	nodes, err := scanRows(rows, func(r *sql.Rows) (lifecycleNode, error) {
		var node lifecycleNode
		err := r.Scan(&node.id, &node.kind, &node.label)
		return node, err
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
	for _, node := range nodes {
		kindByID[node.id] = node.kind
		labelByID[node.id] = node.label
		liveIDs[node.id] = true
	}

	edgeRows, err := st.db.Query(
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
		var edge edgeLite
		err := r.Scan(&edge.from, &edge.to, &edge.rel)
		return edge, err
	})
	if err != nil {
		return nil, err
	}

	resolvedPairs := make(map[string]bool, len(edges))
	for _, edge := range edges {
		if resolutionRelationships[edge.rel] {
			var a, b string
			if edge.from < edge.to {
				a, b = edge.from, edge.to
			} else {
				a, b = edge.to, edge.from
			}
			resolvedPairs[a+"\x00"+b] = true
		}
	}
	hasResolutionBetween := func(a, b string) bool {
		if a > b {
			a, b = b, a
		}
		return resolvedPairs[a+"\x00"+b]
	}

	contested := make(map[string]bool)
	for _, edge := range edges {
		if edge.rel != "contradicts" {
			continue
		}
		if hasResolutionBetween(edge.from, edge.to) {
			continue
		}
		if liveIDs[edge.from] {
			contested[edge.from] = true
		}
		if liveIDs[edge.to] {
			contested[edge.to] = true
		}
	}

	superseded := make(map[string]bool)
	for _, edge := range edges {
		if edge.rel == "supersedes" && liveIDs[edge.to] {
			superseded[edge.to] = true
		}
	}

	resolved := make(map[string]bool)
	for id := range liveIDs {
		if contested[id] {
			continue
		}
		if kindByID[id] == "issue" {
			for _, edge := range edges {
				if edge.from == id && resolutionRelationships[edge.rel] {
					resolved[id] = true
					break
				}
			}
		}
		if resolved[id] {
			continue
		}
		for _, edge := range edges {
			if edge.rel != "contradicts" {
				continue
			}
			partner := ""
			if edge.from == id {
				partner = edge.to
			} else if edge.to == id {
				partner = edge.from
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
