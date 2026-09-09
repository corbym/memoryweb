package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var nonAlpha = regexp.MustCompile(`[^a-z0-9]+`)

// escapeLike escapes the SQL LIKE metacharacters % and _ in s so that they
// are treated as literal characters. The result should be used with
// ESCAPE '\' in the SQL clause.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func slug(input string) string {
	input = strings.ToLower(input)
	input = nonAlpha.ReplaceAllString(input, "-")
	input = strings.Trim(input, "-")
	if len(input) > 32 {
		input = input[:32]
	}
	return input
}

func shortID() string {
	buf := make([]byte, 4)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// tagFilter builds a single WHERE condition (and appends the corresponding args)
// that matches any node whose tag column contains at least one of the supplied tags
// as a whole word. col is the SQL column reference (e.g. "tags" or "n.tags").
// If tags is empty, nothing is appended and conds/args are returned unchanged.
func tagFilter(col string, tags []string, conds []string, args []interface{}) ([]string, []interface{}) {
	if len(tags) == 0 {
		return conds, args
	}
	var clauses []string
	for _, tag := range tags {
		low := strings.ToLower(tag)
		escaped := escapeLike(low)
		clauses = append(clauses,
			"(LOWER("+col+") = ? OR LOWER("+col+") LIKE ? || ' %' ESCAPE '\\' OR LOWER("+col+") LIKE '% ' || ? ESCAPE '\\' OR LOWER("+col+") LIKE '% ' || ? || ' %' ESCAPE '\\')")
		args = append(args, low, escaped, escaped, escaped)
	}
	conds = append(conds, "("+strings.Join(clauses, " OR ")+")")
	return conds, args
}

// nodeMatchesTags reports whether the space-separated tagString contains at least
// one of the supplied tags as a whole word (case-insensitive, matching tagFilter semantics).
func nodeMatchesTags(tagString string, tags []string) bool {
	if tagString == "" || len(tags) == 0 {
		return false
	}
	parts := strings.Fields(tagString)
	for _, want := range tags {
		for _, have := range parts {
			if strings.EqualFold(have, want) {
				return true
			}
		}
	}
	return false
}

// nodeKindFilter appends `col IN (?,?,...)` when kinds is non-empty.
func nodeKindFilter(col string, kinds []string, conds []string, args []interface{}) ([]string, []interface{}) {
	if len(kinds) == 0 {
		return conds, args
	}
	ph, phArgs := inClause(kinds)
	conds = append(conds, col+" IN ("+ph+")")
	args = append(args, phArgs...)
	return conds, args
}

// nodeMatchesNodeKind reports whether nodeKind is in kinds (OR semantics).
// Empty kinds matches everything.
func nodeMatchesNodeKind(nodeKind string, kinds []string) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if nodeKind == k {
			return true
		}
	}
	return false
}

// nullTimeToPtr returns a pointer to nt.Time when valid, nil otherwise.
func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if nt.Valid {
		return &nt.Time
	}
	return nil
}

// scanRows iterates rows, calling scan for each row and accumulating results.
// Caller is responsible for closing rows.
func scanRows[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// inClause returns a "?,?,?" placeholder string and the items as []any,
// ready for use in a SQL IN clause. Returns ("", nil) for an empty slice.
func inClause[T any](items []T) (string, []any) {
	if len(items) == 0 {
		return "", nil
	}
	args := make([]any, len(items))
	for i, v := range items {
		args[i] = v
	}
	placeholders := strings.Repeat("?,", len(items))
	return placeholders[:len(placeholders)-1], args
}

// filter returns a new slice containing only the items for which keep returns true.
func filter[T any](items []T, keep func(T) bool) []T {
	var out []T
	for _, v := range items {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// mapSlice transforms a []T into a []U by applying f to each element.
func mapSlice[T, U any](items []T, f func(T) U) []U {
	out := make([]U, len(items))
	for i, v := range items {
		out[i] = f(v)
	}
	return out
}

// applyStringField appends a SQL SET clause and optional audit change entry for
// an optional string field. If newVal is nil, nothing is appended.
// col is the SQL column name; fieldName is the label used in audit messages.
func applyStringField(newVal *string, current, col, fieldName string, sets, changes *[]string, args *[]interface{}) {
	if newVal == nil {
		return
	}
	*sets = append(*sets, col+" = ?")
	*args = append(*args, *newVal)
	if *newVal != current {
		*changes = append(*changes, fmt.Sprintf("%s (was %q)", fieldName, current))
	}
}

// scanNodeRow scans a single row from a query that selects the standard 11 node
// columns in the order: id, label, description, why_matters, domain,
// created_at, updated_at, occurred_at, archived_at, tags, node_kind.
func scanNodeRow(rows *sql.Rows) (Node, error) {
	var n Node
	var occurredAt, archivedAt sql.NullTime
	if err := rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
		&n.CreatedAt, &n.UpdatedAt, &occurredAt, &archivedAt, &n.Tags, &n.NodeKind); err != nil {
		return Node{}, err
	}
	n.OccurredAt = nullTimeToPtr(occurredAt)
	n.ArchivedAt = nullTimeToPtr(archivedAt)
	return n, nil
}

// scanNodeRows reads all node rows from rows into a slice.
// Caller is responsible for closing rows.
func scanNodeRows(rows *sql.Rows) ([]Node, error) {
	return scanRows(rows, scanNodeRow)
}

// scanNode scans a single row from a query that SELECTs the standard 11 node
// columns in the order: id, label, description, why_matters, tags, domain,
// created_at, updated_at, occurred_at, archived_at, node_kind.
func scanNode(rows *sql.Rows) (Node, error) {
	var node Node
	var description, whyMatters, tags sql.NullString
	var occurredAt, archivedAt sql.NullTime
	var nodeKind string
	if err := rows.Scan(
		&node.ID, &node.Label, &description, &whyMatters, &tags, &node.Domain,
		&node.CreatedAt, &node.UpdatedAt, &occurredAt, &archivedAt, &nodeKind,
	); err != nil {
		return node, err
	}
	node.Description = description.String
	node.WhyMatters = whyMatters.String
	node.Tags = tags.String
	node.OccurredAt = nullTimeToPtr(occurredAt)
	node.ArchivedAt = nullTimeToPtr(archivedAt)
	node.NodeKind = nodeKind
	return node, nil
}
