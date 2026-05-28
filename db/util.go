package db

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
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
