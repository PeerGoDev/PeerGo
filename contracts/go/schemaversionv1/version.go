// Package schemaversionv1 defines database schema versions that form part of
// a cross-service operational contract.
//
// Keep service-private schema details inside each service. Only versions that
// another service must verify during an atomic operation, such as the legacy
// cutover, belong here.
package schemaversionv1

const (
	// Core is the latest Core schema required by both Core and legacy cutover tools.
	Core int64 = 202608210005

	// PrivacyVault is the latest Privacy Vault schema required by the legacy cutover.
	PrivacyVault int64 = 202608150001
)
