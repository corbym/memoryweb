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

func (hnd *Handler) rememberFilingExtras(node *db.Node, relatedTo []json.RawMessage, domainExisted bool) rememberFilingExtras {
	var out rememberFilingExtras
	nudge, err := hnd.trustNudgeForDependencies(dependencyIDsFromRelatedTo(relatedTo), node.ID)
	if err != nil {
		log.Printf("[memoryweb] trust nudge for %s: %v", node.ID, err)
	} else {
		out.TrustNudge = nudge
	}
	if domainExisted {
		return out
	}
	flagged, err := hnd.checkNewDomainMisdomain(node)
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

func (hnd *Handler) snapshotDomainExistence(domains []string) (map[string]bool, error) {
	snap := make(map[string]bool, len(domains))
	for _, domain := range domains {
		resolved := hnd.store.ResolveAlias(domain)
		if _, seen := snap[resolved]; seen {
			continue
		}
		exists, err := hnd.store.DomainExists(domain)
		if err != nil {
			return nil, err
		}
		snap[resolved] = exists
	}
	return snap, nil
}
