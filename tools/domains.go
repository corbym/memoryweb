package tools

import (
	"encoding/json"
	"fmt"
)

// aliasTool dispatches on action: add, remove, resolve, or list.
func (h *Handler) aliasTool(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Action string `json:"action"`
		Alias  string `json:"alias"`
		Domain string `json:"domain"`
		Name   string `json:"name"`
	}
	if err := decodeParams(args, &a, "alias"); err != nil {
		return nil, err
	}
	switch a.Action {
	case "add":
		if err := h.store.AddAlias(a.Alias, a.Domain); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q → %q registered", a.Alias, a.Domain)}}}, nil
	case "remove":
		if err := h.store.RemoveAlias(a.Alias); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q removed", a.Alias)}}}, nil
	case "resolve":
		canonical := h.store.ResolveAlias(a.Name)
		msg := fmt.Sprintf("%q resolves to %q", a.Name, canonical)
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
	case "list":
		aliases, err := h.store.ListAliases()
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(aliases, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	default:
		return errorResult(fmt.Sprintf("unknown alias action %q — use add, remove, resolve, or list", a.Action)), nil
	}
}

func (h *Handler) listDomains(args json.RawMessage) (*ToolResult, error) {
	if err := rejectExtraArgs(args, "domains"); err != nil {
		return nil, err
	}
	domains, err := h.store.ListDomains()
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(domains, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// domainsTool returns a combined response with both the domain list and alias list.
func (h *Handler) domainsTool(args json.RawMessage) (*ToolResult, error) {
	if err := rejectExtraArgs(args, "domains"); err != nil {
		return nil, err
	}
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

func (h *Handler) renameDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		OldDomain string `json:"old_domain"`
		NewDomain string `json:"new_domain"`
	}
	if err := decodeParams(args, &a, "rename_domain"); err != nil {
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
