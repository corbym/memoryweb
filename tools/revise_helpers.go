package tools

import "github.com/corbym/memoryweb/db"

func reviseContentTouched(existing db.Node, label, description, whyMatters, nodeKind *string) bool {
	if label != nil && *label != existing.Label {
		return true
	}
	if description != nil && *description != existing.Description {
		return true
	}
	if whyMatters != nil && *whyMatters != existing.WhyMatters {
		return true
	}
	if nodeKind != nil && *nodeKind != existing.NodeKind {
		return true
	}
	return false
}
