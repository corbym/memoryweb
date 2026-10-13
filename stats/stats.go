// Package stats records tool usage metrics for a memoryweb MCP session.
// It writes two separate outputs:
//   - a human-readable log (MEMORYWEB_STATS_FILE)
//   - a machine-readable JSONL file, one JSON object per session (MEMORYWEB_STATS_JSON_FILE)
//
// Either path may be empty to disable that output.
//
// Crash safety: after every tool call the current session state is written to
// humanPath+".current.<pid>" / jsonPath+".current.<pid>" (per-process files).
// On clean shutdown those files are merged into the main logs and deleted. If
// the process is killed with SIGKILL the .current.<pid> files survive and are
// recovered into the main log on the next run (after confirming the previous
// process is dead via PID liveness check).
package stats

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	"significance": kindRetrieval,
	"remember":     kindWrite, "remember_all": kindWrite,
	"connect": kindWrite, "connect_all": kindWrite,
	"revise": kindWrite, "revise_all": kindWrite,
	"forget": kindWrite, "restore": kindWrite, "merge": kindWrite,
	"forget_all":          kindWrite,
	"restore_all":         kindWrite,
	"suggest_connections": kindMaint, "list_domains": kindMaint,
	"alias_domain": kindMaint, "list_aliases": kindMaint,
	"remove_alias": kindMaint, "resolve_domain": kindMaint,
	"forgotten": kindMaint, "whats_stale": kindMaint,
	"disconnect": kindMaint, "disconnect_all": kindMaint, "visualise": kindMaint,
	"audit": kindMaint, "rename_domain": kindMaint,
	"alias": kindMaint, "domains": kindMaint,
}

// HasKind reports whether tool has an explicit entry in the WKD kind table.
// Tools absent from the table silently fall through as kindRetrieval via
// zero-value map lookup. Use this in tests to catch that case.
func HasKind(tool string) bool {
	_, ok := toolKinds[tool]
	return ok
}

// stale type indices for byType field.
const (
	staleContradicts = iota
	staleSuperseded
	staleOpenQ
	staleDuplicate
	staleTransient
)

// nodeInfo tracks a single node created during the session.
type nodeInfo struct {
	id        string
	hasInline bool   // got at least one inline edge via related_to
	transient bool   // node_kind == "transient" or transient arg was true
	domain    string // domain the node was filed in
}

type callRec struct {
	tool        string
	kind        toolKind
	isError     bool
	ts          time.Time
	nodes       []nodeInfo  // nodes created by this call (remember / remember_all)
	inlineEdges int         // total inline edges created by this call
	edgePairs   [][2]string // [fromID, toID] pairs for connect calls
	edgesCount  int         // edge count when pairs are unavailable (batch fallback)
	staleTotal  int         // candidates returned by whats_stale / audit(stale)
	staleByType [5]int      // indexed by stale* constants
	dupEdges    [4]int      // dup candidate edge-count buckets: 0, 1-2, 3-5, 6+
}

type sessionData struct {
	SessionID       string    `json:"session_id,omitempty"`
	StartTS         time.Time `json:"start_ts"`
	WKD             float64   `json:"wkd"`
	Type            string    `json:"type"`
	NodesFiled      int       `json:"nodes"`
	EdgesFiled      int       `json:"edges"`                    // total (inline + connect)
	EdgesInline     int       `json:"edges_inline,omitempty"`   // edges from related_to
	EdgesConnect    int       `json:"edges_connect,omitempty"`  // edges from explicit connect
	InlineShare     float64   `json:"inline_share,omitempty"`   // EdgesInline / EdgesFiled
	EdgesPerNode    float64   `json:"edges_per_node,omitempty"` // EdgesFiled / NodesFiled
	Orphans         int       `json:"orphans"`
	Transient       int       `json:"transient"`
	Ratio           float64   `json:"ratio"`
	Burst           bool      `json:"burst"`
	ActiveSecs      int       `json:"active_secs,omitempty"`     // last_call - first_call
	WallClockSecs   int       `json:"wall_clock_secs,omitempty"` // time.Since(start) at flush
	Client          string    `json:"client,omitempty"`
	StaleChecks     int       `json:"stale_checks,omitempty"`
	StaleCandidates int       `json:"stale_candidates,omitempty"`
	DupEdge0        int       `json:"dup_edge_0,omitempty"`   // duplicate candidates with 0 edges
	DupEdge12       int       `json:"dup_edge_1_2,omitempty"` // 1-2 edges
	DupEdge35       int       `json:"dup_edge_3_5,omitempty"` // 3-5 edges
	DupEdge6p       int       `json:"dup_edge_6p,omitempty"`  // 6+ edges
}

