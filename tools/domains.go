package tools

import (
	"encoding/json"
	"fmt"
)

func (h *Handler) domainsTool(args json.RawMessage) (*ToolResult, error) {
	args = argsOrEmptyObject(args)
	var a struct {
		Action    string `json:"action"`
		Alias     string `json:"alias"`
		Domain    string `json:"domain"`
		Name      string `json:"name"`
		OldDomain string `json:"old_domain"`
		NewDomain string `json:"new_domain"`
	}
	if err := decodeParams(args, &a, "domains"); err != nil {
		return nil, err
	}
	action := a.Action
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		return h.domainsList()
	case "add_alias":
		if a.Alias == "" || a.Domain == "" {
			return errorResult("alias and domain are required for action=add_alias"), nil
		}
		if err := h.store.AddAlias(a.Alias, a.Domain); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q → %q registered", a.Alias, a.Domain)}}}, nil
	case "remove_alias":
		if a.Alias == "" {
			return errorResult("alias is required for action=remove_alias"), nil
		}
		if err := h.store.RemoveAlias(a.Alias); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q removed", a.Alias)}}}, nil
	case "resolve":
		if a.Name == "" {
			return errorResult("name is required for action=resolve"), nil
		}
		canonical := h.store.ResolveAlias(a.Name)
		msg := fmt.Sprintf("%q resolves to %q", a.Name, canonical)
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
	case "rename":
		if a.OldDomain == "" || a.NewDomain == "" {
			return errorResult("old_domain and new_domain are required for action=rename"), nil
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
	default:
		return errorResult(fmt.Sprintf("unknown domains action %q — use list, add_alias, remove_alias, resolve, or rename", a.Action)), nil
	}
}

func (h *Handler) domainsList() (*ToolResult, error) {
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
