package tools

import (
	"encoding/json"
	"log"

	"github.com/corbym/memoryweb/db"
)

type rememberFilingExtras struct {
	TrustNudge        string
	PossibleMisdomain bool
	SuggestedDomain   string
	SuggestedMemoryID string
}

func (hnd *Handler) rememberFilingExtras(node *db.Node, relatedTo []json.RawMessage, excludeIDs []string) rememberFilingExtras {
	var out rememberFilingExtras
	nudge, err := hnd.trustNudgeForDependencies(dependencyIDsFromRelatedTo(relatedTo), node.ID)
	if err != nil {
		log.Printf("[memoryweb] trust nudge for %s: %v", node.ID, err)
	} else {
		out.TrustNudge = nudge
	}
	flagged, err := hnd.checkMisdomainCandidate(node, excludeIDs)
	if err != nil {
		log.Printf("[memoryweb] misdomain check for %s: %v", node.ID, err)
		return out
	}
	if flagged != nil {
		out.PossibleMisdomain = true
		out.SuggestedDomain = flagged.SuggestedDomain
		out.SuggestedMemoryID = flagged.SuggestedMemoryID
	}
	return out
}
