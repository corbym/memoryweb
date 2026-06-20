package tools

import (
	"encoding/json"
	"fmt"
)

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

func (h *Handler) listDomains(_ json.RawMessage) (*ToolResult, error) {
	domains, err := h.store.ListDomains()
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(domains, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
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
