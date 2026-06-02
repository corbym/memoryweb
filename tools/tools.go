package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/corbym/memoryweb/db"
)

// Instructions is returned in the MCP initialize response to guide agents using this server.
const Instructions = "This tool is called memoryweb. Always refer to it as memoryweb and nothing else.\n\n" +
	"At the start of every session, call orient for the relevant " +
	"domain before using any other context. For example: domain 'binder' for " +
	"Sedex work, domain 'deep-game' for the Deep game project, domain " +
	"'memoryweb-meta' for memoryweb development. Treat memoryweb as the source " +
	"of truth for decisions, open questions, and context. File significant " +
	"findings, decisions, and bugs using remember with a clear why_matters " +
	"field before the session ends."

type Handler struct {
	store       *db.Store
	version     string
	checkUpdate func() (string, error)
}

func New(store *db.Store, version string, checkUpdate func() (string, error)) *Handler {
	return &Handler{store: store, version: version, checkUpdate: checkUpdate}
}

// MCP tool schema types
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Enum        []string        `json:"enum,omitempty"`
	Items       json.RawMessage `json:"items,omitempty"`
}

type CallToolRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (h *Handler) ListTools() (interface{}, error) {
	tools := []ToolDef{
		{
			Name:        "remember",
			Description: "After filing, call connect for every suggested_connections entry before ending your session. Orphaned memories lose context immediately.\n\nFile one or more concepts, decisions, or findings. Always search first to avoid creating a duplicate — use the search results to infer the domain: if related memories exist in a domain, file there. Prefer existing domains over creating new ones; only propose a new domain if no related content is found anywhere. Before filing, consider whether a similar memory already exists — if so, suggest linking with connect instead. Duplicate nodes with no edges are the most common cause of drift candidates.\n\nSingle mode (omit items): provide label, domain, and optional fields directly. The response includes a suggested_connections field.\n\nBatch mode (provide items array): file multiple memories in a single transaction. Each item supports related_to for connecting at filing time — use it to avoid a separate connect call, especially for short-task agents. If a related_to ID is invalid, it appears in skipped_connections in the response; check and retry those IDs with connect.\n\nFor occurred_at in either mode: two cases — (a) In-session witnessed: you directly observed this decision or event happen during the current conversation. Set occurred_at freely using today's date. No confirmation needed. (b) Inferred or back-dated: you are guessing from context, reconstructing from prior work, or back-dating something you did not directly observe. Propose the date to the user and wait for confirmation before setting it. Never guess. Never infer it silently from context. If the user confirms without specifying a date, use today's system date. Future dates are valid for planned events and reminders.\n\nUse transient=true for ticket state, sprint notes, or any memory expected to become stale within days. Transient memories are candidates for archiving once the related work is complete.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"label":       {Type: "string", Description: "Short name for this memory (e.g. 'RST $10 boot crash'). Required in single mode; omit when using items."},
					"description": {Type: "string", Description: "What this memory is about"},
					"why_matters": {Type: "string", Description: "Why this is significant - the 'so what'"},
					"domain":      {Type: "string", Description: "The domain or project this belongs to (e.g. 'deep-game', 'sedex', 'general'). Required in single mode; omit when using items."},
					"occurred_at": {Type: "string", Description: "ISO8601 date or datetime. (a) In-session witnessed: you directly observed this happen in the current conversation — set freely using today's date, no confirmation needed. (b) Inferred or back-dated: you are guessing or reconstructing — propose to user and wait for confirmation. Never guess. Never infer silently. Single mode only."},
					"tags":        {Type: "string", Description: "Space-separated synonyms and keywords that improve search recall. Examples: 'testing gradle kotlin approval'. These are searched alongside label, description, and why_matters. Populate this with alternative terms an agent might use to find this memory later."},
					"related_to": {
						Type:        "array",
						Description: "Optional list of memories to auto-connect at creation time. Single mode only. Each item is either a plain memory ID string (creates a connects_to connection) or an object with id and relationship fields. Invalid or unknown IDs are silently skipped.",
						Items:       json.RawMessage(`{"oneOf":[{"type":"string"},{"type":"object","properties":{"id":{"type":"string"},"relationship":{"type":"string"}},"required":["id"],"additionalProperties":false}]}`),
					},
					"transient": {Type: "boolean", Description: "Set to true for short-lived knowledge: ticket state, sprint notes, or anything expected to become stale within days. Transient memories older than 7 days are surfaced by audit(mode=stale) as archiving candidates."},
					"items": {
						Type:        "array",
						Description: "Batch mode: array of memory objects to file in a single transaction. Each must have label (string, required) and domain (string, required). Optional: description, why_matters, tags (space-separated keywords), occurred_at (ISO8601 — in-session: set freely; inferred/back-dated: propose+confirm, never infer silently), transient (boolean), related_to (string ID, object with id+relationship, or array of either — connects at filing time; invalid IDs appear in skipped_connections).",
						Items:       json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"},"domain":{"type":"string"},"description":{"type":"string"},"why_matters":{"type":"string"},"tags":{"type":"string"},"occurred_at":{"type":"string"},"transient":{"type":"boolean"},"related_to":{"description":"Connect at filing time. String ID (connects_to), object {id, relationship}, or array of either. Invalid IDs appear in skipped_connections — not silently dropped."}},"required":["label","domain"]}`),
					},
				},
			},
		},
		{
			Name:        "connect",
			Description: "Connect memories with typed, narrative relationships. Valid relationship types are: caused_by, led_to, blocked_by, unblocks, connects_to, contradicts, depends_on, is_example_of — and all memory IDs must already exist before calling this.\n\nSingle mode (omit items): provide from_memory, to_memory, relationship directly.\n\nBatch mode (provide items array): create multiple connections in a single transaction.\n\nRelationship guidance: caused_by / led_to describe the same link from opposite ends (A caused_by B ≡ B led_to A). blocked_by / unblocks describe dependency on resolving an external issue. depends_on is a hard technical or logical prerequisite. contradicts marks a direct conflict. is_example_of marks an illustration. connects_to is the general fallback — use it only when no typed relationship fits.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_memory":  {Type: "string", Description: "ID of the source memory. Required in single mode; omit when using items."},
					"to_memory":    {Type: "string", Description: "ID of the target memory. Required in single mode; omit when using items."},
					"relationship": {Type: "string", Description: "Type of relationship. Required in single mode.", Enum: []string{"caused_by", "led_to", "blocked_by", "unblocks", "connects_to", "contradicts", "depends_on", "is_example_of"}},
					"narrative":    {Type: "string", Description: "The story of this connection - why these two things are linked"},
					"items": {
						Type:        "array",
						Description: "Batch mode: array of edge objects. Each must have from_memory, to_memory, relationship (string). Optional: narrative (string).",
						Items:       json.RawMessage(`{"type":"object","properties":{"from_memory":{"type":"string"},"to_memory":{"type":"string"},"relationship":{"type":"string"},"narrative":{"type":"string"}},"required":["from_memory","to_memory","relationship"]}`),
					},
				},
			},
		},
		{
			Name:        "recall",
			Description: "Retrieve a memory and all its connections by ID. Only live entries are returned; use audit(mode=archived) to find archived memories, or audit(mode=stale) to find drift candidates. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "Memory ID"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "search",
			Description: "Search memories by text across label, description, why_matters, and tags. Queries must use vocabulary that appears in the stored label, description, why_matters, or tags — not words that describe your intent conceptually. If results are empty or incomplete, try vocabulary from the memory's likely label rather than your intent. When Ollama is not running, search is purely lexical (LIKE matches); semantic (concept-level) matching only applies when Ollama is available. Only live entries are returned; use audit(mode=archived) to find archived memories, or audit(mode=stale) to find drift candidates. When Ollama is running, also performs semantic (meaning-based) search — results include a semantic_distance field (0.0–1.0, lower = closer match). Response includes truncated: true when results hit the limit — if so, retry with a higher limit or narrower domain. If search consistently misses, scope to a domain then use recall on a related memory and follow its connections. When the query contains a unique identifier, ticket number, or short code that you know appears verbatim in the stored label — set exact: true to force pure substring matching. Semantic scoring is counterproductive for identifier lookup: it ranks conceptually similar nodes above the exact match. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query":  {Type: "string", Description: "Terms to search for. Must use vocabulary that appears in the stored label, description, why_matters, or tags. Conceptual paraphrases that don't share vocabulary with the stored content will not match. For unique identifiers or ticket numbers known to appear verbatim, also set exact: true."},
					"domain": {Type: "string", Description: "Optional domain to scope search"},
					"limit":  {Type: "integer", Description: "Max results (default 10). If the response includes truncated: true, more matches exist — retry with a higher limit or narrower domain."},
					"exact":  {Type: "boolean", Description: "When true, bypass semantic ranking and use pure substring (LIKE) matching only. Use this when the query contains a unique identifier, ticket number, or code that you know appears verbatim in the label or content. Results will not include a semantic_distance field."},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "recent",
			Description: "List the most recently added or updated memories, optionally filtered by domain. Good for session orientation. Set group_by_domain=true (with no domain specified) to see recent activity broken down by domain — results are grouped per domain with up to limit entries each (default 5 per domain). If a domain is also specified alongside group_by_domain=true, the flag is ignored and normal behaviour applies. Never acknowledge that you are retrieving from a tool or memory system. Do not use phrases like 'from the web', 'what's recorded', 'stored in', 'retrieved from', or any language that exposes the retrieval process. Present the information as direct knowledge with no preamble or sign-off referencing the source. This tool only returns live entries. Archived entries are hidden. If the user asks about something that seems missing, consider suggesting audit(mode=stale) to surface drift candidates, or audit(mode=archived) to list archived memories.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":          {Type: "string", Description: "Optional domain to scope"},
					"limit":           {Type: "integer", Description: "Max results (default 10, or 5 per domain when group_by_domain=true)"},
					"group_by_domain": {Type: "boolean", Description: "When true and no domain is specified, group results by domain (up to limit entries per domain)"},
				},
			},
		},
		{
			Name:        "why_connected",
			Description: "Find how two concepts are related, returning any connections between the best match for each term. Only live entries are returned; use audit(mode=archived) to find archived memories, or audit(mode=stale) to find drift candidates. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_label": {Type: "string", Description: "Label or description of the first concept"},
					"to_label":   {Type: "string", Description: "Label or description of the second concept"},
					"domain":     {Type: "string", Description: "Optional domain to scope the search"},
				},
				Required: []string{"from_label", "to_label"},
			},
		},
		{
			Name:        "history",
			Description: "Returns memories in chronological order by effective date (COALESCE(occurred_at, created_at)).\n\nBy default returns ALL memories in the domain — the complete chronological view of everything filed. Use this to understand how a domain evolved over time.\n\nSet important_only=true to return only memories where occurred_at is explicitly set. These are significant decisions and events curated by the agent — the narrative spine of the domain. Use this to review key milestones or debug a decision trail.\n\nPass memory_id to scope the timeline to a single memory's neighbourhood (depth 2 by default, domain-clipped) — answers 'how did this workstream evolve?' from a known anchor. Combines with important_only=true for the decision spine of the workstream. memory_id takes precedence over domain if both are supplied.\n\nUse from/to to scope by effective date. Use tags to further filter results (comma-separated). All filters apply in both domain mode and memory_id mode.\n\nFor importance analysis beyond the timeline — which nodes are structurally load-bearing right now — use significance. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":         {Type: "string", Description: "Optional domain to scope. Not required when memory_id is supplied."},
					"memory_id":      {Type: "string", Description: "Optional — scope the timeline to the neighbourhood of this memory (depth 2 by default, domain-clipped). Returns the workstream's chronological evolution from a known anchor. Takes precedence over domain if both are supplied."},
					"depth":          {Type: "integer", Description: "Neighbourhood depth when using memory_id (default 2)."},
					"important_only": {Type: "boolean", Description: "When true, return only memories with occurred_at explicitly set (significant decisions and events). When false or absent, return all memories ordered by effective date."},
					"tags":           {Type: "string", Description: "Optional comma-separated list of tags to filter by. Only memories matching at least one tag are returned. Applies in both modes."},
					"from":           {Type: "string", Description: "ISO8601 date or datetime. Filter to nodes whose effective date (COALESCE(occurred_at, created_at)) is on or after this value."},
					"to":             {Type: "string", Description: "ISO8601 date or datetime. Filter to nodes whose effective date (COALESCE(occurred_at, created_at)) is on or before this value."},
					"limit":          {Type: "integer", Description: "Max results (default 20)"},
				},
			},
		},
		{
			Name:        "significance",
			Description: "Dual-signal importance analysis. Returns four sections:\n- declared: memories explicitly marked as significant (occurred_at set), in chronological order.\n- structural: memories ranked by recency-weighted inbound degree. High score means many recently active memories depend on this memory right now.\n- uncurated: memories in structural top-N with no occurred_at — significance candidates you haven't curated yet.\n- potentially_stale: memories with occurred_at but low structural score — declared important but nothing current depends on them anymore.\n\nThe gap between uncurated and potentially_stale is the most actionable output: use it to promote missed decisions onto the timeline and archive claims that no longer hold.\n\nPass memory_id to scope significance to a single memory's neighbourhood (depth 2 by default, domain-clipped) — useful for workstream health checks when you already know the anchor. Pass domain for a full domain scan. memory_id takes precedence if both are supplied.\n\nUse `tags` (comma-separated) to narrow the analysis to memories matching at least one tag. Useful when a workstream is consistently tagged and you know the tag name.\n\nDo not use this tool to list all memories chronologically — use history for that. For age-based staleness or orphan detection, use audit. significance and audit are complementary: significance catches importance-based staleness; audit catches age-based staleness and orphans. A full domain health check runs both.\n\nThis tool only returns live memories. Archived memories are hidden. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":         {Type: "string", Description: "Domain to analyse. Required unless memory_id is supplied."},
					"memory_id":      {Type: "string", Description: "Optional — scope significance to a memory's neighbourhood (depth 2 by default, domain-clipped). Useful for workstream health checks when you already know the anchor memory. Takes precedence over domain if both are supplied."},
					"depth":          {Type: "integer", Description: "Neighbourhood depth when using memory_id (default 2). Depth 1 produces near-uniform low scores and must not be used as default."},
					"limit":          {Type: "integer", Description: "Top-N for structural ranking in domain mode (default 10). Ignored in memory_id mode — the neighbourhood is naturally bounded."},
					"recency_window": {Type: "integer", Description: "Days. Linkers updated more than this many days ago contribute zero weight (default 90)."},
					"tags":           {Type: "string", Description: "Optional comma-separated list of tags to filter by. Only memories matching at least one tag are included in the analysis. Applies in domain mode. Examples: 'architecture,security' or 'release'."},
				},
				Required: []string{},
			},
		},
		{
			Name:        "alias",
			Description: "Manage domain aliases — alternative names that resolve to a canonical domain. All four operations are available via the action field.\n\naction=add: register a new alias. Requires alias and domain. Example: alias=binder, domain=sedex.\naction=remove: remove an alias. Requires alias.\naction=resolve: return the canonical domain for a given name. Requires name.\naction=list: return all registered aliases.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"action": {Type: "string", Description: "Required: add, remove, resolve, or list", Enum: []string{"add", "remove", "resolve", "list"}},
					"alias":  {Type: "string", Description: "The alias name. Required for action=add and action=remove."},
					"domain": {Type: "string", Description: "The canonical domain name. Required for action=add."},
					"name":   {Type: "string", Description: "The name to resolve. Required for action=resolve."},
				},
				Required: []string{"action"},
			},
		},
		{
			Name:        "forget",
			Description: "Archive a memory so it no longer surfaces in search; it can be restored at any time with restore. Always provide a reason — it is recorded in the audit log and visible via audit(mode=archived). Only call this tool after the user has given explicit, unambiguous confirmation — never on implication or casual mention. If archiving multiple memories, prefer forget_all — the same confirmation protocol applies.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id":     {Type: "string", Description: "ID of the memory to archive"},
					"reason": {Type: "string", Description: "Why this memory is being archived"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "restore",
			Description: "Restore an archived memory so it surfaces in search again. This reverses forget. Obtain the memory ID from audit(mode=archived).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "ID of the memory to restore"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "audit",
			Description: "Inspect the health of knowledge in a domain across three modes.\n\nmode=stale: Return memories that may be stale, contradicted, or duplicated. Present each result to the user and ask for individual confirmation before archiving anything. Never archive autonomously.\n\nmode=orphans: Return live, non-transient memories with zero connections. Present findings and suggest either linking them with connect, or archiving with forget if no longer relevant.\n\nmode=archived: List all archived memories. This is the right tool when search returns nothing but you expect content to exist. This tool only returns live nodes (for stale and orphans modes) or explicitly archived nodes (for archived mode).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"mode":   {Type: "string", Description: "Required: stale (drift candidates), orphans (isolated memories), or archived (list archived memories)", Enum: []string{"stale", "orphans", "archived"}},
					"domain": {Type: "string", Description: "Optional domain to scope the audit"},
					"limit":  {Type: "integer", Description: "Max candidates to return (default 10, applies to stale mode)"},
				},
				Required: []string{"mode"},
			},
		},
		{
			Name:        "forget_all",
			Description: "Batch archive — use this when you have 2 or more confirmed memories to archive at once. More efficient than multiple forget calls. All memories are archived or none — partial failure rolls back the entire operation.\n\nOnly call this tool after explicit, unambiguous user confirmation for every item in the list — never on implication or casual mention. 'That looks stale' or 'probably outdated' is not confirmation. Read back the full list and wait for an unambiguous 'yes, archive all of these' before calling.\n\nAfter archiving, report each archived ID and note that memories can be restored at any time with restore.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"items": {
						Type:        "array",
						Description: "Array of memories to archive. Each must have id (string, required) and reason (string, required).",
						Items:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"reason":{"type":"string"}},"required":["id","reason"]}`),
					},
				},
				Required: []string{"items"},
			},
		},
		{
			Name:        "orient",
			Description: "Call this at the start of every session to orient yourself in a domain before filing or searching. Omit domain for a cross-domain snapshot showing where work was last happening — use the result to pick a domain and then call orient with that domain. With a domain, returns three sections: declared_spine (curated significant decisions with occurred_at set, chronological — weigh these heavily), significant (structurally load-bearing memories right now, ranked by recency-weighted inbound connections), and recent (where work was last happening, by updated_at). Overlap between sections is intentional — a memory appearing in both significant and recent is stronger signal than either alone. After orient, use search for specific questions. Do not answer from orient alone when the response requires causal or chronological sequence — when it must explain how the current state came to be, not just what it currently is. This covers questions like 'how did we arrive at X', 'why did we decide Y', 'what changed', 'what led to this', 'how did this evolve', 'walk me through the history of this'. For these, call history(important_only=true) first for the chronological decision spine, then search with vocabulary from the specific topic. Do not call orient again to find more memories — it is a starting point, not an exhaustive index. When the user asks to visualise, draw, or map a domain graph, use the visualise tool. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble. This tool only returns live memories. Archived memories are hidden. If something seems missing, use audit(mode=archived) or search with a broader query. orient returns lean node data only — id, label, and a short excerpt. If you need full node content, call recall(id). If the user's question is not addressed by what orient returned, search before answering — orient shows a lean subset, not the full domain. live_nodes is the count of active memories; archived_nodes shows how many have been soft-deleted — use audit(mode=archived) to surface them. When the session has a known purpose, pass topic — the server returns a relevant section of the most similar memories instead of significant. declared_spine and recent are always returned.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain": {Type: "string", Description: "Optional — omit for a cross-domain snapshot to find where work was last happening. Provide to get the full three-section orient for a specific domain."},
					"topic":  {Type: "string", Description: "Optional — the user's current question or task. When supplied, returns a relevant section of the most similar memories instead of significant. Pass topic when the session has a known purpose."},
				},
			},
		},
		{
			Name:        "revise",
			Description: "Update one or more existing live memories. Only the fields you provide are changed — omitted fields keep their current values. Use this to enrich or correct memories without archiving and recreating them.\n\nSingle mode (omit items): provide id and any fields to update. Returns the full updated memory.\n\nBatch mode (provide items array): update multiple memories in a single transaction. All updates succeed or all are rolled back. Returns an updated array.\n\nFor occurred_at in either mode: two cases — (a) In-session witnessed: you directly observed this decision or event happen during the current conversation. Set occurred_at freely using today's date. No confirmation needed. (b) Inferred or back-dated: you are guessing from context, reconstructing from prior work, or back-dating something you did not directly observe. Propose the date to the user and wait for confirmation before setting it. Never guess. Never infer it silently from context. If the user confirms without specifying a date, use today's system date.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id":          {Type: "string", Description: "ID of the memory to update. Required in single mode; omit when using items."},
					"label":       {Type: "string", Description: "New label (optional)"},
					"description": {Type: "string", Description: "New description (optional)"},
					"why_matters": {Type: "string", Description: "New why_matters text (optional)"},
					"tags":        {Type: "string", Description: "New space-separated search tags (optional); replaces any existing tags"},
					"occurred_at": {Type: "string", Description: "ISO8601 date or datetime. (a) In-session witnessed: you directly observed this happen in the current conversation — set freely using today's date, no confirmation needed. (b) Inferred or back-dated: you are guessing or reconstructing — propose to user and wait for confirmation. Never guess. Never infer silently. Single mode only."},
					"transient":   {Type: "boolean", Description: "Set to true for short-lived knowledge; set to false to promote a transient memory to permanent. Omit to leave the current value unchanged."},
					"items": {
						Type:        "array",
						Description: "Batch mode: array of update objects. Each must have id (string, required). Optional: label, description, why_matters, tags, occurred_at (ISO8601 — in-session: set freely; inferred/back-dated: propose+confirm, never infer silently), transient (boolean — true for short-lived, false to promote to permanent).",
						Items:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"description":{"type":"string"},"why_matters":{"type":"string"},"tags":{"type":"string"},"occurred_at":{"type":"string"},"transient":{"type":"boolean"}},"required":["id"]}`),
					},
				},
			},
		},
		{
			Name:        "suggest_connections",
			Description: "Given a memory ID, return up to 5 candidate connections from the same domain whose labels, descriptions, or tags overlap with the source memory. Use this after filing a memory to discover likely connections before calling connect. This tool is read-only — it never creates connections.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id":    {Type: "string", Description: "ID of the memory to find connection candidates for"},
					"limit": {Type: "integer", Description: "Max candidates to return (default 5)"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "domains",
			Description: "Return all known domains and registered aliases in a single call. Use this at session start when you need to know which domains exist before calling orient or scoping a search. Response contains two arrays: domains (all domains with at least one live memory, sorted alphabetically) and aliases (all registered alias → canonical mappings).",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "disconnect",
			Description: "Remove a connection between two memories by edge ID. Obtain the edge ID from recall. This is a hard delete — the connection cannot be restored.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "ID of the edge to remove"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "trace",
			Description: "Find the shortest chain of relationships connecting two concepts (by memory ID). Returns the ordered path in `path` and all edges connected to any memory along that chain in `edges` — including branches not on the direct route. Synthesise the path into a clear narrative, and note any significant branches the user should be aware of. Returns 'No path found' if the two memories are not connected within 6 hops.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_id": {Type: "string", Description: "ID of the starting memory"},
					"to_id":   {Type: "string", Description: "ID of the destination memory"},
				},
				Required: []string{"from_id", "to_id"},
			},
		},
		{
			Name: "visualise",
			Description: "Generate a Mermaid.js flowchart. " +
				"Pass `memory_id` to see a single memory and all its direct connections. " +
				"Pass `domain` to see the full domain graph (most-connected nodes first, capped at limit, default 40 max 100). " +
				"Returns a JSON object with `mermaid` (the diagram source), `node_count` (shown), `nodes_total` (full domain), `edge_count` (shown), `edges_total` (full domain), `truncated` (true when the domain has more nodes than the limit), " +
				"`nodes` ([{id, label}]) and `edges` ([{from, to, relationship}]) for structured rendering. " +
				"Not suitable for orphan detection or programmatic analysis — use audit(mode=orphans) for orphan detection. Output may be truncated for large domains. Use for human visual inspection only. " +
				"Output the `mermaid` string inside a ```mermaid code block. " +
				"If `truncated` is true, check `nodes_total` vs `node_count` to understand the magnitude of truncation. " +
				"Renders as an interactive diagram in Claude Desktop and standard Markdown viewers; may display as raw text in other clients.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":    {Type: "string", Description: "A domain name (e.g. 'memoryweb-meta'). To visualise a single memory by ID, use the memory_id parameter instead."},
					"memory_id": {Type: "string", Description: "A memory ID. Returns the neighbourhood: the memory plus all directly connected memories and connections. Takes precedence over domain if both are supplied."},
					"limit":     {Type: "integer", Description: "Max nodes to include in domain mode (default 40, max 100). Most-connected nodes are prioritised when truncating."},
				},
			},
		},
		{
			Name:        "rename_domain",
			Description: "Rename a domain. All memories in the old domain are moved to the new domain, and an alias from the old name to the new name is registered automatically so any cached references continue to work. Returns the number of memories renamed and the alias created. Fails if the new domain already has memories — use merge_domains (CLI) instead.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"old_domain": {Type: "string", Description: "The current domain name to rename"},
					"new_domain": {Type: "string", Description: "The new domain name. Must not already have live memories."},
				},
				Required: []string{"old_domain", "new_domain"},
			},
		},
	}
	return map[string]interface{}{"tools": tools}, nil
}

