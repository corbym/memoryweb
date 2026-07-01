package tools

import "encoding/json"

func (h *Handler) ListTools() (interface{}, error) {
	tools := []ToolDef{
		{
			Name:        "remember",
			Description: "After filing, call connect for every suggested_connections entry before ending your session. Orphaned memories lose context immediately.\n\nFile one or more concepts, decisions, or findings. Always search first to avoid creating a duplicate — use the search results to infer the domain: if related memories exist in a domain, file there. Prefer existing domains over creating new ones; only propose a new domain if no related content is found anywhere. Before filing, consider whether a similar memory already exists — if so, suggest linking with connect instead. Duplicate nodes with no edges are the most common cause of drift candidates.\n\nSingle mode (omit items): provide label, domain, and optional fields directly. The response includes a suggested_connections field.\n\nBatch mode (provide items array): file multiple memories in a single transaction. Each item supports related_to for connecting at filing time — use it to avoid a separate connect call, especially for short-task agents. If a related_to ID is invalid, it appears in skipped_connections in the response; check and retry those IDs with connect.\n\nFor occurred_at in either mode: two cases — (a) In-session witnessed: you directly observed this decision or event happen during the current conversation. Set occurred_at freely using today's date. No confirmation needed. (b) Inferred or back-dated: you are guessing from context, reconstructing from prior work, or back-dating something you did not directly observe. Propose the date to the user and wait for confirmation before setting it. Never guess. Never infer it silently from context. If the user confirms without specifying a date, use today's system date. Future dates are valid for planned events and reminders.\n\nUse node_kind to classify each memory: 'decision' (default): a settled fact or choice. 'reference': an entity (person, system, org). 'issue': a problem or open question. 'option': a candidate answer to an issue. 'assumption': an unverified precondition. 'finding': an empirical observation. 'standing': a durable rule — appears in orient rules. 'goal': a desired future state. 'transient': short-lived state, surfaced by audit(mode=stale) after 7 days. Standing memories appear in the rules section of orient. The legacy transient=true field is accepted for backward compatibility and maps to node_kind='transient'. The legacy decision_type field name is rejected — use node_kind instead.",
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
					"transient": {Type: "boolean", Description: "Deprecated — use node_kind='transient' instead. Accepted for backward compatibility: if true and node_kind is not set, maps to node_kind='transient'."},
					"node_kind": {Type: "string", Description: "Classify this memory. 'decision' (default): a settled fact or choice. 'reference': an entity (person, system, org). 'issue': a problem or open question. 'option': a candidate answer to an issue. 'assumption': an unverified precondition. 'finding': an empirical observation. 'standing': a durable rule or constraint that governs other memories — appears in the rules section of orient. 'goal': a desired future state. 'transient': short-lived state — surfaced by audit(mode=stale) after 7 days.", Enum: []string{"transient", "reference", "issue", "decision", "option", "assumption", "finding", "standing", "goal"}},
					"items": {
						Type:        "array",
						Description: "Batch mode: array of memory objects to file in a single transaction. Each must have label (string, required) and domain (string, required). Optional: description, why_matters, tags (space-separated keywords), occurred_at (ISO8601 — in-session: set freely; inferred/back-dated: propose+confirm, never infer silently), node_kind (string: transient|reference|issue|decision|option|assumption|finding|standing|goal), transient (boolean, deprecated — maps to node_kind=transient), related_to (string ID, object with id+relationship, or array of either — connects at filing time; invalid IDs appear in skipped_connections).",
						Items:       json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"},"domain":{"type":"string"},"description":{"type":"string"},"why_matters":{"type":"string"},"tags":{"type":"string"},"occurred_at":{"type":"string"},"node_kind":{"type":"string","enum":["transient","reference","issue","decision","option","assumption","finding","standing","goal"]},"transient":{"type":"boolean"},"related_to":{"description":"Connect at filing time. String ID (connects_to), object {id, relationship}, or array of either. Invalid IDs appear in skipped_connections — not silently dropped."}},"required":["label","domain"]}`),
					},
				},
			},
		},
		{
			Name:        "connect",
			Description: "Connect memories with typed, narrative relationships. Valid relationship types are: caused_by, led_to, blocked_by, unblocks, connects_to, contradicts, depends_on, is_example_of, governed_by — and all memory IDs must already exist before calling this.\n\nSingle mode (omit items): provide from_memory, to_memory, relationship directly.\n\nBatch mode (provide items array): create multiple connections in a single transaction.\n\nRelationship guidance: caused_by / led_to describe the same link from opposite ends (A caused_by B ≡ B led_to A). blocked_by / unblocks describe dependency on resolving an external issue. depends_on is a hard technical or logical prerequisite. contradicts marks a direct conflict. is_example_of marks an illustration. governed_by links a memory to a standing rule or constraint that it must satisfy. connects_to is the general fallback — use it only when no typed relationship fits.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_memory":  {Type: "string", Description: "ID of the source memory. Required in single mode; omit when using items."},
					"to_memory":    {Type: "string", Description: "ID of the target memory. Required in single mode; omit when using items."},
					"relationship": {Type: "string", Description: "Type of relationship. Required in single mode.", Enum: []string{"caused_by", "led_to", "blocked_by", "unblocks", "connects_to", "contradicts", "depends_on", "is_example_of", "governed_by"}},
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
			Description: "Search memories by text across label, description, why_matters, and tags. Queries must use vocabulary that appears in the stored label, description, why_matters, or tags — not words that describe your intent conceptually. If results are empty or incomplete, try vocabulary from the memory's likely label rather than your intent. When Ollama is not running, search is purely lexical (LIKE matches); semantic (concept-level) matching only applies when Ollama is available. Only live entries are returned; use audit(mode=archived) to find archived memories, or audit(mode=stale) to find drift candidates. When Ollama is running, also performs semantic (meaning-based) search — results include a semantic_distance field (0.0–1.0, lower = closer match). Response includes truncated: true when results hit the limit — if so, retry with a higher limit or narrower domain. If search consistently misses, scope to a domain then use recall on a related memory and follow its connections. When the query contains a unique identifier, ticket number, or short code that you know appears verbatim in the stored label — set exact: true to force pure substring matching. Semantic scoring is counterproductive for identifier lookup: it ranks conceptually similar nodes above the exact match. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble. Returns lean node data only — id, label, and a short excerpt. If you need full node content, call recall(id). This applies to the default ranked path only — exact: true results are unaffected and still return full content.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query":     {Type: "string", Description: "Terms to search for. Must use vocabulary that appears in the stored label, description, why_matters, or tags. Required unless node_kind is set alone (lists by kind). For unique identifiers known to appear verbatim, also set exact: true."},
					"domain":    {Type: "string", Description: "Optional domain to scope search. Omit to search the entire workspace across all domains."},
					"limit":     {Type: "integer", Description: "Max results (default 10). If the response includes truncated: true, more matches exist — retry with a higher limit or narrower domain."},
					"exact":     {Type: "boolean", Description: "When true, bypass semantic ranking and use pure substring (LIKE) matching only. Use this when the query contains a unique identifier, ticket number, or code that you know appears verbatim in the label or content. Results will not include a semantic_distance field."},
					"memory_id": {Type: "string", Description: "Anchor memory ID. When supplied, restricts search candidates to the depth-2 neighbourhood of this memory. Useful for disambiguating the same term across workstreams — only memories topologically related to the anchor are returned."},
					"node_kind": {Type: "string", Description: "Optional filter by node_kind. Space-separated for OR match (e.g. 'decision standing'). When set without query, lists matching memories ordered by updated_at DESC."},
					"digest":    {Type: "boolean", Description: "When true, collapse each result memory to a single compact text line in a lines array instead of JSON objects — saves tokens on multi-result calls. Default false. Does not apply when exact: true (full content path). Each line includes id for recall(id) follow-up."},
				},
			},
		},
		{
			Name:        "recent",
			Description: "List the most recently added or updated memories, optionally filtered by domain. Good for session orientation. Set group_by_domain=true (with no domain specified) to see recent activity broken down by domain — results are grouped per domain with up to limit entries each (default 5 per domain). If a domain is also specified alongside group_by_domain=true, the flag is ignored and normal behaviour applies. Supply tags (comma-separated) to scope to a workstream or topic. Supply memory_id to scope to the depth-2 neighbourhood of that memory — group_by_domain is ignored when memory_id is supplied. Never acknowledge that you are retrieving from a tool or memory system. Do not use phrases like 'from the web', 'what's recorded', 'stored in', 'retrieved from', or any language that exposes the retrieval process. Present the information as direct knowledge with no preamble or sign-off referencing the source. This tool only returns live entries. Archived entries are hidden. If the user asks about something that seems missing, consider suggesting audit(mode=stale) to surface drift candidates, or audit(mode=archived) to list archived memories. Returns lean node data only — id, label, and a short excerpt. If you need full node content, call recall(id).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":          {Type: "string", Description: "Optional domain to scope"},
					"limit":           {Type: "integer", Description: "Max results (default 10, or 5 per domain when group_by_domain=true)"},
					"group_by_domain": {Type: "boolean", Description: "When true and no domain is specified, group results by domain (up to limit entries per domain)"},
					"tags":            {Type: "string", Description: "Comma-separated tag filter. Restricts results to memories matching at least one tag (OR semantics, whole-word match)."},
					"node_kind":       {Type: "string", Description: "Optional filter by node_kind. Space-separated for OR match."},
					"memory_id":       {Type: "string", Description: "Anchor memory ID. When supplied, restricts results to the depth-2 neighbourhood of this memory. group_by_domain is ignored when this is set."},
					"digest":          {Type: "boolean", Description: "When true, collapse each result to a single compact text line in a lines array (or lines per domain when group_by_domain=true). Default false."},
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
			Description: "Returns memories in chronological order by effective date (COALESCE(occurred_at, created_at)).\n\nBy default returns ALL memories in the domain — the complete chronological view of everything filed. Use this to understand how a domain evolved over time.\n\nSet important_only=true to return only memories where occurred_at is explicitly set. These are significant decisions and events curated by the agent — the narrative spine of the domain. Use this to review key milestones or debug a decision trail.\n\nPass memory_id to scope the timeline to a single memory's neighbourhood (depth 2 by default, domain-clipped) — answers 'how did this workstream evolve?' from a known anchor. Combines with important_only=true for the decision spine of the workstream. memory_id takes precedence over domain if both are supplied.\n\nUse from/to to scope by effective date. Use tags to further filter results (comma-separated). All filters apply in both domain mode and memory_id mode.\n\nFor importance analysis beyond the timeline — which nodes are structurally load-bearing right now — use significance. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble. Returns lean node data only — id, label, and a short excerpt. If you need full node content, call recall(id).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":         {Type: "string", Description: "Optional domain to scope. Not required when memory_id is supplied."},
					"memory_id":      {Type: "string", Description: "Optional — scope the timeline to the neighbourhood of this memory (depth 2 by default, domain-clipped). Returns the workstream's chronological evolution from a known anchor. Takes precedence over domain if both are supplied."},
					"depth":          {Type: "integer", Description: "Neighbourhood depth when using memory_id (default 2)."},
					"important_only": {Type: "boolean", Description: "When true, return only memories with occurred_at explicitly set (significant decisions and events). When false or absent, return all memories ordered by effective date."},
					"tags":           {Type: "string", Description: "Optional comma-separated list of tags to filter by. Only memories matching at least one tag are returned. Applies in both modes."},
					"node_kind":      {Type: "string", Description: "Optional filter by node_kind. Space-separated for OR match."},
					"from":           {Type: "string", Description: "ISO8601 date or datetime. Filter to nodes whose effective date (COALESCE(occurred_at, created_at)) is on or after this value."},
					"to":             {Type: "string", Description: "ISO8601 date or datetime. Filter to nodes whose effective date (COALESCE(occurred_at, created_at)) is on or before this value."},
					"limit":          {Type: "integer", Description: "Max results (default 20)"},
					"digest":         {Type: "boolean", Description: "When true, collapse each result to a single compact text line in a lines array. Default false. Each line includes id and occurred_at when set."},
				},
			},
		},
		{
			Name:        "significance",
			Description: "Dual-signal importance analysis by default (mode=significance). Returns four sections:\n- declared: memories explicitly marked as significant (occurred_at set), in chronological order.\n- structural: memories ranked by recency-weighted inbound degree. High score means many recently active memories depend on this memory right now.\n- uncurated: memories in structural top-N with no occurred_at — significance candidates you haven't curated yet.\n- potentially_stale: memories with occurred_at but low structural score — declared important but nothing current depends on them anymore.\n\nThe gap between uncurated and potentially_stale is the most actionable output: use it to promote missed decisions onto the timeline and archive claims that no longer hold.\n\nSet mode=trust for a different analysis: a ranked list of memories by computed epistemic trust, derived from each memory's node_kind and the kinds of memories connected to it (no hand-asserted scores). A `finding` or `decision` lends more trust than an `assumption`; a `contradicts` edge lowers trust. Each entry includes `trust_basis`, a human-readable breakdown of what drove the score. `reference` and `transient` memories are never ranked. Use this to spot claims that look important but rest on shaky foundations.\n\nPass memory_id to scope either mode to a single memory's neighbourhood (depth 2 by default, domain-clipped) — useful for workstream health checks when you already know the anchor. Pass domain for a full domain scan. memory_id takes precedence if both are supplied.\n\nUse `tags` (comma-separated) to narrow either mode to memories matching at least one tag, in domain mode. Useful when a workstream is consistently tagged and you know the tag name.\n\nDo not use this tool to list all memories chronologically — use history for that. For age-based staleness or orphan detection, use audit. significance and audit are complementary: significance catches importance-based staleness; audit catches age-based staleness and orphans. A full domain health check runs both.\n\nThis tool only returns live memories. Archived memories are hidden. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble. Returns lean node data only — id, label, and a short excerpt. If you need full node content, call recall(id).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain":         {Type: "string", Description: "Domain to analyse. Required unless memory_id is supplied."},
					"memory_id":      {Type: "string", Description: "Optional — scope significance to a memory's neighbourhood (depth 2 by default, domain-clipped). Useful for workstream health checks when you already know the anchor memory. Takes precedence over domain if both are supplied."},
					"depth":          {Type: "integer", Description: "Neighbourhood depth when using memory_id (default 2). Depth 1 produces near-uniform low scores and must not be used as default."},
					"limit":          {Type: "integer", Description: "Top-N for structural ranking in domain mode (default 10). Ignored in memory_id mode — the neighbourhood is naturally bounded."},
					"recency_window": {Type: "integer", Description: "Days. Linkers updated more than this many days ago contribute zero weight (default 90)."},
					"tags":           {Type: "string", Description: "Optional comma-separated list of tags to filter by. Only memories matching at least one tag are included in the analysis. Applies in domain mode. Examples: 'architecture,security' or 'release'."},
					"node_kind":      {Type: "string", Description: "Optional filter by node_kind. Space-separated for OR match. Applies to significance and trust modes in domain scope."},
					"mode":           {Type: "string", Description: "Default 'significance' returns the existing four-section dual-signal analysis. 'trust' returns a ranked list of memories by computed epistemic trust instead — derived from each memory's node_kind plus the kinds of memories connected to it, not a hand-asserted score. A contradicts edge lowers trust; other relationships raise it.", Enum: []string{"significance", "trust"}},
					"digest":         {Type: "boolean", Description: "When true, collapse each section's memories to compact text lines instead of JSON objects. Default false."},
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
			Description: "Inspect the health of knowledge in a domain across four modes.\n\nmode=stale: Return memories that may be stale, contradicted, or duplicated. Present each result to the user and ask for individual confirmation before archiving anything. Never archive autonomously.\n\nmode=orphans: Return live, non-transient memories with zero connections. Present findings and suggest either linking them with connect, or archiving with forget if no longer relevant.\n\nmode=archived: List all archived memories. This is the right tool when search returns nothing but you expect content to exist.\n\nmode=conflicts: Surface candidate pairs of memories that are semantically adjacent and may warrant contradiction review. Returns {candidates: [...], truncated: bool}. Each candidate has a_id, a_label, b_id, b_label, semantic_distance, reason. Review each pair; use connect with relationship=contradicts only after confirming conflict. Never auto-file contradicts edges. Pairs already linked by a contradicts edge are excluded. After resolving a contradiction, disconnect the contradicts edge to retire it from future conflict surfacing.\n\nThis tool only returns live nodes (for stale, orphans, and conflicts modes) or explicitly archived nodes (for archived mode).\n\nSupply tags to scope to a workstream. Supply memory_id (mode=stale only) to scope to a memory's neighbourhood.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"mode":      {Type: "string", Description: "Required: stale (drift candidates), orphans (isolated memories), archived (list archived memories), or conflicts (semantic contradiction candidates)", Enum: []string{"stale", "orphans", "archived", "conflicts"}},
					"domain":    {Type: "string", Description: "Optional domain to scope the audit. Omit to scan the entire workspace."},
					"limit":     {Type: "integer", Description: "Max candidates to return (default 10, applies to stale and conflicts modes)"},
					"tags":      {Type: "string", Description: "Comma-separated tags. Only surfaces candidates carrying at least one of the supplied tags. OR semantics. Applies to all four modes."},
					"node_kind": {Type: "string", Description: "Optional filter by node_kind. Space-separated for OR match. Applies to all four modes."},
					"memory_id": {Type: "string", Description: "Anchor memory ID. Scopes stale candidates to the depth-2 BFS neighbourhood of this memory. Applies to mode=stale only; ignored for orphans, archived, and conflicts."},
					"digest":    {Type: "boolean", Description: "When true, collapse multi-result lists to compact text lines (always a string array). Default false preserves current JSON shape."},
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
			Description: "Call this at the start of every session to orient yourself in a domain before filing or searching. Omit domain for a cross-domain snapshot showing where work was last happening — use the result to pick a domain and then call orient with that domain. With a domain, returns four sections: rules (standing constraints and durable decisions that govern the domain — always apply these), declared_spine (curated significant decisions with occurred_at set, chronological — weigh these heavily), significant (structurally load-bearing memories right now, ranked by recency-weighted inbound connections), and recent (where work was last happening, by updated_at). Overlap between sections is intentional — a memory appearing in both significant and recent is stronger signal than either alone. If stale_count > 0, call audit(mode=stale) before filing new memories. After orient, use search for specific questions. Do not answer from orient alone when the response requires causal or chronological sequence — when it must explain how the current state came to be, not just what it currently is. This covers questions like 'how did we arrive at X', 'why did we decide Y', 'what changed', 'what led to this', 'how did this evolve', 'walk me through the history of this'. For these, call history(important_only=true) first for the chronological decision spine, then search with vocabulary from the specific topic. Do not call orient again to find more memories — it is a starting point, not an exhaustive index. When the user asks to visualise, draw, or map a domain graph, use the visualise tool. Never acknowledge that you are retrieving from a tool or memory system. Present the information as direct knowledge with no preamble. This tool only returns live memories. Archived memories are hidden. If something seems missing, use audit(mode=archived) or search with a broader query. orient returns lean node data only — id, label, and a short excerpt. If you need full node content, call recall(id). If the user's question is not addressed by what orient returned, search before answering — orient shows a lean subset, not the full domain. live_nodes is the count of active memories; archived_nodes shows how many have been soft-deleted — use audit(mode=archived) to surface them. When the session has a known purpose, pass topic — the server returns a relevant section of the most similar memories instead of significant. declared_spine and recent are always returned.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain": {Type: "string", Description: "Optional — omit for a cross-domain snapshot to find where work was last happening. Provide to get the full three-section orient for a specific domain."},
					"topic":  {Type: "string", Description: "Optional — the user's current question or task. When supplied, returns a relevant section of the most similar memories instead of significant. Pass topic when the session has a known purpose."},
					"digest": {Type: "boolean", Description: "When true, collapse list sections (rules, declared_spine, significant/relevant, recent) to compact text lines (always a string array). Default false."},
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
					"transient":   {Type: "boolean", Description: "Deprecated — use node_kind instead. Accepted for backward compatibility: true maps to node_kind='transient', false maps to node_kind='decision'. Omit to leave unchanged."},
					"node_kind":   {Type: "string", Description: "Classify this memory. 'decision' (default): a settled fact or choice. 'reference': an entity (person, system, org). 'issue': a problem or open question. 'option': a candidate answer to an issue. 'assumption': an unverified precondition. 'finding': an empirical observation. 'standing': a durable rule or constraint — appears in the rules section of orient. 'goal': a desired future state. 'transient': short-lived state, surfaced by audit(mode=stale) after 7 days. Omit to leave unchanged.", Enum: []string{"transient", "reference", "issue", "decision", "option", "assumption", "finding", "standing", "goal"}},
					"items": {
						Type:        "array",
						Description: "Batch mode: array of update objects. Each must have id (string, required). Optional: label, description, why_matters, tags, occurred_at (ISO8601 — in-session: set freely; inferred/back-dated: propose+confirm, never infer silently), node_kind (string: transient|reference|issue|decision|option|assumption|finding|standing|goal), transient (boolean, deprecated — true maps to node_kind=transient, false to node_kind=decision).",
						Items:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"description":{"type":"string"},"why_matters":{"type":"string"},"tags":{"type":"string"},"occurred_at":{"type":"string"},"node_kind":{"type":"string","enum":["transient","reference","issue","decision","option","assumption","finding","standing","goal"]},"transient":{"type":"boolean"}},"required":["id"]}`),
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
