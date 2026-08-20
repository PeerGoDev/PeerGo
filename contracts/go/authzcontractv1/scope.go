// Package authzcontractv1 contains the persisted authorization identifiers
// shared by Core and finite cross-database migration tools. Business policy
// and authorization evaluation remain owned by Core.
package authzcontractv1

const (
	// SiteScopeType is the persisted scope kind for site-wide grants.
	SiteScopeType = "site"
	// SiteScopeID is the single canonical PeerGo site scope. Persisting a
	// different identifier makes a grant intentionally fail closed.
	SiteScopeID = "peergo"
)
