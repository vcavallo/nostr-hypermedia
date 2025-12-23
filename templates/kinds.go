package templates

import "nostr-server/templates/kinds"

// GetKindTemplates returns all kind templates concatenated.
// Templates are organized in the kinds/ subdirectory with one file per kind.
func GetKindTemplates() string {
	return kinds.GetAllTemplates()
}
