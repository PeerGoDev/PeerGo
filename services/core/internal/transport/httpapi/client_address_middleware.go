package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type clientAddressContextKey struct{}

// CaptureClientAddress accepts a proxy-supplied address only from an exact,
// configured immediate peer.  A public caller cannot choose their own stored
// IP by adding or extending X-Forwarded-For.
func CaptureClientAddress(trustedProxyCIDRs []netip.Prefix) func(http.Handler) http.Handler {
	prefixes := append([]netip.Prefix(nil), trustedProxyCIDRs...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if address, ok := requestClientAddress(r, prefixes); ok {
				r = r.WithContext(context.WithValue(r.Context(), clientAddressContextKey{}, address))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientAddressFromContext(ctx context.Context) netip.Addr {
	address, _ := ctx.Value(clientAddressContextKey{}).(netip.Addr)
	return address
}

func requestClientAddress(request *http.Request, trustedProxyCIDRs []netip.Prefix) (netip.Addr, bool) {
	peer, ok := remoteAddress(request.RemoteAddr)
	if !ok {
		return netip.Addr{}, false
	}
	trusted := false
	for _, prefix := range trustedProxyCIDRs {
		if prefix.Contains(peer) {
			trusted = true
			break
		}
	}
	if !trusted {
		return peer, true
	}
	values := request.Header.Values("X-Forwarded-For")
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	raw := strings.TrimSpace(values[0])
	if raw == "" || strings.Contains(raw, ",") {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	if address.Zone() != "" || (!address.Is4() && !address.Is6()) {
		return netip.Addr{}, false
	}
	return address, true
}

func remoteAddress(raw string) (netip.Addr, bool) {
	endpoint, err := netip.ParseAddrPort(raw)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(raw)
		if splitErr != nil {
			return netip.Addr{}, false
		}
		address, parseErr := netip.ParseAddr(host)
		if parseErr != nil {
			return netip.Addr{}, false
		}
		address = address.Unmap()
		return address, address.Zone() == "" && (address.Is4() || address.Is6())
	}
	address := endpoint.Addr().Unmap()
	return address, address.Zone() == "" && (address.Is4() || address.Is6())
}
