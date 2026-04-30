// Package stats records tool usage metrics for a memoryweb MCP session.
// It writes two separate outputs:
//   - a human-readable log (MEMORYWEB_STATS_FILE)
//   - a machine-readable JSONL file, one JSON object per session (MEMORYWEB_STATS_JSON_FILE)
//
// Either path may be empty to disable that output.
package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type toolKind int

const (
	kindRetrieval toolKind = iota
	kindWrite
	kindMaint
)

var toolKinds = map[string]toolKind{
	"search": kindRetrieval, "recall": kindRetrieval, "recent": kindRetrieval,
	"orient": kindRetrieval, "why_connected": kindRetrieval, "trace": kindRetrieval,
	"history": kindRetrieval, "disconnected": kindRetrieval,
	"remember": kindWrite, "remember_all": kindWrite,
	"connect": kindWrite, "connect_all": kindWrite,
	"revise": kindWrite, "revise_all": kindWrite,
	"forget": kindWrite, "restore": kindWrite, "merge": kindWrite,
	"suggest_connections": kindMaint, "list_domains": kindMaint,
	"alias_domain": kindMaint, "list_aliases": kindMaint,
	"remove_alias": kindMaint, "resolve_domain": kindMaint,
	"forgotten": kindMaint, "whats_stale": kindMaint,
	"disconnect": kindMaint, "visualise": kindMaint,
}

type callRec struct {
	tool       string
	kind       toolKind
	isError    bool
	ts         time.Time
	nodesFiled int
	transient  int
	edgesFiled int
	domain     string
}

type sessionData struct {
	StartTS    time.Time `json:"start_ts"`
	WKD        float64   `json:"wkd"`
	Type       string    `json:"type"`
	NodesFiled int       `json:"nodes"`
	EdgesFiled int       `json:"edges"`
	Orphans    int       `json:"orphans"`
	Transient  int       `json:"transient"`
	Ratio      float64   `json:"ratio"`
	Burst      bool      `json:"burst"`
	Client     string    `json:"client,omitempty"`
}

// Recorder observes tool calls and writes session summaries.
// All exported methods are safe for concurrent use.
type Recorder struct {
	mu        sync.Mutex
	humanPath string // human-readable log; may be empty
	jsonPath  string // JSONL machine-readable log; may be empty
	client    string // value of MEMORYWEB_CLIENT env var; may be empty
	start     time.Time
	calls     []callRec
}

// New returns a Recorder. humanPath receives the human-readable summary;
// jsonPath receives one JSON object per session (JSONL). Either may be empty.
// The MEMORYWEB_CLIENT env var, if set, is stamped into each session record.
func New(humanPath, jsonPath string) *Recorder {
	return &Recorder{
		humanPath: humanPath,
		jsonPath:  jsonPath,
		client:    os.Getenv("MEMORYWEB_CLIENT"),
		start:     time.Now().UTC(),
	}
}

// Record observes one tool call. argsRaw is the raw JSON arguments;
// resultText is the first content block text from the result.
func (r *Recorder) Record(tool string, argsRaw json.RawMessage, resultText string, isError bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cr := callRec{tool: tool, kind: toolKinds[tool], isError: isError, ts: time.Now().UTC()}
	if !isError {
		switch tool {
		case "remember":
			cr.nodesFiled = 1
			cr.transient = parseTransientArg(argsRaw)
			cr.domain = parseDomainArg(argsRaw)
		case "remember_all":
			cr.nodesFiled, cr.transient = parseRememberAllArgs(argsRaw)
		case "connect":
			cr.edgesFiled = 1
		case "connect_all":
			cr.edgesFiled = parseEdgesCreated(resultText)
		case "merge":
			cr.edgesFiled = 1
		}
	}
	r.calls = append(r.calls, cr)
}

// Flush computes the session summary, appends it to the log file, and returns
// the summary text. Safe to call more than once.
func (r *Recorder) Flush() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flush()
}