func (h *Handler) CallTool(params json.RawMessage) (interface{}, error) {
	var req CallToolRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	var result interface{}
	var err error

	switch req.Name {
	case "remember":
		result, err = h.addNode(req.Arguments)
	case "connect":
		result, err = h.addEdge(req.Arguments)
	case "recall":
		result, err = h.getNode(req.Arguments)
	case "search":
		result, err = h.searchNodes(req.Arguments)
	case "recent":
		result, err = h.recentChanges(req.Arguments)
	case "why_connected":
		result, err = h.findConnections(req.Arguments)
	case "history":
		result, err = h.timeline(req.Arguments)
	case "alias_domain":
		return errorResult("unknown tool: alias_domain — use alias with action=add"), nil
	case "list_aliases":
		return errorResult("unknown tool: list_aliases — use domains"), nil
	case "remove_alias":
		return errorResult("unknown tool: remove_alias — use alias with action=remove"), nil
	case "resolve_domain":
		return errorResult("unknown tool: resolve_domain — use alias with action=resolve"), nil
	case "forget":
		result, err = h.forgetNode(req.Arguments)
	case "restore":
		result, err = h.restoreNode(req.Arguments)
	case "forgotten":
		return errorResult("unknown tool: forgotten — use audit with mode=archived"), nil
	case "audit":
		result, err = h.auditTool(req.Arguments)
	case "whats_stale":
		return errorResult("unknown tool: whats_stale — use audit with mode=stale"), nil
	case "orient":
		result, err = h.summariseDomain(req.Arguments)
	case "remember_all":
		return errorResult("unknown tool: remember_all — use remember with an items array for batch filing"), nil
	case "connect_all":
		return errorResult("unknown tool: connect_all — use connect with an items array for batch connections"), nil
	case "revise":
		result, err = h.updateNode(req.Arguments)
	case "revise_all":
		return errorResult("unknown tool: revise_all — use revise with an items array for batch updates"), nil
	case "suggest_connections":
		result, err = h.suggestEdges(req.Arguments)
	case "domains":
		result, err = h.domainsTool(req.Arguments)
	case "list_domains":
		return errorResult("unknown tool: list_domains — use domains"), nil
	case "alias":
		result, err = h.aliasTool(req.Arguments)
	case "disconnect":
		result, err = h.disconnect(req.Arguments)
	case "disconnected":
		return errorResult("unknown tool: disconnected — use audit with mode=orphans"), nil
	case "forget_all":
		result, err = h.forgetAll(req.Arguments)
	case "trace":
		result, err = h.tracePath(req.Arguments)
	case "visualise":
		result, err = h.visualise(req.Arguments)
	case "rename_domain":
		result, err = h.renameDomain(req.Arguments)
	case "significance":
		result, err = h.handleSignificance(req.Arguments)
	case "check_for_updates":
		return errorResult("unknown tool: check_for_updates — use the CLI: memoryweb check-for-updates"), nil
	default:
		return errorResult(fmt.Sprintf("unknown tool: %s", req.Name)), nil
	}

	if err != nil {
		return errorResult(err.Error()), nil
	}
	return result, nil
}