// ProcessAliveFn checks whether the process with the given PID is still running.
// It is a package-level variable so tests can replace it with a stub.
var ProcessAliveFn = func(pid int) bool {
	return defaultProcessAlive(pid)
}

// Recorder observes tool calls and writes session summaries.
// All exported methods are safe for concurrent use.
type Recorder struct {
	mu          sync.Mutex
	humanPath   string // human-readable log; may be empty
	jsonPath    string // JSONL machine-readable log; may be empty
	client      string // MEMORYWEB_CLIENT or "unknown"
	sessionID   string // stable ID for this session
	start       time.Time
	firstCallTS *time.Time // timestamp of first tool call in this session
	lastCallTS  *time.Time // timestamp of most-recent tool call
	calls       []callRec
	flushed     bool // true once Flush() has successfully written this session
}

// New returns a Recorder. humanPath receives the human-readable summary;
// jsonPath receives one JSON object per session (JSONL). Either may be empty.
// The MEMORYWEB_CLIENT env var is stamped into each session record; if unset
// it defaults to "unknown". Any orphaned .current.<pid> files from crashed
// processes are recovered into the main logs before returning.
func New(humanPath, jsonPath string) *Recorder {
	client := os.Getenv("MEMORYWEB_CLIENT")
	if client == "" {
		client = "unknown"
	}
	rec := &Recorder{
		humanPath: humanPath,
		jsonPath:  jsonPath,
		client:    client,
		sessionID: newSessionID(),
		start:     time.Now().UTC(),
	}
	rec.recoverCurrent()
	return rec
}

// newSessionID generates a random 8-byte hex session identifier.
func newSessionID() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// SetSessionID overrides the auto-generated session ID.
// Call this when the MCP client provides a stable session identifier.
func (r *Recorder) SetSessionID(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = id
}

// recoverCurrent scans for .current.<pid> files left by crashed processes,
// confirms each owner is dead, and merges their content into the main logs.
// Uses atomic rename as a recovery lock so concurrent processes claim each
// file exactly once.
func (r *Recorder) recoverCurrent() {
	myPID := os.Getpid()
	for _, base := range []string{r.humanPath, r.jsonPath} {
		if base == "" {
			continue
		}
		matches, _ := filepath.Glob(base + ".current.*")
		prefix := base + ".current."
		for _, cur := range matches {
			if !strings.HasPrefix(cur, prefix) {
				continue
			}
			suffix := cur[len(prefix):]
			if !isAllDigits(suffix) {
				continue // skip .current.<pid>.recovering and other non-PID suffixes
			}
			ownerPID, err := strconv.Atoi(suffix)
			if err != nil {
				continue
			}
			if ownerPID == myPID {
				continue // skip our own (shouldn't exist yet, but be defensive)
			}
			if ProcessAliveFn(ownerPID) {
				continue // leave live processes' files alone
			}
			// Claim atomically via rename — first claimer wins.
			claimed := cur + ".recovering"
			if err := os.Rename(cur, claimed); err != nil {
				continue // another process claimed it
			}
			data, err := os.ReadFile(claimed)
			if err != nil || len(data) == 0 {
				os.Remove(claimed) //nolint:errcheck
				continue
			}
			if f, err := os.OpenFile(base, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); err == nil {
				f.Write(data) //nolint:errcheck
				f.Close()
			}
			os.Remove(claimed) //nolint:errcheck
		}
	}
}

// writeCurrent overwrites the per-process .current.<pid> files with the latest
// session state. Called after every tool call so the data survives a SIGKILL.
// Must be called with r.mu held.
func (r *Recorder) writeCurrent() {
	if len(r.calls) == 0 {
		return
	}
	sess := r.computeSession()
	prior := r.readPriorSessions()
	pid := os.Getpid()

	if r.humanPath != "" {
		summary := r.formatSummary(sess, prior)
		cur := fmt.Sprintf("%s.current.%d", r.humanPath, pid)
		os.WriteFile(cur, []byte(summary+"\n"), 0600) //nolint:errcheck
	}
	if r.jsonPath != "" {
		dataJSON, _ := json.Marshal(sess.data)
		cur := fmt.Sprintf("%s.current.%d", r.jsonPath, pid)
		os.WriteFile(cur, append(dataJSON, '\n'), 0600) //nolint:errcheck
	}
}

