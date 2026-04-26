// cmd/gitimport imports git log entries into memoryweb as nodes.
// Each meaningful commit becomes a node filed under the given domain,
// with occurred_at set to the commit date.  Re-runs are idempotent:
// commits whose hash already appears in a node description are skipped.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/corbym/memoryweb/db"
)

// ── noise filter ─────────────────────────────────────────────────────────────

// noiseWords cause a short subject to be skipped.
var noiseWords = []string{
	"wip", "fix", "update", "merge", "bump",
	"typo", "cleanup", "refactor", "misc", "temp", "test",
}

// noisePrefixRE matches subjects that are always noise regardless of length.
var noisePrefixRE = regexp.MustCompile(`(?i)^(` +
	`merge (branch|pull request|remote-tracking branch)` +
	`|revert "` +
	`|merged in renovate/` +
	`|merged in renovate` +
	`|update dependency` +
	`|update flyway` +
	`|bump ` +
	`)`)

// noisePatternRE matches subjects by pattern that are always noise.
var noisePatternRE = regexp.MustCompile(`(?i)^update .+ to v[0-9]`)

// ticketRE detects a Jira-style or GitHub-style ticket ID anywhere in the subject.
var ticketRE = regexp.MustCompile(`(?i)[A-Z]+-\d+|#\d+`)

func wordCount(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			n++
		}
	}
	return n
}

func isNoise(subject string) bool {
	// Always-noise patterns
	if noisePrefixRE.MatchString(subject) {
		return true
	}
	if noisePatternRE.MatchString(subject) {
		return true
	}

	wc := wordCount(subject)
	lower := strings.ToLower(subject)

	// Short AND contains a noise word → skip
	if wc <= 10 {
		for _, w := range noiseWords {
			// whole-word match
			re := regexp.MustCompile(`\b` + w + `\b`)
			if re.MatchString(lower) {
				return true
			}
		}
	}

	// No ticket ID AND very short → skip
	if !ticketRE.MatchString(subject) && wc < 5 {
		return true
	}

	return false
}

// ── git log parsing ───────────────────────────────────────────────────────────

type commit struct {
	hash       string
	occurredAt time.Time
	subject    string
	body       string
}

// runGitLog runs git log on repoPath and returns parsed commits.
func runGitLog(repoPath, since string) ([]commit, error) {
	args := []string{
		"-C", repoPath,
		"log",
		"--format=%H|%ad|%s|%b",
		"--date=iso",
		"-z", // NUL-separate records so multi-line bodies don't break parsing
	}
	if since != "" {
		args = append(args, "--since="+since)
	}

	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []commit
	// Records are NUL-separated; each record: HASH|DATE|SUBJECT|BODY\n
	records := strings.Split(string(out), "\x00")
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		// Split on first three pipes only — body may contain pipes
		parts := strings.SplitN(rec, "|", 4)
		if len(parts) < 3 {
			continue
		}
		hash := strings.TrimSpace(parts[0])
		dateStr := strings.TrimSpace(parts[1])
		subject := strings.TrimSpace(parts[2])
		body := ""
		if len(parts) == 4 {
			body = strings.TrimSpace(parts[3])
		}

		if hash == "" || subject == "" {
			continue
		}

		// git --date=iso produces "2026-04-01 14:32:00 +0100"
		t, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr)
		if err != nil {
			// fall back, keep zero time
			t = time.Time{}
		}

		commits = append(commits, commit{
			hash:       hash,
			occurredAt: t,
			subject:    subject,
			body:       body,
		})
	}
	return commits, nil
}

// ── deduplication check ───────────────────────────────────────────────────────

// hashTag is the string we embed in the description so we can detect duplicates.
func hashTag(hash string) string {
	return "git:" + hash
}

// alreadyImported returns true if a node whose description contains the hash
// tag already exists in the store.
func alreadyImported(store *db.Store, hash string) (bool, error) {
	result, err := store.SearchNodes(hashTag(hash), "", 1)
	if err != nil {
		return false, err
	}
	tag := hashTag(hash)
	for _, n := range result.Nodes {
		if strings.Contains(n.Description, tag) {
			return true, nil
		}
	}
	return false, nil
}

// ── label truncation ──────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// cut at last space before max
	cut := s[:max]
	if idx := strings.LastIndex(cut, " "); idx > max/2 {
		cut = cut[:idx]
	}
	return cut + "…"
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	repo := flag.String("repo", "", "Path to the git repository (required)")
	domain := flag.String("domain", "", "memoryweb domain to file commits under (required)")
	dbPath := flag.String("db", "", "Path to the memoryweb SQLite database (default: ~/.memoryweb/.memoryweb.db)")
	dryRun := flag.Bool("dry-run", false, "Print what would be filed without writing anything")
	since := flag.String("since", "", "Only import commits after this date, e.g. 2024-01-01")
	flag.Parse()

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "error: --repo is required")
		flag.Usage()
		os.Exit(1)
	}
	if *domain == "" {
		fmt.Fprintln(os.Stderr, "error: --domain is required")
		flag.Usage()
		os.Exit(1)
	}

	// Resolve db path
	resolvedDB := *dbPath
	if resolvedDB == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
			os.Exit(1)
		}
		resolvedDB = filepath.Join(home, ".memoryweb", ".memoryweb.db")
	}

	// Resolve repo path
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: invalid repo path:", err)
		os.Exit(1)
	}

	// Open store (skip in dry-run so we don't create a DB file accidentally)
	var store *db.Store
	if !*dryRun {
		store, err = db.New(resolvedDB)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot open database:", err)
			os.Exit(1)
		}
		defer store.Close()
	}

	commits, err := runGitLog(absRepo, *since)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	var (
		imported  int
		skippedNoise int
		skippedDup   int
	)

	scanner := bufio.NewScanner(os.Stdin)
	_ = scanner

	for _, c := range commits {
		// Quality filter
		if isNoise(c.subject) {
			skippedNoise++
			if *dryRun {
				fmt.Printf("SKIP (noise)  %s  %s\n", c.hash[:8], c.subject)
			}
			continue
		}

		// Deduplication
		if !*dryRun {
			dup, err := alreadyImported(store, c.hash)
			if err != nil {
				fmt.Fprintln(os.Stderr, "warning: dedup check failed:", err)
			}
			if dup {
				skippedDup++
				continue
			}
		}

		label := truncate(c.subject, 60)

		// Build description: body + hash tag on a new line
		var descParts []string
		if c.body != "" {
			descParts = append(descParts, c.body)
		}
		descParts = append(descParts, hashTag(c.hash))
		description := strings.Join(descParts, "\n\n")

		if *dryRun {
			fmt.Printf("IMPORT        %s  [%s]  %s\n",
				c.hash[:8],
				c.occurredAt.Format("2006-01-02"),
				label,
			)
			imported++
			continue
		}

		var oa *time.Time
		if !c.occurredAt.IsZero() {
			t := c.occurredAt.UTC()
			oa = &t
		}

		_, err := store.AddNode(label, description, "", *domain, oa)
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to add node:", err)
			continue
		}
		imported++
		fmt.Printf("imported  %s  %s\n", c.hash[:8], label)
	}

	fmt.Printf("\n%d imported, %d skipped (noise), %d skipped (duplicate)\n",
		imported, skippedNoise, skippedDup)
}

