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

func slug(s string) string {
	s = strings.ToLower(s)
	s = nonAlpha.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

func shortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
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
		clauses = append(clauses,
			"("+col+" = ? OR "+col+" LIKE ? || ' %' OR "+col+" LIKE '% ' || ? OR "+col+" LIKE '% ' || ? || ' %')")
		args = append(args, tag, tag, tag, tag)
	}
	conds = append(conds, "("+strings.Join(clauses, " OR ")+")")
	return conds, args
}

// nodeMatchesTags reports whether the space-separated tagString contains at least
// one of the supplied tags as a whole word (case-sensitive, matching tagFilter semantics).
func nodeMatchesTags(tagString string, tags []string) bool {
	if tagString == "" || len(tags) == 0 {
		return false
	}
	parts := strings.Fields(tagString)
	for _, want := range tags {
		for _, have := range parts {
			if have == want {
				return true
			}
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
	ph := strings.Repeat("?,", len(items))
	return ph[:len(ph)-1], args
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