// Record observes one tool call. argsRaw is the raw JSON arguments;
// resultText is the first content block text from the result.
func (r *Recorder) Record(tool string, argsRaw json.RawMessage, resultText string, isError bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	call := callRec{tool: tool, kind: toolKinds[tool], isError: isError, ts: now}

	if r.firstCallTS == nil {
		r.firstCallTS = &now
	}
	r.lastCallTS = &now

	if !isError {
		switch tool {
		case "remember":
			if isBatchArgs(argsRaw, "items") {
				call.nodes, call.inlineEdges = parseRememberBatch(argsRaw, resultText)
			} else {
				call.nodes, call.inlineEdges = parseRememberSingle(argsRaw, resultText)
			}
		case "remember_all":
			call.nodes, call.inlineEdges = parseRememberAll(argsRaw, resultText)
		case "connect":
			if isBatchArgs(argsRaw, "items") {
				call.edgePairs, call.edgesCount = parseConnectBatch(argsRaw, resultText)
			} else {
				if pair, ok := parseConnectSingle(argsRaw, resultText); ok {
					call.edgePairs = [][2]string{pair}
				}
			}
		case "connect_all":
			call.edgesCount = parseEdgesCreated(resultText)
		case "merge":
			call.edgesCount = 1
		case "whats_stale":
			call.staleTotal, call.staleByType, call.dupEdges = parseWhatsStaleResult(resultText)
		case "audit":
			var a struct {
				Mode string `json:"mode"`
			}
			if json.Unmarshal(argsRaw, &a) == nil && a.Mode == "stale" {
				call.staleTotal, call.staleByType, call.dupEdges = parseWhatsStaleResult(resultText)
			}
		}
	}
	r.calls = append(r.calls, call)
	r.writeCurrent()
}

// Flush computes the session summary, appends it to the log file, and returns
// the summary text. Idempotent: subsequent calls after the first successful
// write return immediately without writing again.
func (r *Recorder) Flush() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flush()
}

// FlushAndReset flushes the current session (if any tool calls were made) and
// then resets the recorder for a new session. Call this when a new MCP client
// session starts (initialize request) to prevent a long-lived process from
// carrying one session's totals into the next.
func (r *Recorder) FlushAndReset() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	summary, err := r.flush()
	r.resetLocked()
	return summary, err
}

// resetLocked resets session state without flushing. Must be called with r.mu held.
func (r *Recorder) resetLocked() {
	r.calls = nil
	r.firstCallTS = nil
	r.lastCallTS = nil
	r.start = time.Now().UTC()
	r.sessionID = newSessionID()
	r.flushed = false
}

