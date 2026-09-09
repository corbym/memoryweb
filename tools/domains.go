package tools

import (
	"encoding/json"
	"fmt"
)

func (hnd *Handler) domainsTool(args json.RawMessage) (*ToolResult, error) {
	args = argsOrEmptyObject(args)
	var params struct {
		Action    string `json:"action"`
		Alias     string `json:"alias"`
		Domain    string `json:"domain"`
		Name      string `json:"name"`
		OldDomain string `json:"old_domain"`
		NewDomain string `json:"new_domain"`
	}
	if err := decodeParams(args, &params, "domains"); err != nil {
		return nil, err
	}
	action := params.Action
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		return hnd.domainsList()
	case "add_alias":
		if params.Alias == "" || params.Domain == "" {
			return errorResult("alias and domain are required for action=add_alias"), nil
		}
		if err := hnd.store.AddAlias(params.Alias, params.Domain); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q → %q registered", params.Alias, params.Domain)}}}, nil
	case "remove_alias":
		if params.Alias == "" {
			return errorResult("alias is required for action=remove_alias"), nil
		}
		if err := hnd.store.RemoveAlias(params.Alias); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q removed", params.Alias)}}}, nil
	case "resolve":
		if params.Name == "" {
			return errorResult("name is required for action=resolve"), nil
		}
		canonical := hnd.store.ResolveAlias(params.Name)
		msg := fmt.Sprintf("%q resolves to %q", params.Name, canonical)
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
	case "rename":
		if params.OldDomain == "" || params.NewDomain == "" {
			return errorResult("old_domain and new_domain are required for action=rename"), nil
		}
		result, err := hnd.store.RenameDomain(params.OldDomain, params.NewDomain)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		out := map[string]interface{}{
			"nodes_renamed": result.NodesRenamed,
			"alias_created": result.OldDomain + " → " + result.NewDomain,
		}
		b, err := marshalResponseIndent(out)
		if err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
	default:
		return errorResult(fmt.Sprintf("unknown domains action %q — use list, add_alias, remove_alias, resolve, or rename", params.Action)), nil
	}
}

func (hnd *Handler) domainsList() (*ToolResult, error) {
	domains, err := hnd.store.ListDomains()
	if err != nil {
		return nil, err
	}
	aliases, err := hnd.store.ListAliases()
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"domains": domains,
		"aliases": aliases,
	}
	b, err := marshalResponseIndent(out)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}
