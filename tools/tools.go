package tools

import (
	"encoding/json"
	"fmt"
	"strings"

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
	"field before the session ends.\n\n" +
	"Never file operational credentials, connection strings, API keys, or tokens in memories."

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

func (hnd *Handler) CallTool(params json.RawMessage) (interface{}, error) {
	var req CallToolRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	var result interface{}
	var err error

	switch req.Name {
	case "remember":
		result, err = hnd.addNode(req.Arguments)
	case "connect":
		result, err = hnd.addEdge(req.Arguments)
	case "recall":
		result, err = hnd.getNode(req.Arguments)
	case "search":
		result, err = hnd.searchNodes(req.Arguments)
	case "recent":
		return errorResult("unknown tool: recent — use history with order=modified"), nil
	case "why_connected":
		result, err = hnd.findConnections(req.Arguments)
	case "history":
		result, err = hnd.timeline(req.Arguments)
	case "alias_domain":
		return errorResult("unknown tool: alias_domain — use domains with action=add_alias"), nil
	case "list_aliases":
		return errorResult("unknown tool: list_aliases — use domains"), nil
	case "remove_alias":
		return errorResult("unknown tool: remove_alias — use domains with action=remove_alias"), nil
	case "resolve_domain":
		return errorResult("unknown tool: resolve_domain — use domains with action=resolve"), nil
	case "forget":
		result, err = hnd.forgetNode(req.Arguments)
	case "restore":
		return errorResult("unknown tool: restore — use forget with restore=true"), nil
	case "forgotten":
		return errorResult("unknown tool: forgotten — use audit with mode=archived"), nil
	case "audit":
		result, err = hnd.auditTool(req.Arguments)
	case "whats_stale":
		return errorResult("unknown tool: whats_stale — use audit with mode=stale"), nil
	case "orient":
		result, err = hnd.summariseDomain(req.Arguments)
	case "remember_all":
		return errorResult("unknown tool: remember_all — use remember with an items array for batch filing"), nil
	case "connect_all":
		return errorResult("unknown tool: connect_all — use connect with an items array for batch connections"), nil
	case "revise":
		result, err = hnd.updateNode(req.Arguments)
	case "revise_all":
		return errorResult("unknown tool: revise_all — use revise with an items array for batch updates"), nil
	case "suggest_connections":
		result, err = hnd.suggestEdges(req.Arguments)
	case "domains":
		result, err = hnd.domainsTool(req.Arguments)
	case "list_domains":
		return errorResult("unknown tool: list_domains — use domains"), nil
	case "alias":
		return errorResult("unknown tool: alias — use domains with action=add_alias, remove_alias, or resolve"), nil
	case "disconnect":
		result, err = hnd.disconnect(req.Arguments)
	case "disconnected":
		return errorResult("unknown tool: disconnected — use audit with mode=orphans"), nil
	case "forget_all":
		result, err = hnd.forgetAll(req.Arguments)
	case "restore_all":
		result, err = hnd.restoreAll(req.Arguments)
	case "disconnect_all":
		result, err = hnd.disconnectAll(req.Arguments)
	case "trace":
		return errorResult("unknown tool: trace — use why_connected for direct edges between two IDs, or recall for neighbourhood context"), nil
	case "visualise":
		result, err = hnd.visualise(req.Arguments)
	case "rename_domain":
		return errorResult("unknown tool: rename_domain — use domains with action=rename"), nil
	case "significance":
		result, err = hnd.handleSignificance(req.Arguments)
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

func (hnd *Handler) getNode(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := decodeParams(args, &params, "recall", "id"); err != nil {
		return nil, err
	}
	nwe, err := hnd.store.GetNode(params.ID)
	if err != nil {
		return nil, err
	}
	b, err := marshalResponseIndent(nwe)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

func errorResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentBlock{{Type: "text", Text: msg}},
	}
}

// splitTrimmed returns trimmed, non-empty tokens from parts.
func splitTrimmed(parts []string) []string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitTags splits a comma-separated tags string into trimmed, non-empty tokens.
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return splitTrimmed(strings.Split(s, ","))
}

// splitNodeKinds splits a space-separated node_kind filter into tokens (OR match).
func splitNodeKinds(s string) []string {
	if s == "" {
		return nil
	}
	return splitTrimmed(strings.Fields(s))
}

func (hnd *Handler) checkForUpdates(_ json.RawMessage) (*ToolResult, error) {
	info := func(msg string) *ToolResult {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}
	}

	if hnd.checkUpdate == nil {
		return info("update check not available"), nil
	}
	if hnd.version == "dev" {
		return info("running dev build — skipping update check"), nil
	}
	latest, err := hnd.checkUpdate()
	if err != nil {
		return info(fmt.Sprintf("could not reach update server: %v", err)), nil
	}
	if latest == hnd.version {
		return info(fmt.Sprintf("memoryweb is up to date (%s)", hnd.version)), nil
	}
	return info(fmt.Sprintf(
		"memoryweb %s is available (you are running %s). "+
			"To update, download the binary for your platform from "+
			"https://github.com/corbym/memoryweb/releases/latest and replace "+
			"the existing binary, then restart your MCP client.",
		latest, hnd.version,
	)), nil
}
