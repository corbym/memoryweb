package db

import (
	"fmt"
	"time"
)

type DomainAlias struct {
	Alias     string    `json:"alias"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
}

// ── domain aliases ────────────────────────────────────────────────────────────

// ResolveAlias returns the canonical domain for name, or name itself if no
// alias is registered.
func (s *Store) ResolveAlias(name string) string {
	var canonical string
	err := s.db.QueryRow(`SELECT domain FROM domain_aliases WHERE alias = ?`, name).Scan(&canonical)
	if err != nil {
		return name
	}
	return canonical
}

// AddAlias registers alias as an alternative name for domain.
func (s *Store) AddAlias(alias, domain string) error {
	var liveCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, alias,
	).Scan(&liveCount); err != nil {
		return err
	}
	if liveCount > 0 {
		return fmt.Errorf("cannot register alias %q: %d live node(s) already filed under that domain name — revise their domain to %q first", alias, liveCount, domain)
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO domain_aliases (alias, domain, created_at) VALUES (?, ?, ?)`,
		alias, domain, time.Now().UTC(),
	)
	return err
}

// ListAliases returns all registered domain aliases.
func (s *Store) ListAliases() ([]DomainAlias, error) {
	rows, err := s.db.Query(`SELECT alias, domain, created_at FROM domain_aliases ORDER BY alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainAlias
	for rows.Next() {
		var a DomainAlias
		rows.Scan(&a.Alias, &a.Domain, &a.CreatedAt)
		out = append(out, a)
	}
	return out, nil
}

// RemoveAlias deletes an alias. Returns an error if the alias does not exist.
func (s *Store) RemoveAlias(alias string) error {
	res, err := s.db.Exec(`DELETE FROM domain_aliases WHERE alias = ?`, alias)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("alias not found: %s", alias)
	}
	return nil
}

// ── list domains ──────────────────────────────────────────────────────────────

// ListDomains returns all distinct domains that have at least one live node,
// sorted alphabetically.
// RenameDomainResult holds the output of RenameDomain.
type RenameDomainResult struct {
	NodesRenamed int
	OldDomain    string
	NewDomain    string
}

// RenameDomain renames all live nodes in oldDomain to newDomain, then inserts
// a domain alias from oldDomain → newDomain so cached references continue to
// resolve. Both the UPDATE and alias INSERT are performed in a single
// transaction.
//
// Returns an error if:
//   - oldDomain has no live nodes (not found)
//   - newDomain already has live nodes (caller should use MergeDomains instead)
func (s *Store) RenameDomain(oldDomain, newDomain string) (*RenameDomainResult, error) {
	var oldCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, oldDomain,
	).Scan(&oldCount); err != nil {
		return nil, err
	}
	if oldCount == 0 {
		return nil, fmt.Errorf("domain %q has no live nodes", oldDomain)
	}

	var newCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, newDomain,
	).Scan(&newCount); err != nil {
		return nil, err
	}
	if newCount > 0 {
		return nil, fmt.Errorf("domain %q already has live nodes — use merge_domains instead", newDomain)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE nodes SET domain = ? WHERE domain = ?`, newDomain, oldDomain)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO domain_aliases (alias, domain, created_at) VALUES (?, ?, ?)`,
		oldDomain, newDomain, time.Now().UTC(),
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RenameDomainResult{NodesRenamed: int(n), OldDomain: oldDomain, NewDomain: newDomain}, nil
}

// MergeDomainsResult holds the output of MergeDomains.
type MergeDomainsResult struct {
	NodesMoved      int
	SourceDomain    string
	TargetDomain    string
	LabelCollisions []string
}

// MergeDomains moves all live nodes from sourceDomain into targetDomain, then
// inserts a domain alias from sourceDomain → targetDomain. Both the UPDATE and
// alias INSERT are performed in a single transaction.
//
// When dryRun is true, no writes are performed; the result describes what
// would happen.
//
// Returns an error if:
//   - sourceDomain has no live nodes (not found)
//   - targetDomain has no live nodes (caller should use RenameDomain instead)
func (s *Store) MergeDomains(sourceDomain, targetDomain string, dryRun bool) (*MergeDomainsResult, error) {
	var srcCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, sourceDomain,
	).Scan(&srcCount); err != nil {
		return nil, err
	}
	if srcCount == 0 {
		return nil, fmt.Errorf("source domain %q has no live nodes", sourceDomain)
	}

	var tgtCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, targetDomain,
	).Scan(&tgtCount); err != nil {
		return nil, err
	}
	if tgtCount == 0 {
		return nil, fmt.Errorf("target domain %q has no live nodes — use rename_domain instead", targetDomain)
	}

	// Detect label collisions before any write.
	colRows, err := s.db.Query(`
		SELECT s.label FROM nodes s
		JOIN nodes t ON LOWER(s.label) = LOWER(t.label)
		WHERE s.domain = ? AND s.archived_at IS NULL
		  AND t.domain = ? AND t.archived_at IS NULL
	`, sourceDomain, targetDomain)
	if err != nil {
		return nil, err
	}
	var collisions []string
	for colRows.Next() {
		var label string
		if err := colRows.Scan(&label); err != nil {
			colRows.Close()
			return nil, err
		}
		collisions = append(collisions, label)
	}
	colRows.Close()
	if err := colRows.Err(); err != nil {
		return nil, err
	}

	if dryRun {
		return &MergeDomainsResult{
			NodesMoved:      srcCount,
			SourceDomain:    sourceDomain,
			TargetDomain:    targetDomain,
			LabelCollisions: collisions,
		}, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE nodes SET domain = ? WHERE domain = ?`, targetDomain, sourceDomain)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO domain_aliases (alias, domain, created_at) VALUES (?, ?, ?)`,
		sourceDomain, targetDomain, time.Now().UTC(),
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MergeDomainsResult{
		NodesMoved:      int(n),
		SourceDomain:    sourceDomain,
		TargetDomain:    targetDomain,
		LabelCollisions: collisions,
	}, nil
}

func (s *Store) DomainExists(domain string) (bool, error) {
	domain = s.ResolveAlias(domain)
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, domain).Scan(&n)
	return n > 0, err
}

func (s *Store) ListDomains() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT domain FROM nodes WHERE archived_at IS NULL ORDER BY domain ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
}