func (r *Recorder) flush() (string, error) {
	if len(r.calls) == 0 {
		return "", nil
	}
	sess := r.computeSession()
	prior := r.readPriorSessions()
	summary := r.formatSummary(sess, prior)

	var firstErr error

	// Write human-readable log.
	if r.humanPath != "" {
		f, err := os.OpenFile(r.humanPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			firstErr = err
		} else {
			_, err = fmt.Fprintln(f, summary)
			f.Close()
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	// Write JSON object to JSONL file.
	if r.jsonPath != "" {
		dataJSON, _ := json.Marshal(sess.data)
		f, err := os.OpenFile(r.jsonPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			_, err = fmt.Fprintf(f, "%s\n", dataJSON)
			f.Close()
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	return summary, firstErr
}

// ── computation ──────────────────────────────────────────────────────────────

type computedSession struct {
	data       sessionData
	duration   time.Duration
	byTool     map[string]int
	errors     int
	totalCalls int
	grade      string
	domains    map[string]int
}

func (r *Recorder) computeSession() computedSession {
	var nodesFiled, edgesFiled, transientFiled, errors int
	byTool := make(map[string]int)
	domains := make(map[string]int)
	var retrievalTotal, retrievalToWrite int

	for i, c := range r.calls {
		byTool[c.tool]++
		if c.isError {
			errors++
		}
		nodesFiled += c.nodesFiled
		edgesFiled += c.edgesFiled
		transientFiled += c.transient
		if c.domain != "" {
			domains[c.domain]++
		}
		if c.kind == kindRetrieval && !c.isError {
			retrievalTotal++
			for j := i + 1; j <= i+3 && j < len(r.calls); j++ {
				if r.calls[j].kind == kindWrite && !r.calls[j].isError {
					retrievalToWrite++
					break
				}
			}
		}
	}

	orphans := imax(0, nodesFiled-edgesFiled)
	connected := imin(nodesFiled, edgesFiled)
	ratio := 0.0
	if retrievalTotal > 0 {
		ratio = float64(retrievalToWrite) / float64(retrievalTotal)
	}

	// WKD: +2 per connected node, +1.5 per edge, -1 per orphan,
	//      -0.5 per transient node, +10x retrieval-action ratio.
	wkd := float64(connected)*2.0 +
		float64(edgesFiled)*1.5 -
		float64(orphans)*1.0 -
		float64(transientFiled)*0.5 +
		ratio*10.0
	wkd = math.Round(wkd*10) / 10

	sessType := "filing"
	if nodesFiled == 0 && edgesFiled == 0 {
		if retrievalTotal >= 3 || len(r.calls) >= 3 {
			sessType = "retrieval"
		} else {
			sessType = "minimal"
		}
	}

	grade := "retrieval-only"
	if sessType == "filing" {
		grade = WKDGrade(wkd)
	}

	return computedSession{
		data: sessionData{
			StartTS: r.start, WKD: wkd, Type: sessType,
			NodesFiled: nodesFiled, EdgesFiled: edgesFiled,
			Orphans: orphans, Transient: transientFiled,
			Ratio: math.Round(ratio*100) / 100,
			Burst:  nodesFiled > 15,
			Client: r.client,
		},
		duration:   time.Since(r.start),
		byTool:     byTool,
		errors:     errors,
		totalCalls: len(r.calls),
		grade:      grade,
		domains:    domains,
	}
}

// WKDGrade converts a WKD score to a letter grade. Exported for testing.
func WKDGrade(wkd float64) string {
	switch {
	case wkd >= 25:
		return "A"
	case wkd >= 15:
		return "B+"
	case wkd >= 8:
		return "B"
	case wkd >= 2:
		return "C"
	case wkd >= 0:
		return "D"
	default:
		return "D-"
	}
}

// ── prior session reading ─────────────────────────────────────────────────────

// readPriorSessions reads historical session data.
// It prefers the JSONL file (one JSON object per line); if that is not
// configured it falls back to scanning the human log for legacy
// <!-- data: … --> lines so that existing log files keep working.
func (r *Recorder) readPriorSessions() []sessionData {
	if r.jsonPath != "" {
		return readJSONL(r.jsonPath)
	}
	return readLegacyDataLines(r.humanPath)
}

func readJSONL(path string) []sessionData {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []sessionData
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var sd sessionData
		if json.Unmarshal([]byte(line), &sd) == nil {
			out = append(out, sd)
		}
	}
	return out
}

func readLegacyDataLines(path string) []sessionData {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []sessionData
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "<!-- data: ") {
			continue
		}
		js := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- data: "), " -->")
		var sd sessionData
		if json.Unmarshal([]byte(js), &sd) == nil {
			out = append(out, sd)
		}
	}
	return out
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := make([]float64, len(vals))
	copy(s, vals)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[n/2]
}

// ── formatting ────────────────────────────────────────────────────────────────

func (r *Recorder) formatSummary(sess computedSession, prior []sessionData) string {
	var sb strings.Builder
	now := time.Now().UTC()

	sb.WriteString(fmt.Sprintf("\n=== memoryweb session -- %s ===\n", now.Format("2006-01-02 15:04 UTC")))
	if sess.data.Client != "" {
		sb.WriteString(fmt.Sprintf("Client        %s\n", sess.data.Client))
	}

	mins := int(sess.duration.Minutes())
	if mins < 1 {
		mins = 1
	}
	sb.WriteString(fmt.Sprintf("Active %d min | %d tool calls", mins, sess.totalCalls))
	if len(sess.domains) > 0 {
		var dp []string
		for d, n := range sess.domains {
			dp = append(dp, fmt.Sprintf("%s (%d)", d, n))
		}
		sort.Strings(dp)
		sb.WriteString(" across " + strings.Join(dp, ", "))
	}
	sb.WriteString("\n")

	type toolCount struct {
		name string
		n    int
	}
	var tc []toolCount
	for t, n := range sess.byTool {
		tc = append(tc, toolCount{t, n})
	}
	sort.Slice(tc, func(i, j int) bool { return tc[i].n > tc[j].n })
	if len(tc) > 5 {
		tc = tc[:5]
	}
	parts := make([]string, len(tc))
	for i, t := range tc {
		parts[i] = fmt.Sprintf("%s x%d", t.name, t.n)
	}
	sb.WriteString(fmt.Sprintf("  Most used    %s\n", strings.Join(parts, ", ")))

	if sess.errors > 0 {
		pct := int(float64(sess.errors) / float64(sess.totalCalls) * 100)
		sb.WriteString(fmt.Sprintf("  Errors       %d (%d%%)\n", sess.errors, pct))
	}

	switch sess.data.Type {
	case "retrieval":
		sb.WriteString("  Session type  high-value retrieval - agent used memoryweb for context without filing\n")
		if sess.data.Ratio > 0 {
			sb.WriteString(fmt.Sprintf("  Retrieval     %.0f%% of lookups influenced downstream action\n", sess.data.Ratio*100))
		}
	case "filing":
		if sess.data.Ratio > 0 {
			quality := "good - checks before filing"
			if sess.data.Ratio < 0.4 {
				quality = "low - filing without retrieving first"
			}
			sb.WriteString(fmt.Sprintf("  Retrieval     %.0f%% retrieval->action ratio (%s)\n", sess.data.Ratio*100, quality))
		}
		filed := fmt.Sprintf("%d nodes, %d edges", sess.data.NodesFiled, sess.data.EdgesFiled)
		if sess.data.Transient > 0 {
			filed += fmt.Sprintf(", %d transient", sess.data.Transient)
		}
		sb.WriteString(fmt.Sprintf("  Filed         %s\n", filed))
		if sess.data.Orphans > 0 {
			sb.WriteString(fmt.Sprintf("  Orphans       %d node(s) filed but never connected - consider linking or archiving\n", sess.data.Orphans))
		}
		if sess.data.Burst {
			sb.WriteString("  Note          burst session (>15 nodes) - excluded from trend median\n")
		}
		sb.WriteString(fmt.Sprintf("  Usefulness    %-3s  WKD %.1f\n", sess.grade, sess.data.WKD))
	case "minimal":
		sb.WriteString("  Session type  minimal - few calls, no substantive activity\n")
	}

	if len(prior) > 0 {
		now30 := now.AddDate(0, 0, -30)
		now7 := now.AddDate(0, 0, -7)
		var wkd30, wkd7 []float64
		var total30, ret30 int

		for _, p := range prior {
			if p.StartTS.Before(now30) {
				continue
			}
			total30++
			if p.Type == "retrieval" {
				ret30++
				continue
			}
			if !p.Burst {
				wkd30 = append(wkd30, p.WKD)
				if p.StartTS.After(now7) {
					wkd7 = append(wkd7, p.WKD)
				}
			}
		}
		if sess.data.Type == "filing" && !sess.data.Burst {
			wkd30 = append(wkd30, sess.data.WKD)
			wkd7 = append(wkd7, sess.data.WKD)
		}

		sb.WriteString("\n-- 30-day trend --\n")
		sb.WriteString(fmt.Sprintf("  Sessions      %d total (%d retrieval-only)\n", total30+1, ret30))
		if len(wkd30) > 0 {
			m30 := median(wkd30)
			m7 := median(wkd7)
			trend := "-> stable"
			if len(wkd7) > 0 {
				if m7 > m30*1.15 {
					trend = "^ improving"
				} else if m7 < m30*0.85 {
					trend = "v declining"
				}
			}
			sb.WriteString(fmt.Sprintf("  WKD median    %.1f (30d)  %.1f (7d)  %s\n", m30, m7, trend))
		}
		if ret30 > 0 {
			pct := int(float64(ret30) / float64(total30+1) * 100)
			sb.WriteString(fmt.Sprintf("  Retrieval pct %d%% of sessions were retrieval-only\n", pct))
		}
	}

	sb.WriteString("=== end ===\n")
	return sb.String()
}

// ── argument/result parsers ───────────────────────────────────────────────────

func parseTransientArg(args json.RawMessage) int {
	var a struct {
		Transient bool `json:"transient"`
	}
	if json.Unmarshal(args, &a) == nil && a.Transient {
		return 1
	}
	return 0
}

func parseDomainArg(args json.RawMessage) string {
	var a struct {
		Domain string `json:"domain"`
	}
	json.Unmarshal(args, &a)
	return a.Domain
}

func parseRememberAllArgs(args json.RawMessage) (nodes, transient int) {
	var a struct {
		Nodes []struct {
			Transient bool `json:"transient"`
		} `json:"nodes"`
	}
	if json.Unmarshal(args, &a) == nil {
		nodes = len(a.Nodes)
		for _, n := range a.Nodes {
			if n.Transient {
				transient++
			}
		}
	}
	return
}

func parseEdgesCreated(resultText string) int {
	var r struct {
		EdgesCreated int `json:"edges_created"`
	}
	if json.Unmarshal([]byte(resultText), &r) == nil && r.EdgesCreated > 0 {
		return r.EdgesCreated
	}
	return 1
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}






