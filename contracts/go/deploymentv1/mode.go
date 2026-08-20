// Package deploymentv1 defines the deployment boundary shared by PeerGo
// processes. Cluster remains the fail-closed default; single-server must be
// selected explicitly before a process may use approved container-local
// clear-text transports.
package deploymentv1

import (
	"fmt"
	"os"
	"strings"
)

type Mode string

const (
	Cluster      Mode = "cluster"
	SingleServer Mode = "single-server"
)

// Parse treats an omitted mode as cluster so existing production deployments
// retain their TLS requirements without configuration changes.
func Parse(value string) (Mode, error) {
	switch Mode(strings.TrimSpace(value)) {
	case "", Cluster:
		return Cluster, nil
	case SingleServer:
		return SingleServer, nil
	default:
		return "", fmt.Errorf("PEERGO_DEPLOYMENT_MODE must be %q or %q", Cluster, SingleServer)
	}
}

func Load() (Mode, error) {
	return Parse(os.Getenv("PEERGO_DEPLOYMENT_MODE"))
}
