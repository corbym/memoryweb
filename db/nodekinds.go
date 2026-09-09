package db

// Valid node kinds, mirroring the taxonomy exposed through the MCP tool
// definitions (tools/definitions.go). The database rejects anything outside
// this set at insert time so an invalid kind can never be stored silently.
var ValidNodeKinds = []string{
	"transient", "reference", "issue", "decision",
	"option", "assumption", "finding", "standing", "goal",
}

func isValidNodeKind(kind string) bool {
	for _, k := range ValidNodeKinds {
		if k == kind {
			return true
		}
	}
	return false
}
