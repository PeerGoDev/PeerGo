package catalog

import "regexp"

var catalogIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// validCatalogID is shared by category administration and user-owned catalog
// relationships. UUID text and legacy-compatible slugs both satisfy the
// public catalog contract; raw numeric Tracker IDs do not cross this boundary.
func validCatalogID(value string) bool {
	return catalogIDPattern.MatchString(value)
}
