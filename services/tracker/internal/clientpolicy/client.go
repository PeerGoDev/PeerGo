// Package clientpolicy recognises the curated Azureus-style peer fingerprints
// accepted by PeerGo. HTTP User-Agent and administrator-provided regexes are
// deliberately excluded because neither is a trustworthy protocol identity.
package clientpolicy

import (
	"strconv"
	"strings"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

type Identity struct {
	Family  trackerruntimepolicyv1.ClientFamily
	Version [4]int
}

func Allowed(policy trackerruntimepolicyv1.Policy, peerID [20]byte) bool {
	if policy.ClientMode == trackerruntimepolicyv1.ClientModeAllowAll {
		return true
	}
	identity, ok := Identify(peerID)
	if !ok {
		return false
	}
	for _, rule := range policy.AllowedClients {
		if rule.Family != identity.Family {
			continue
		}
		minimum, ok := parseMinimum(rule.MinVersion)
		return ok && compare(identity.Version, minimum) >= 0
	}
	return false
}

func Identify(peerID [20]byte) (Identity, bool) {
	if peerID[0] != '-' || peerID[7] != '-' {
		return Identity{}, false
	}
	var family trackerruntimepolicyv1.ClientFamily
	switch string(peerID[1:3]) {
	case "qB":
		family = trackerruntimepolicyv1.ClientFamilyQBittorrent
	case "TR":
		family = trackerruntimepolicyv1.ClientFamilyTransmission
	case "DE":
		family = trackerruntimepolicyv1.ClientFamilyDeluge
	case "LT":
		family = trackerruntimepolicyv1.ClientFamilyLibtorrent
	case "UT":
		family = trackerruntimepolicyv1.ClientFamilyUTorrent
	default:
		return Identity{}, false
	}
	identity := Identity{Family: family}
	for index, value := range peerID[3:7] {
		decoded, ok := base36(value)
		if !ok {
			return Identity{}, false
		}
		identity.Version[index] = decoded
	}
	return identity, true
}

func parseMinimum(value string) ([4]int, bool) {
	var version [4]int
	if value == "" {
		return version, true
	}
	components := strings.Split(value, ".")
	if len(components) < 2 || len(components) > len(version) {
		return version, false
	}
	for index, component := range components {
		parsed, err := strconv.Atoi(component)
		if err != nil || parsed < 0 || parsed > 35 {
			return [4]int{}, false
		}
		version[index] = parsed
	}
	return version, true
}

func compare(left, right [4]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func base36(value byte) (int, bool) {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0'), true
	case value >= 'A' && value <= 'Z':
		return int(value-'A') + 10, true
	default:
		return 0, false
	}
}