func (r *Recorder) flush() (string, error) {
	// Idempotency: skip if already flushed this session or nothing was recorded.
	if r.flushed || len(r.calls) == 0 {
		return "", nil
	}
	sess := r.computeSession()
	prior := r.readPriorSessions()
	summary := r.formatSummary(sess, prior)

	var firstErr error

	// Write human-readable log.
	if r.humanPath != "" {
		f, err := os.OpenFile(r.humanPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
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
		f, err := os.OpenFile(r.jsonPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
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

	// Clean up per-process .current files now that data is in the main log.
	pid := os.Getpid()
	if r.humanPath != "" {
		os.Remove(fmt.Sprintf("%s.current.%d", r.humanPath, pid)) //nolint:errcheck
	}
	if r.jsonPath != "" {
		os.Remove(fmt.Sprintf("%s.current.%d", r.jsonPath, pid)) //nolint:errcheck
	}

	if firstErr == nil {
		r.flushed = true
	}
	return summary, firstErr
}

// ── computation ──────────────────────────────────────────────────────────────

type computedSession struct {
	data            sessionData
	duration        time.Duration // call-span (last_call - first_call), not wall-clock
	byTool          map[string]int
	errors          int
	totalCalls      int
	grade           string
	domains         map[string]int
	staleChecks     int
	staleCandidates int
	staleByType     [5]int
	dupEdges        [4]int
}

func (r *Recorder) computeSession() computedSession {
	var transientFiled, errors int
	sessionNodeIDs := make(map[string]struct{})
	connectedNodeIDs := make(map[string]struct{})
	var edgesInline, edgesConnect int
	byTool := make(map[string]int)
	domains := make(map[string]int)
	var retrievalTotal, retrievalToWrite int
	var staleChecks, staleCandidates int
	var staleByType [5]int
	var dupEdges [4]int
	hasWrites := false

	for i, call := range r.calls {
		byTool[call.tool]++
		if call.isError {
			errors++
			continue
		}

		// Node tracking
		for _, n := range call.nodes {
			if n.id != "" {
				sessionNodeIDs[n.id] = struct{}{}
				if n.hasInline {
					connectedNodeIDs[n.id] = struct{}{}
				}
				if n.transient {
					transientFiled++
				}
				if n.domain != "" {
					domains[n.domain]++
				}
			}
		}

		// Inline edges
		edgesInline += call.inlineEdges

		// Connect pairs — mark session nodes as connected
		for _, pair := range call.edgePairs {
			edgesConnect++
			if _, ok := sessionNodeIDs[pair[0]]; ok {
				connectedNodeIDs[pair[0]] = struct{}{}
			}
			if _, ok := sessionNodeIDs[pair[1]]; ok {
				connectedNodeIDs[pair[1]] = struct{}{}
			}
		}
		// Batch-connect count where we don't have specific pairs
		edgesConnect += call.edgesCount

		if call.kind == kindWrite {
			hasWrites = true
		}
		if call.kind == kindRetrieval {
			retrievalTotal++
			for j := i + 1; j <= i+3 && j < len(r.calls); j++ {
				if r.calls[j].kind == kindWrite && !r.calls[j].isError {
					retrievalToWrite++
					break
				}
			}
		}
		if call.staleTotal > 0 {
			staleChecks++
			staleCandidates += call.staleTotal
			for t := range staleByType {
				staleByType[t] += call.staleByType[t]
			}
			for b := range dupEdges {
				dupEdges[b] += call.dupEdges[b]
			}
		}
	}

	nodesFiled := len(sessionNodeIDs)
	edgesTotal := edgesInline + edgesConnect
	orphans := imax(0, nodesFiled-len(connectedNodeIDs))
	connected := nodesFiled - orphans

	ratio := 0.0
	if retrievalTotal > 0 {
		ratio = float64(retrievalToWrite) / float64(retrievalTotal)
	}

	// WKD: +2 per connected node, +1.5 per edge, -1 per orphan,
	//      -0.5 per transient node, +10x retrieval-action ratio.
	wkd := float64(connected)*2.0 +
		float64(edgesTotal)*1.5 -
		float64(orphans)*1.0 -
		float64(transientFiled)*0.5 +
		ratio*10.0
	wkd = math.Round(wkd*10) / 10

	// Session type: "filing" if any write tool was called.
	// "retrieval" if no writes and enough calls. "minimal" otherwise.
	sessType := "filing"
	if nodesFiled == 0 && edgesTotal == 0 && !hasWrites {
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

	var activeDuration time.Duration
	if r.firstCallTS != nil && r.lastCallTS != nil {
		activeDuration = r.lastCallTS.Sub(*r.firstCallTS)
	}
	wallClockSecs := int(time.Since(r.start).Seconds())

	var inlineShare float64
	if edgesTotal > 0 {
		inlineShare = math.Round(float64(edgesInline)/float64(edgesTotal)*100) / 100
	}
	var edgesPerNode float64
	if nodesFiled > 0 {
		edgesPerNode = math.Round(float64(edgesTotal)/float64(nodesFiled)*100) / 100
	}

	return computedSession{
		data: sessionData{
			SessionID:       r.sessionID,
			StartTS:         r.start,
			WKD:             wkd,
			Type:            sessType,
			NodesFiled:      nodesFiled,
			EdgesFiled:      edgesTotal,
			EdgesInline:     edgesInline,
			EdgesConnect:    edgesConnect,
			InlineShare:     inlineShare,
			EdgesPerNode:    edgesPerNode,
			Orphans:         orphans,
			Transient:       transientFiled,
			Ratio:           math.Round(ratio*100) / 100,
			Burst:           nodesFiled > 15,
			ActiveSecs:      int(activeDuration.Seconds()),
			WallClockSecs:   wallClockSecs,
			Client:          r.client,
			StaleChecks:     staleChecks,
			StaleCandidates: staleCandidates,
			DupEdge0:        dupEdges[0],
			DupEdge12:       dupEdges[1],
			DupEdge35:       dupEdges[2],
			DupEdge6p:       dupEdges[3],
		},
		duration:        activeDuration,
		byTool:          byTool,
		errors:          errors,
		totalCalls:      len(r.calls),
		grade:           grade,
		domains:         domains,
		staleChecks:     staleChecks,
		staleCandidates: staleCandidates,
		staleByType:     staleByType,
		dupEdges:        dupEdges,
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

	var raw []sessionData
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var sd sessionData
		if json.Unmarshal([]byte(line), &sd) == nil {
			raw = append(raw, sd)
		}
	}
	return deduplicateSessions(raw)
}

// deduplicateSessions collapses duplicate rows for the same session.
// Key: session_id when present; start_ts (RFC3339 precision) for legacy rows.
// Keeps the row with the most nodes filed; ties go to the later (last) row.
// Preserves original order of first occurrence.
func deduplicateSessions(sessions []sessionData) []sessionData {
	type key struct{ id, ts string }
	best := make(map[key]sessionData)
	var order []key

	for _, s := range sessions {
		var k key
		if s.SessionID != "" {
			k = key{id: s.SessionID}
		} else {
			k = key{ts: s.StartTS.UTC().Format(time.RFC3339)}
		}
		if existing, seen := best[k]; !seen {
			best[k] = s
			order = append(order, k)
		} else {
			// Prefer the row with more nodes; break ties by taking the later row.
			if s.NodesFiled > existing.NodesFiled ||
				(s.NodesFiled == existing.NodesFiled) {
				best[k] = s
			}
		}
	}

	out := make([]sessionData, 0, len(order))
	for _, k := range order {
		out = append(out, best[k])
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
	scores := make([]float64, len(vals))
	copy(scores, vals)
	sort.Float64s(scores)
	count := len(scores)
	if count%2 == 0 {
		return (scores[count/2-1] + scores[count/2]) / 2
	}
	return scores[count/2]
}

// ── formatting ────────────────────────────────────────────────────────────────

func (r *Recorder) formatSummary(sess computedSession, prior []sessionData) string {
	var sb strings.Builder
	now := time.Now().UTC()

	sb.WriteString(fmt.Sprintf("\n=== memoryweb session -- %s ===\n", now.Format("2006-01-02 15:04 UTC")))
	if sess.data.Client != "" {
		sb.WriteString(fmt.Sprintf("Client        %s\n", sess.data.Client))
	}

	// Active time: call-span (last_call - first_call), not wall-clock process uptime.
	mins := int(sess.duration.Minutes())
	if mins < 1 {
		mins = 1
	}
	sb.WriteString(fmt.Sprintf("Active %d min | %d tool calls", mins, sess.totalCalls))
	if len(sess.domains) > 0 {
		var dp []string
		for domain, count := range sess.domains {
			dp = append(dp, fmt.Sprintf("%s (%d)", domain, count))
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
	for tool, count := range sess.byTool {
		tc = append(tc, toolCount{tool, count})
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

	if sess.staleChecks > 0 {
		typeNames := [5]string{"contradicts", "superseded", "stale", "duplicate", "transient"}
		var parts []string
		for i, count := range sess.staleByType {
			if count > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", count, typeNames[i]))
			}
		}
		breakdown := ""
		if len(parts) > 0 {
			breakdown = " (" + strings.Join(parts, ", ") + ")"
		}
		sb.WriteString(fmt.Sprintf("  Stale checks  %d call(s), %d candidate(s)%s\n",
			sess.staleChecks, sess.staleCandidates, breakdown))
		dupTotal := sess.dupEdges[0] + sess.dupEdges[1] + sess.dupEdges[2] + sess.dupEdges[3]
		if dupTotal > 0 {
			sb.WriteString(fmt.Sprintf("  Dup edges     %dx(0)  %dx(1-2)  %dx(3-5)  %dx(6+)\n",
				sess.dupEdges[0], sess.dupEdges[1], sess.dupEdges[2], sess.dupEdges[3]))
		}
	}

	if len(prior) > 0 {
		now30 := now.AddDate(0, 0, -30)
		now7 := now.AddDate(0, 0, -7)
		var wkd30, wkd7 []float64
		var total30, ret30 int

		for _, previous := range prior {
			if previous.StartTS.Before(now30) {
				continue
			}
			total30++
			if previous.Type == "retrieval" {
				ret30++
				continue
			}
			if !previous.Burst {
				wkd30 = append(wkd30, previous.WKD)
				if previous.StartTS.After(now7) {
					wkd7 = append(wkd7, previous.WKD)
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

// isBatchArgs returns true if args contains the given key as an array.
func isBatchArgs(args json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(args, &m) != nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	return len(v) > 2 && v[0] == '[' // non-empty array
}

// parseRememberSingle extracts nodeInfo and inline edge count from a single remember call.
func parseRememberSingle(argsRaw json.RawMessage, resultText string) (nodes []nodeInfo, inlineEdges int) {
	var args struct {
		RelatedTo []json.RawMessage `json:"related_to"`
		Transient bool              `json:"transient"`
		NodeKind  string            `json:"node_kind"`
		Domain    string            `json:"domain"`
	}
	var result struct {
		Node struct {
			ID       string `json:"id"`
			NodeKind string `json:"node_kind"`
		} `json:"node"`
		SkippedConnections []json.RawMessage `json:"skipped_connections"`
	}
	json.Unmarshal(argsRaw, &args)              //nolint:errcheck
	json.Unmarshal([]byte(resultText), &result) //nolint:errcheck

	if result.Node.ID == "" {
		return
	}
	isTransient := result.Node.NodeKind == "transient" || args.Transient || args.NodeKind == "transient"
	skipped := len(result.SkippedConnections)
	inline := imax(0, len(args.RelatedTo)-skipped)
	nodes = []nodeInfo{{
		id:        result.Node.ID,
		hasInline: inline > 0,
		transient: isTransient,
		domain:    args.Domain,
	}}
	inlineEdges = inline
	return
}

// parseRememberBatch extracts nodeInfos and total inline edge count from a batch
// remember call (args.items format).
func parseRememberBatch(argsRaw json.RawMessage, resultText string) (nodes []nodeInfo, inlineEdges int) {
	var args struct {
		Items []struct {
			RelatedTo []json.RawMessage `json:"related_to"`
			Transient bool              `json:"transient"`
			NodeKind  string            `json:"node_kind"`
			Domain    string            `json:"domain"`
		} `json:"items"`
	}
	var result struct {
		Nodes []struct {
			Node struct {
				ID       string `json:"id"`
				NodeKind string `json:"node_kind"`
			} `json:"node"`
			SkippedConnections []json.RawMessage `json:"skipped_connections"`
		} `json:"nodes"`
	}
	json.Unmarshal(argsRaw, &args)              //nolint:errcheck
	json.Unmarshal([]byte(resultText), &result) //nolint:errcheck

	for i, rn := range result.Nodes {
		if rn.Node.ID == "" {
			continue
		}
		isTransient := rn.Node.NodeKind == "transient"
		var nodeInline int
		var domain string
		if i < len(args.Items) {
			if args.Items[i].Transient || args.Items[i].NodeKind == "transient" {
				isTransient = true
			}
			skipped := len(rn.SkippedConnections)
			nodeInline = imax(0, len(args.Items[i].RelatedTo)-skipped)
			domain = args.Items[i].Domain
		}
		nodes = append(nodes, nodeInfo{
			id:        rn.Node.ID,
			hasInline: nodeInline > 0,
			transient: isTransient,
			domain:    domain,
		})
		inlineEdges += nodeInline
	}
	return
}

// parseRememberAll extracts nodeInfos from legacy remember_all (args.nodes format).
func parseRememberAll(argsRaw json.RawMessage, resultText string) (nodes []nodeInfo, inlineEdges int) {
	var args struct {
		Nodes []struct {
			RelatedTo []json.RawMessage `json:"related_to"`
			Transient bool              `json:"transient"`
			NodeKind  string            `json:"node_kind"`
			Domain    string            `json:"domain"`
		} `json:"nodes"`
	}
	var result struct {
		Nodes []struct {
			Node struct {
				ID       string `json:"id"`
				NodeKind string `json:"node_kind"`
			} `json:"node"`
			SkippedConnections []json.RawMessage `json:"skipped_connections"`
		} `json:"nodes"`
	}
	json.Unmarshal(argsRaw, &args)              //nolint:errcheck
	json.Unmarshal([]byte(resultText), &result) //nolint:errcheck

	for i, rn := range result.Nodes {
		if rn.Node.ID == "" {
			continue
		}
		isTransient := rn.Node.NodeKind == "transient"
		var nodeInline int
		var domain string
		if i < len(args.Nodes) {
			if args.Nodes[i].Transient || args.Nodes[i].NodeKind == "transient" {
				isTransient = true
			}
			skipped := len(rn.SkippedConnections)
			nodeInline = imax(0, len(args.Nodes[i].RelatedTo)-skipped)
			domain = args.Nodes[i].Domain
		}
		nodes = append(nodes, nodeInfo{
			id:        rn.Node.ID,
			hasInline: nodeInline > 0,
			transient: isTransient,
			domain:    domain,
		})
		inlineEdges += nodeInline
	}
	return
}

// parseConnectSingle extracts the from/to node IDs from a single connect call.
// It tries the result JSON first (full Edge with from_memory/to_memory), then
// falls back to args (also checking legacy from_node/to_node for test fixtures).
func parseConnectSingle(argsRaw json.RawMessage, resultText string) (pair [2]string, ok bool) {
	// Try result first — a successful connect returns the full Edge JSON.
	var edge struct {
		FromMemory string `json:"from_memory"`
		ToMemory   string `json:"to_memory"`
	}
	if json.Unmarshal([]byte(resultText), &edge) == nil && edge.FromMemory != "" {
		return [2]string{edge.FromMemory, edge.ToMemory}, true
	}
	// Fall back to args.
	var args struct {
		FromMemory string `json:"from_memory"`
		ToMemory   string `json:"to_memory"`
		FromNode   string `json:"from_node"` // legacy key used in test fixtures
		ToNode     string `json:"to_node"`   // legacy key used in test fixtures
	}
	if json.Unmarshal(argsRaw, &args) == nil {
		from := args.FromMemory
		if from == "" {
			from = args.FromNode
		}
		to := args.ToMemory
		if to == "" {
			to = args.ToNode
		}
		if from != "" && to != "" {
			return [2]string{from, to}, true
		}
	}
	return [2]string{}, false
}

// parseConnectBatch extracts from/to pairs from a batch connect call's args items.
// Falls back to result edge count when args parsing yields no pairs.
func parseConnectBatch(argsRaw json.RawMessage, resultText string) (pairs [][2]string, count int) {
	var args struct {
		Items []struct {
			FromMemory string `json:"from_memory"`
			ToMemory   string `json:"to_memory"`
		} `json:"items"`
	}
	if json.Unmarshal(argsRaw, &args) == nil {
		for _, item := range args.Items {
			if item.FromMemory != "" && item.ToMemory != "" {
				pairs = append(pairs, [2]string{item.FromMemory, item.ToMemory})
			}
		}
		if len(pairs) > 0 {
			return pairs, 0
		}
	}
	return nil, parseEdgesCreated(resultText)
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

// parseWhatsStaleResult parses the JSON result text from a whats_stale call.
// Returns total candidate count, per-type counts ([contradicts, superseded,
// stale, duplicate, transient]), and a dup-candidate edge-count histogram
// ([0 edges, 1-2, 3-5, 6+]).
func parseWhatsStaleResult(resultText string) (total int, byType [5]int, dupEdges [4]int) {
	var candidates []struct {
		Reason    string `json:"reason"`
		EdgeCount int    `json:"edge_count"`
	}
	if json.Unmarshal([]byte(resultText), &candidates) != nil {
		return
	}
	total = len(candidates)
	for _, candidate := range candidates {
		idx := staleTypeIdx(candidate.Reason)
		byType[idx]++
		if idx == staleDuplicate {
			dupEdges[edgeBucket(candidate.EdgeCount)]++
		}
	}
	return
}

func staleTypeIdx(reason string) int {
	switch {
	case strings.HasPrefix(reason, "explicitly"):
		return staleContradicts
	case strings.HasPrefix(reason, "label suggests"):
		return staleSuperseded
	case strings.HasPrefix(reason, "open question"):
		return staleOpenQ
	case strings.HasPrefix(reason, "possible duplicate"):
		return staleDuplicate
	default:
		return staleTransient
	}
}

func edgeBucket(n int) int {
	switch {
	case n == 0:
		return 0
	case n <= 2:
		return 1
	case n <= 5:
		return 2
	default:
		return 3
	}
}

// isAllDigits reports whether s is a non-empty string of ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