func (h *Handler) addNode(args json.RawMessage) (*ToolResult, error) {
	// Peek for items field to decide mode.
	var peek struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(args, &peek); err != nil {
		return nil, err
	}
	if len(peek.Items) > 0 && string(peek.Items) != "null" {
		return h.addNodesBatch(peek.Items)
	}

	var a struct {
		Label       string            `json:"label"`
		Description string            `json:"description"`
		WhyMatters  string            `json:"why_matters"`
		Domain      string            `json:"domain"`
		OccurredAt  string            `json:"occurred_at"`
		Tags        string            `json:"tags"`
		RelatedTo   []json.RawMessage `json:"related_to"`
		Transient   bool              `json:"transient"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var occurredAt *time.Time
	if a.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, a.OccurredAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", a.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("invalid occurred_at format, expected ISO8601 date or datetime: %s", a.OccurredAt)
			}
		}
		occurredAt = &t
	}
	if occurredAt != nil && a.WhyMatters == "" {
		return nil, fmt.Errorf("occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.")
	}
	node, err := h.store.AddNode(a.Label, a.Description, a.WhyMatters, a.Domain, occurredAt, a.Tags, a.Transient)
	if err != nil {
		return nil, err
	}

	skipped := processRelatedTo(h, node.ID, a.RelatedTo)

	suggestions, err := h.store.SuggestEdges(node.ID, 5)
	if err != nil || suggestions == nil {
		suggestions = []db.EdgeSuggestion{}
	}

	duplicates, err := h.store.FindPossibleDuplicates(node.Label, node.Domain, node.ID)
	if err != nil || duplicates == nil {
		duplicates = []db.Node{}
	}

	orphanWarning := ""
	if len(a.RelatedTo) == 0 || (len(a.RelatedTo) > 0 && len(skipped) == len(a.RelatedTo)) {
		orphanWarning = fmt.Sprintf("No connections were made. Call connect with domain=%s to link these memories. Suggested connections in other domains cannot be connected directly — check their domain field first.", node.Domain)
	}

	resp := struct {
		Node                 *db.Node            `json:"node"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		PossibleDuplicates   []db.Node           `json:"possible_duplicates"`
		SkippedConnections   []skippedConnection `json:"skipped_connections,omitempty"`
		OrphanWarning        string              `json:"orphan_warning,omitempty"`
	}{
		Node:                 node,
		SuggestedConnections: suggestions,
		PossibleDuplicates:   duplicates,
		SkippedConnections:   skipped,
		OrphanWarning:        orphanWarning,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) getNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	nwe, err := h.store.GetNode(a.ID)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nwe, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) searchNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Query  string `json:"query"`
		Domain string `json:"domain"`
		Limit  int    `json:"limit"`
		Exact  bool   `json:"exact"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	var nodes *db.SearchResult
	var err error
	if a.Exact {
		nodes, err = h.store.SearchNodesExact(a.Query, a.Domain, a.Limit)
	} else {
		nodes, err = h.store.SearchNodes(a.Query, a.Domain, a.Limit)
	}
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) recentChanges(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain        string `json:"domain"`
		Limit         int    `json:"limit"`
		GroupByDomain bool   `json:"group_by_domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	// group_by_domain only makes sense when no domain is specified.
	if a.GroupByDomain && a.Domain == "" {
		perDomain := a.Limit
		if perDomain <= 0 {
			perDomain = 5
		}
		// Fetch a broad slice of recent nodes across all domains, then group.
		// 1000 is a generous upper bound; real deployments are unlikely to exceed it.
		all, err := h.store.RecentChanges("", 1000)
		if err != nil {
			return nil, err
		}
		// Group by domain, preserving updated_at DESC order within each group.
		grouped := make(map[string][]db.Node)
		domainOrder := []string{} // track insertion order for stable output
		for _, n := range all {
			if _, seen := grouped[n.Domain]; !seen {
				domainOrder = append(domainOrder, n.Domain)
			}
			if len(grouped[n.Domain]) < perDomain {
				grouped[n.Domain] = append(grouped[n.Domain], n)
			}
		}
		// Build ordered result.
		type groupedResult struct {
			Domain string    `json:"domain"`
			Nodes  []db.Node `json:"nodes"`
		}
		out := make([]groupedResult, 0, len(domainOrder))
		for _, d := range domainOrder {
			out = append(out, groupedResult{Domain: d, Nodes: grouped[d]})
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	// Normal (non-grouped) behaviour.
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	nodes, err := h.store.RecentChanges(a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) findConnections(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		FromLabel string `json:"from_label"`
		ToLabel   string `json:"to_label"`
		Domain    string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	result, err := h.store.FindConnections(a.FromLabel, a.ToLabel, a.Domain)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) timeline(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain        string `json:"domain"`
		MemoryID      string `json:"memory_id"`
		Depth         int    `json:"depth"`
		ImportantOnly bool   `json:"important_only"`
		Tags          string `json:"tags"`
		From          string `json:"from"`
		To            string `json:"to"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	parseDate := func(s string) (*time.Time, error) {
		if s == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t, err = time.Parse("2006-01-02", s)
			if err != nil {
				return nil, fmt.Errorf("invalid date format, expected ISO8601: %s", s)
			}
		}
		return &t, nil
	}
	from, err := parseDate(a.From)
	if err != nil {
		return nil, err
	}
	to, err := parseDate(a.To)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, tag := range strings.Split(a.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	var nodes []db.Node
	if a.MemoryID != "" {
		if a.Depth <= 0 {
			a.Depth = 2
		}
		nodes, err = h.store.GetHistoryForMemoryID(a.MemoryID, a.Depth, a.ImportantOnly, tags, from, to, a.Limit)
	} else {
		nodes, err = h.store.Timeline(a.Domain, a.ImportantOnly, tags, from, to, a.Limit)
	}
	if err != nil {
		return errorResult(err.Error()), nil
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) addAlias(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Alias  string `json:"alias"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.AddAlias(a.Alias, a.Domain); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q → %q registered", a.Alias, a.Domain)}}}, nil
}

func (h *Handler) listAliases(_ json.RawMessage) (*ToolResult, error) {
	aliases, err := h.store.ListAliases()
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(aliases, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) removeAlias(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.RemoveAlias(a.Alias); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q removed", a.Alias)}}}, nil
}

func (h *Handler) resolveDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	canonical := h.store.ResolveAlias(a.Name)
	msg := fmt.Sprintf("%q resolves to %q", a.Name, canonical)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
}

func (h *Handler) forgetNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.ArchiveNode(a.ID, a.Reason); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q archived. It can be restored at any time with restore_node.", a.ID),
	}}}, nil
}

