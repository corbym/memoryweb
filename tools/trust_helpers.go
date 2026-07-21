package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
)

const orientTrustRecencyWindow = 90

func (h *Handler) annotateSignificantTrust(entries []scoredLeanEntry) ([]scoredLeanEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	assessments, err := h.store.AssessTrustForNodeIDs(ids, orientTrustRecencyWindow)
	if err != nil {
		return nil, err
	}
	for i, e := range entries {
		a, ok := assessments[e.ID]
		if !ok || !a.IsLowTrust {
			continue
		}
		entries[i].Trust = "low — " + a.TrustBasis
	}
	return entries, nil
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (h *Handler) trustNudgeForDependencies(depIDs []string, excludeInboundFrom string) (string, error) {
	depIDs = uniqueIDs(depIDs)
	if len(depIDs) == 0 {
		return "", nil
	}
	assessments, err := h.store.AssessTrustForNodeIDs(depIDs, orientTrustRecencyWindow, excludeInboundFrom)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, id := range depIDs {
		a, ok := assessments[id]
		if !ok || !a.IsLowTrust {
			continue
		}
		parts = append(parts, fmt.Sprintf("memory %s (%s)", id, a.TrustBasis))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "You're depending on a low-trust " + strings.Join(parts, "; ") + ".", nil
}

func dependencyIDsFromRelatedTo(entries []json.RawMessage) []string {
	var ids []string
	for _, raw := range entries {
		relID := ""
		var strID string
		if err := json.Unmarshal(raw, &strID); err == nil {
			relID = strID
		} else {
			var entry struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &entry); err == nil {
				relID = entry.ID
			}
		}
		if relID != "" {
			ids = append(ids, relID)
		}
	}
	return ids
}

func outboundDependencyIDs(edges []db.Edge, nodeID string) []string {
	var ids []string
	for _, e := range edges {
		if e.FromNode != nodeID {
			continue
		}
		switch e.Relationship {
		case "depends_on", "caused_by", "blocked_by":
			ids = append(ids, e.ToNode)
		}
	}
	return ids
}
