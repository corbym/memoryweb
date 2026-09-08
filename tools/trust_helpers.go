package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
)

const orientTrustRecencyWindow = 90

// annotateTrustDeltas appends a worsening hint ("↓ since last orient") to the
// Trust field of entries that are currently low-trust but had a positive score the
// last time trust was logged. At most maxDeltas entries are annotated to cap token
// overhead. Only entries already marked low-trust (Trust != "") are candidates.
// Non-blocking: store failures are silently swallowed so orient never fails due
// to delta annotation. The signature returns no error to make this intent explicit
// and prevent callers from accidentally propagating store errors.
func (hnd *Handler) annotateTrustDeltas(entries []scoredLeanEntry, maxDeltas int) []scoredLeanEntry {
	var ids []string
	for _, entry := range entries {
		if entry.Trust != "" {
			ids = append(ids, entry.ID)
		}
	}
	if len(ids) == 0 {
		return entries
	}
	prior, err := hnd.store.LastTrustScores(ids)
	if err != nil {
		return entries // non-fatal: skip delta annotations
	}
	const worseningThreshold = 0.3
	annotated := 0
	for i, entry := range entries {
		if annotated >= maxDeltas {
			break
		}
		if entry.Trust == "" {
			continue
		}
		if score, ok := prior[entry.ID]; ok && score > worseningThreshold {
			entries[i].Trust += "; ↓ since last orient"
			annotated++
		}
	}
	return entries
}

func (hnd *Handler) annotateSignificantTrust(entries []scoredLeanEntry) ([]scoredLeanEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	assessments, err := hnd.store.AssessTrustForNodeIDs(ids, orientTrustRecencyWindow)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		assessment, ok := assessments[entry.ID]
		if !ok || !assessment.IsLowTrust {
			continue
		}
		entries[i].Trust = "low — " + assessment.TrustBasis
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

func (hnd *Handler) trustNudgeForDependencies(depIDs []string, excludeInboundFrom string) (string, error) {
	depIDs = uniqueIDs(depIDs)
	if len(depIDs) == 0 {
		return "", nil
	}
	assessments, err := hnd.store.AssessTrustForNodeIDs(depIDs, orientTrustRecencyWindow, excludeInboundFrom)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, id := range depIDs {
		assessment, ok := assessments[id]
		if !ok || !assessment.IsLowTrust {
			continue
		}
		parts = append(parts, fmt.Sprintf("memory %s (%s)", id, assessment.TrustBasis))
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
	for _, edge := range edges {
		if edge.FromNode != nodeID {
			continue
		}
		switch edge.Relationship {
		case "connects_to", "depends_on", "caused_by", "blocked_by":
			ids = append(ids, edge.ToNode)
		}
	}
	return ids
}