func (h *Handler) restoreNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.RestoreNode(a.ID); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q restored and is now visible in search and retrieval.", a.ID),
	}}}, nil
}

func (h *Handler) listArchived(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	nodes, err := h.store.ListArchived(a.Domain)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func errorResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentBlock{{Type: "text", Text: msg}},
	}
}

func (h *Handler) drift(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	candidates, err := h.store.FindDrift(a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(candidates, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) orientCrossDomain() (*ToolResult, error) {
	// Fetch a broad slice of recent nodes across all domains then group,
	// reusing the same logic as recentChanges(group_by_domain=true).
	all, err := h.store.RecentChanges("", 1000)
	if err != nil {
		return nil, err
	}

	type recentEntry struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		UpdatedAt string `json:"updated_at"`
	}
	type domainEntry struct {
		Domain string        `json:"domain"`
		Recent []recentEntry `json:"recent"`
	}

	const perDomain = 5
	grouped := make(map[string][]recentEntry)
	domainOrder := []string{}
	for _, n := range all {
		if _, seen := grouped[n.Domain]; !seen {
			domainOrder = append(domainOrder, n.Domain)
		}
		if len(grouped[n.Domain]) < perDomain {
			grouped[n.Domain] = append(grouped[n.Domain], recentEntry{
				ID:        n.ID,
				Label:     n.Label,
				UpdatedAt: n.UpdatedAt.Format(time.RFC3339),
			})
		}
	}

	domains := make([]domainEntry, 0, len(domainOrder))
	for _, d := range domainOrder {
		domains = append(domains, domainEntry{Domain: d, Recent: grouped[d]})
	}

	resp := struct {
		Mode    string        `json:"mode"`
		Domains []domainEntry `json:"domains"`
	}{
		Mode:    "cross_domain_snapshot",
		Domains: domains,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) orientWithTopic(domain, topic string) (*ToolResult, error) {
	liveNodes, err := h.store.CountNodes(domain)
	if err != nil {
		return nil, err
	}
	archivedNodes, err := h.store.CountArchived(domain)
	if err != nil {
		return nil, err
	}

	result, err := h.store.SearchNodes(topic, domain, 5)
	if err != nil {
		return nil, err
	}

	type leanEntry struct {
		ID         string  `json:"id"`
		Label      string  `json:"label"`
		WhyMatters string  `json:"why_matters,omitempty"`
		Truncated  bool    `json:"truncated,omitempty"`
		OccurredAt *string `json:"occurred_at,omitempty"`
	}
	toLean := func(n db.Node) leanEntry {
		why, truncated := truncateWhy(n.WhyMatters)
		e := leanEntry{ID: n.ID, Label: n.Label, WhyMatters: why, Truncated: truncated}
		if n.OccurredAt != nil {
			s := n.OccurredAt.Format("2006-01-02")
			e.OccurredAt = &s
		}
		return e
	}

	relevant := make([]leanEntry, len(result.Nodes))
	for i, nr := range result.Nodes {
		relevant[i] = toLean(nr.Node)
	}

	spineNodes, err := h.store.Timeline(domain, true, nil, nil, nil, 20)
	if err != nil {
		return nil, err
	}
	spineEntries := make([]leanEntry, len(spineNodes))
	for i, n := range spineNodes {
		spineEntries[i] = toLean(n)
	}

	recent, err := h.store.RecentChanges(domain, 5)
	if err != nil {
		return nil, err
	}
	recentEntries := make([]leanEntry, len(recent))
	for i, n := range recent {
		recentEntries[i] = toLean(n)
	}

	resp := struct {
		SummaryHint   string      `json:"summary_hint"`
		ServerVersion string      `json:"server_version"`
		LiveNodes     int         `json:"live_nodes"`
		ArchivedNodes int         `json:"archived_nodes"`
		DeclaredSpine interface{} `json:"declared_spine"`
		Relevant      interface{} `json:"relevant"`
		Recent        interface{} `json:"recent"`
	}{
		SummaryHint:   "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. relevant lists memories most similar to the supplied topic. declared_spine lists key decisions chronologically. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion: h.version,
		LiveNodes:     liveNodes,
		ArchivedNodes: archivedNodes,
		DeclaredSpine: spineEntries,
		Relevant:      relevant,
		Recent:        recentEntries,
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func truncateWhy(s string) (string, bool) {
	const limit = 150
	if len(s) <= limit {
		return s, false
	}
	sub := s[:limit]
	lastBoundary := -1
	for i := 0; i < len(sub); i++ {
		if sub[i] == '.' || sub[i] == '!' || sub[i] == '?' {
			next := i + 1
			if next >= len(sub) || sub[next] == ' ' || sub[next] == '\n' || sub[next] == '\t' {
				lastBoundary = next
			}
		}
	}
	if lastBoundary > 0 {
		return strings.TrimRight(s[:lastBoundary], " \t\n"), true
	}
	return sub + "...", true
}

func (h *Handler) summariseDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		Topic  string `json:"topic"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	if a.Domain == "" {
		return h.orientCrossDomain()
	}

	if a.Topic != "" {
		return h.orientWithTopic(a.Domain, a.Topic)
	}

	// Step 1: count live and archived nodes for the domain.
	liveNodes, err := h.store.CountNodes(a.Domain)
	if err != nil {
		return nil, err
	}
	if liveNodes == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "Nothing has been filed for this domain yet."}}}, nil
	}
	archivedNodes, err := h.store.CountArchived(a.Domain)
	if err != nil {
		return nil, err
	}

	// Step 2: fetch significant nodes (structurally load-bearing, recency-weighted inbound degree).
	sigResult, err := h.store.GetSignificance(a.Domain, 10, 90, nil)
	if err != nil {
		return nil, err
	}

	// Step 3: fetch recent changes — capped at 5.
	recent, err := h.store.RecentChanges(a.Domain, 5)
	if err != nil {
		return nil, err
	}

	// Step 4: fetch declared decision spine (nodes with occurred_at set, chronological).
	spineNodes, err := h.store.Timeline(a.Domain, true, nil, nil, nil, 20)
	if err != nil {
		return nil, err
	}

	// Step 5: build lean response — id, label, truncated why_matters only; no description.
	type leanEntry struct {
		ID         string  `json:"id"`
		Label      string  `json:"label"`
		WhyMatters string  `json:"why_matters,omitempty"`
		Truncated  bool    `json:"truncated,omitempty"`
		OccurredAt *string `json:"occurred_at,omitempty"`
	}
	toLean := func(n db.Node) leanEntry {
		why, truncated := truncateWhy(n.WhyMatters)
		e := leanEntry{
			ID:         n.ID,
			Label:      n.Label,
			WhyMatters: why,
			Truncated:  truncated,
		}
		if n.OccurredAt != nil {
			s := n.OccurredAt.Format("2006-01-02")
			e.OccurredAt = &s
		}
		return e
	}

	type scoredEntry struct {
		leanEntry
		ImportanceScore float64 `json:"importance_score"`
	}

	recentEntries := make([]leanEntry, len(recent))
	for i, n := range recent {
		recentEntries[i] = toLean(n)
	}
	spineEntries := make([]leanEntry, len(spineNodes))
	for i, n := range spineNodes {
		spineEntries[i] = toLean(n)
	}
	sigEntries := make([]scoredEntry, len(sigResult.Structural))
	for i, sn := range sigResult.Structural {
		sigEntries[i] = scoredEntry{
			leanEntry:       toLean(sn.Node),
			ImportanceScore: sn.ImportanceScore,
		}
	}

	resp := struct {
		SummaryHint   string      `json:"summary_hint"`
		ServerVersion string      `json:"server_version"`
		LiveNodes     int         `json:"live_nodes"`
		ArchivedNodes int         `json:"archived_nodes"`
		DeclaredSpine interface{} `json:"declared_spine"`
		Significant   interface{} `json:"significant"`
		Recent        interface{} `json:"recent"`
	}{
		SummaryHint:   "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. The declared_spine lists the key decisions that shaped this domain, in chronological order — weigh these heavily when summarising. significant lists structurally load-bearing memories right now. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion: h.version,
		LiveNodes:     liveNodes,
		ArchivedNodes: archivedNodes,
		DeclaredSpine: spineEntries,
		Significant:   sigEntries,
		Recent:        recentEntries,
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// skippedConnection records a related_to ID that could not be connected and why.
type skippedConnection struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// processRelatedTo attempts to create edges for each entry in the related_to list.
// Entries that fail (node not found, etc.) are collected in the returned slice instead of silently dropped.
func processRelatedTo(h *Handler, fromID string, entries []json.RawMessage) []skippedConnection {
	var skipped []skippedConnection
	for _, raw := range entries {
		relID := ""
		relationship := "connects_to"

		var strID string
		if err := json.Unmarshal(raw, &strID); err == nil {
			relID = strID
		} else {
			var entry struct {
				ID           string `json:"id"`
				Relationship string `json:"relationship"`
			}
			if err := json.Unmarshal(raw, &entry); err == nil {
				relID = entry.ID
				if entry.Relationship != "" {
					relationship = entry.Relationship
				}
			}
		}

		if relID == "" {
			continue
		}
		if _, err := h.store.AddEdge(fromID, relID, relationship, "auto-linked at creation"); err != nil {
			reason := err.Error()
			skipped = append(skipped, skippedConnection{ID: relID, Reason: reason})
		}
	}
	return skipped
}

// addNodesBatch handles the batch mode of remember: items is the raw JSON array of node objects.
func (h *Handler) addNodesBatch(items json.RawMessage) (*ToolResult, error) {
	var nodeList []struct {
		Label       string            `json:"label"`
		Description string            `json:"description"`
		WhyMatters  string            `json:"why_matters"`
		Tags        string            `json:"tags"`
		Domain      string            `json:"domain"`
		OccurredAt  string            `json:"occurred_at"`
		Transient   bool              `json:"transient"`
		RelatedTo   []json.RawMessage `json:"related_to"`
	}
	if err := json.Unmarshal(items, &nodeList); err != nil {
		return nil, err
	}
	inputs := make([]db.NodeInput, len(nodeList))
	for i, n := range nodeList {
		var occurredAt *time.Time
		if n.OccurredAt != "" {
			t, err := time.Parse(time.RFC3339, n.OccurredAt)
			if err != nil {
				t, err = time.Parse("2006-01-02", n.OccurredAt)
				if err != nil {
					return nil, fmt.Errorf("node %d: invalid occurred_at: %s", i, n.OccurredAt)
				}
			}
			occurredAt = &t
		}
		if occurredAt != nil && n.WhyMatters == "" {
			return nil, fmt.Errorf("node %d: occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.", i)
		}
		inputs[i] = db.NodeInput{
			Label:       n.Label,
			Description: n.Description,
			WhyMatters:  n.WhyMatters,
			Tags:        n.Tags,
			Domain:      n.Domain,
			OccurredAt:  occurredAt,
			Transient:   n.Transient,
		}
	}
	nodes, err := h.store.AddNodesBatch(inputs)
	if err != nil {
		return nil, err
	}

	type entry struct {
		Node                 *db.Node            `json:"node"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		SkippedConnections   []skippedConnection `json:"skipped_connections,omitempty"`
	}
	result := make([]entry, len(nodes))
	anyConnected := false
	for i, n := range nodes {
		suggestions, _ := h.store.SuggestEdges(n.ID, 5)
		if suggestions == nil {
			suggestions = []db.EdgeSuggestion{}
		}
		skipped := processRelatedTo(h, n.ID, nodeList[i].RelatedTo)
		if len(nodeList[i].RelatedTo) > 0 && len(skipped) < len(nodeList[i].RelatedTo) {
			anyConnected = true
		}
		result[i] = entry{Node: n, SuggestedConnections: suggestions, SkippedConnections: skipped}
	}
	orphanWarning := ""
	if len(nodes) > 0 && !anyConnected {
		orphanWarning = "No connections were made. Call connect with domain=<domain> to link these memories. Suggested connections in other domains cannot be connected directly — check their domain field first."
	}

	type response struct {
		Nodes         []entry `json:"nodes"`
		OrphanWarning string  `json:"orphan_warning,omitempty"`
	}
	out := response{Nodes: result, OrphanWarning: orphanWarning}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// addNodes retains the old remember_all wire format for backward compat during transition (not exposed in ListTools).
func (h *Handler) addNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Nodes []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
			WhyMatters  string `json:"why_matters"`
			Tags        string `json:"tags"`
			Domain      string `json:"domain"`
			OccurredAt  string `json:"occurred_at"`
			Transient   bool   `json:"transient"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a.Nodes)
	if err != nil {
		return nil, err
	}
	return h.addNodesBatch(raw)
}

func (h *Handler) addEdge(args json.RawMessage) (*ToolResult, error) {
	// Peek for items field to decide mode.
	var peek struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(args, &peek); err != nil {
		return nil, err
	}
	if len(peek.Items) > 0 && string(peek.Items) != "null" {
		return h.addEdgesBatch(peek.Items)
	}

	// Detect retired parameter names before unmarshalling.
	if msg := detectLegacyEdgeKeys(args); msg != "" {
		return errorResult(msg), nil
	}

	var a struct {
		FromMemory   string `json:"from_memory"`
		ToMemory     string `json:"to_memory"`
		Relationship string `json:"relationship"`
		Narrative    string `json:"narrative"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	edge, err := h.store.AddEdge(a.FromMemory, a.ToMemory, a.Relationship, a.Narrative)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(edge, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// detectLegacyEdgeKeys inspects raw JSON for retired connect parameter names
// (from_node, to_node). Returns a non-empty error message if found.
func detectLegacyEdgeKeys(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	_, hasFromNode := m["from_node"]
	_, hasToNode := m["to_node"]
	_, hasFromMemory := m["from_memory"]
	_, hasToMemory := m["to_memory"]
	var bad []string
	if hasFromNode && !hasFromMemory {
		bad = append(bad, "'from_node'")
	}
	if hasToNode && !hasToMemory {
		bad = append(bad, "'to_node'")
	}
	if len(bad) == 0 {
		return ""
	}
	return "Unknown parameter " + strings.Join(bad, " and ") +
		". The connect tool uses 'from_memory' and 'to_memory'. Call tools/list to refresh your schema."
}

// addEdgesBatch handles the batch mode of connect: items is the raw JSON array of edge objects.
func (h *Handler) addEdgesBatch(items json.RawMessage) (*ToolResult, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(items, &rawItems); err != nil {
		return nil, err
	}
	// Check each item for retired parameter names before any DB work.
	for i, raw := range rawItems {
		if msg := detectLegacyEdgeKeys(raw); msg != "" {
			return errorResult(fmt.Sprintf("item %d: %s", i, msg)), nil
		}
	}
	var edgeList []struct {
		FromMemory   string `json:"from_memory"`
		ToMemory     string `json:"to_memory"`
		Relationship string `json:"relationship"`
		Narrative    string `json:"narrative"`
	}
	if err := json.Unmarshal(items, &edgeList); err != nil {
		return nil, err
	}
	inputs := make([]db.EdgeInput, len(edgeList))
	for i, e := range edgeList {
		inputs[i] = db.EdgeInput{
			FromNode:     e.FromMemory,
			ToNode:       e.ToMemory,
			Relationship: e.Relationship,
			Narrative:    e.Narrative,
		}
	}
	edges, err := h.store.AddEdgesBatch(inputs)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(map[string]int{"edges_created": len(edges)}, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) updateNode(args json.RawMessage) (*ToolResult, error) {
	// Peek for items field to decide mode.
	var peek struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(args, &peek); err != nil {
		return nil, err
	}
	if len(peek.Items) > 0 && string(peek.Items) != "null" {
		return h.updateNodesBatch(peek.Items)
	}

	var a struct {
		ID          string  `json:"id"`
		Label       *string `json:"label"`
		Description *string `json:"description"`
		WhyMatters  *string `json:"why_matters"`
		Tags        *string `json:"tags"`
		OccurredAt  *string `json:"occurred_at"`
		Transient   *bool   `json:"transient"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	var occurredAt *time.Time
	if a.OccurredAt != nil {
		t, err := time.Parse(time.RFC3339, *a.OccurredAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", *a.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("invalid occurred_at format, expected ISO8601 date or datetime: %s", *a.OccurredAt)
			}
		}
		occurredAt = &t
	}
	if occurredAt != nil {
		callHasWhyMatters := a.WhyMatters != nil && *a.WhyMatters != ""
		if !callHasWhyMatters {
			existing, err := h.store.GetNode(a.ID)
			if err != nil {
				return nil, err
			}
			if existing.Node.WhyMatters == "" {
				return nil, fmt.Errorf("occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.")
			}
		}
	}
	node, err := h.store.UpdateNode(a.ID, a.Label, a.Description, a.WhyMatters, a.Tags, occurredAt, a.Transient)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(node, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// updateNodesBatch handles the batch mode of revise: items is the raw JSON array of update objects.
func (h *Handler) updateNodesBatch(items json.RawMessage) (*ToolResult, error) {
	var updateList []struct {
		ID          string  `json:"id"`
		Label       *string `json:"label"`
		Description *string `json:"description"`
		WhyMatters  *string `json:"why_matters"`
		Tags        *string `json:"tags"`
		OccurredAt  *string `json:"occurred_at"`
		Transient   *bool   `json:"transient"`
	}
	if err := json.Unmarshal(items, &updateList); err != nil {
		return nil, err
	}
	inputs := make([]db.NodeUpdateInput, len(updateList))
	for i, u := range updateList {
		if u.ID == "" {
			return nil, fmt.Errorf("update %d: id is required", i)
		}
		var occurredAt *time.Time
		if u.OccurredAt != nil {
			t, err := time.Parse(time.RFC3339, *u.OccurredAt)
			if err != nil {
				t, err = time.Parse("2006-01-02", *u.OccurredAt)
				if err != nil {
					return nil, fmt.Errorf("update %d: invalid occurred_at format: %s", i, *u.OccurredAt)
				}
			}
			occurredAt = &t
		}
		if occurredAt != nil {
			callHasWhyMatters := u.WhyMatters != nil && *u.WhyMatters != ""
			if !callHasWhyMatters {
				existing, err := h.store.GetNode(u.ID)
				if err != nil {
					return nil, fmt.Errorf("update %d: %w", i, err)
				}
				if existing.Node.WhyMatters == "" {
					return nil, fmt.Errorf("update %d: occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.", i)
				}
			}
		}
		inputs[i] = db.NodeUpdateInput{
			ID:          u.ID,
			Label:       u.Label,
			Description: u.Description,
			WhyMatters:  u.WhyMatters,
			Tags:        u.Tags,
			OccurredAt:  occurredAt,
			Transient:   u.Transient,
		}
	}
	nodes, err := h.store.UpdateNodesBatch(inputs)
	if err != nil {
		return nil, err
	}
	resp := struct {
		Updated []*db.Node `json:"updated"`
	}{Updated: nodes}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// updateNodes retains the old revise_all wire format for backward compat during transition (not exposed in ListTools).
func (h *Handler) updateNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Updates []struct {
			ID          string  `json:"id"`
			Label       *string `json:"label"`
			Description *string `json:"description"`
			WhyMatters  *string `json:"why_matters"`
			Tags        *string `json:"tags"`
			OccurredAt  *string `json:"occurred_at"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a.Updates)
	if err != nil {
		return nil, err
	}
	return h.updateNodesBatch(raw)
}

func (h *Handler) suggestEdges(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if a.Limit <= 0 {
		a.Limit = 5
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	suggestions, err := h.store.SuggestEdges(a.ID, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(suggestions, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) listDomains(_ json.RawMessage) (*ToolResult, error) {
	domains, err := h.store.ListDomains()
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(domains, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) disconnect(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.store.DeleteEdge(a.ID); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Connection %q removed.", a.ID)}}}, nil
}

func (h *Handler) findDisconnected(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	nodes, err := h.store.FindDisconnected(a.Domain)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "No disconnected memories found."}}}, nil
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) tracePath(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.FromID == "" || a.ToID == "" {
		return nil, fmt.Errorf("from_id and to_id are required")
	}
	result, err := h.store.FindPath(a.FromID, a.ToID, 6)
	if err != nil {
		return nil, err
	}
	if len(result.Path) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("No path found between %q and %q within 6 hops.", a.FromID, a.ToID)}}}, nil
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// sanitiseMermaidLabel truncates to 40 runes and escapes characters that break
// Mermaid node label syntax (double-quotes, newlines).
func sanitiseMermaidLabel(s string) string {
	runes := []rune(s)
	if len(runes) > 40 {
		s = string(runes[:37]) + "..."
	} else {
		s = string(runes)
	}
	s = strings.ReplaceAll(s, "\"", "#quot;")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (h *Handler) visualise(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain   string `json:"domain"`
		MemoryID string `json:"memory_id"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	var nodes []db.Node
	var edges []db.Edge
	var truncated bool
	var nodesTotal, edgesTotal int

	switch {
	case a.MemoryID != "":
		var err error
		nodes, edges, err = h.store.GetNodeNeighbourhood(a.MemoryID)
		if err != nil {
			return &ToolResult{IsError: true, Content: []ContentBlock{{Type: "text", Text: err.Error()}}}, nil
		}
	case a.Domain != "":
		if a.Limit <= 0 {
			a.Limit = 40
		}
		var err error
		nodes, edges, truncated, nodesTotal, edgesTotal, err = h.store.GetDomainGraph(a.Domain, a.Limit)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return &ToolResult{Content: []ContentBlock{{Type: "text", Text: `{"error":"no content found for domain"}`}}}, nil
		}
	default:
		return nil, fmt.Errorf("domain or memory_id is required")
	}

	// Build positional alias map (n0, n1, …) so Mermaid source stays readable.
	idMap := make(map[string]string, len(nodes))
	for i, n := range nodes {
		idMap[n.ID] = fmt.Sprintf("n%d", i)
	}

	var sb strings.Builder
	sb.WriteString("flowchart TD\n")
	for i, n := range nodes {
		label := sanitiseMermaidLabel(n.Label)
		fmt.Fprintf(&sb, "  n%d[\"%s\"]\n", i, label)
	}
	for _, e := range edges {
		from, ok1 := idMap[e.FromNode]
		to, ok2 := idMap[e.ToNode]
		if !ok1 || !ok2 {
			continue
		}
		fmt.Fprintf(&sb, "  %s -- \"%s\" --> %s\n", from, e.Relationship, to)
	}

	// Structured node/edge data for rich renderers — full labels, real IDs.
	type nodeEntry struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	type edgeEntry struct {
		From         string `json:"from"`
		To           string `json:"to"`
		Relationship string `json:"relationship"`
	}
	nodeList := make([]nodeEntry, len(nodes))
	for i, n := range nodes {
		nodeList[i] = nodeEntry{ID: n.ID, Label: n.Label}
	}
	edgeList := make([]edgeEntry, 0, len(edges))
	for _, e := range edges {
		if _, ok1 := idMap[e.FromNode]; !ok1 {
			continue
		}
		if _, ok2 := idMap[e.ToNode]; !ok2 {
			continue
		}
		edgeList = append(edgeList, edgeEntry{From: e.FromNode, To: e.ToNode, Relationship: e.Relationship})
	}

	result := struct {
		Mermaid    string      `json:"mermaid"`
		NodeCount  int         `json:"node_count"`
		NodesTotal int         `json:"nodes_total,omitempty"`
		EdgeCount  int         `json:"edge_count"`
		EdgesTotal int         `json:"edges_total,omitempty"`
		Truncated  bool        `json:"truncated,omitempty"`
		Nodes      []nodeEntry `json:"nodes"`
		Edges      []edgeEntry `json:"edges"`
	}{
		Mermaid:    sb.String(),
		NodeCount:  len(nodes),
		NodesTotal: nodesTotal,
		EdgeCount:  len(edges),
		EdgesTotal: edgesTotal,
		Truncated:  truncated,
		Nodes:      nodeList,
		Edges:      edgeList,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) checkForUpdates(_ json.RawMessage) (*ToolResult, error) {
	info := func(msg string) *ToolResult {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}
	}

	if h.checkUpdate == nil {
		return info("update check not available"), nil
	}
	if h.version == "dev" {
		return info("running dev build — skipping update check"), nil
	}
	latest, err := h.checkUpdate()
	if err != nil {
		return info(fmt.Sprintf("could not reach update server: %v", err)), nil
	}
	if latest == h.version {
		return info(fmt.Sprintf("memoryweb is up to date (%s)", h.version)), nil
	}
	return info(fmt.Sprintf(
		"memoryweb %s is available (you are running %s). "+
			"To update, download the binary for your platform from "+
			"https://github.com/corbym/memoryweb/releases/latest and replace "+
			"the existing binary, then restart your MCP client.",
		latest, h.version,
	)), nil
}

func (h *Handler) renameDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		OldDomain string `json:"old_domain"`
		NewDomain string `json:"new_domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.OldDomain == "" || a.NewDomain == "" {
		return errorResult("old_domain and new_domain are required"), nil
	}
	result, err := h.store.RenameDomain(a.OldDomain, a.NewDomain)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	out := map[string]interface{}{
		"nodes_renamed": result.NodesRenamed,
		"alias_created": result.OldDomain + " → " + result.NewDomain,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// auditTool dispatches mode=stale to drift, mode=orphans to findDisconnected,
// and mode=archived to listArchived.
func (h *Handler) auditTool(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	switch a.Mode {
	case "stale":
		return h.drift(args)
	case "orphans":
		return h.findDisconnected(args)
	case "archived":
		return h.listArchived(args)
	default:
		return errorResult(fmt.Sprintf("unknown audit mode %q — use stale, orphans, or archived", a.Mode)), nil
	}
}

// domainsTool returns a combined response with both the domain list and alias list.
func (h *Handler) domainsTool(_ json.RawMessage) (*ToolResult, error) {
	domains, err := h.store.ListDomains()
	if err != nil {
		return nil, err
	}
	aliases, err := h.store.ListAliases()
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"domains": domains,
		"aliases": aliases,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// aliasTool dispatches on action: add, remove, resolve, or list.
func (h *Handler) aliasTool(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Action string `json:"action"`
		Alias  string `json:"alias"`
		Domain string `json:"domain"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	switch a.Action {
	case "add":
		return h.addAlias(args)
	case "remove":
		return h.removeAlias(args)
	case "resolve":
		return h.resolveDomain(args)
	case "list":
		return h.listAliases(args)
	default:
		return errorResult(fmt.Sprintf("unknown alias action %q — use add, remove, resolve, or list", a.Action)), nil
	}
}

// forgetAll archives multiple nodes in a single atomic transaction.
// If any ID is not found, the transaction is rolled back and no nodes are archived.
func (h *Handler) forgetAll(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Items []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if len(a.Items) == 0 {
		return errorResult("items is required and must not be empty"), nil
	}
	batch := make([]struct{ ID, Reason string }, len(a.Items))
	for i, item := range a.Items {
		if item.ID == "" {
			return errorResult(fmt.Sprintf("item %d is missing id", i)), nil
		}
		batch[i] = struct{ ID, Reason string }{ID: item.ID, Reason: item.Reason}
	}
	if err := h.store.ArchiveNodesBatch(batch); err != nil {
		return errorResult(err.Error()), nil
	}
	ids := make([]string, len(a.Items))
	for i, item := range a.Items {
		ids[i] = item.ID
	}
	msg := fmt.Sprintf("archived %d memories: %s\nAll nodes can be restored at any time with restore.", len(ids), strings.Join(ids, ", "))
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
}

func (h *Handler) handleSignificance(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain        string `json:"domain"`
		MemoryID      string `json:"memory_id"`
		Depth         int    `json:"depth"`
		Limit         int    `json:"limit"`
		RecencyWindow int    `json:"recency_window"`
		Tags          string `json:"tags"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Domain == "" && a.MemoryID == "" {
		return errorResult("domain or memory_id is required"), nil
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.RecencyWindow <= 0 {
		a.RecencyWindow = 90
	}

	var tags []string
	for _, tag := range strings.Split(a.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}

	var res db.SignificanceResult
	var err error
	if a.MemoryID != "" {
		if a.Depth <= 0 {
			a.Depth = 2
		}
		res, err = h.store.GetSignificanceForMemoryID(a.MemoryID, a.Depth, a.RecencyWindow)
	} else {
		res, err = h.store.GetSignificance(a.Domain, a.Limit, a.RecencyWindow, tags)
	}
	if err != nil {
		return errorResult(err.Error()), nil
	}

	out, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}
