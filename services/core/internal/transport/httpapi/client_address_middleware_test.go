package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestCaptureClientAddressTrustsOnlyConfiguredImmediateProxy(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{netip.MustParsePrefix("172.20.0.4/32")}
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{
			name: "untrusted peer cannot spoof forwarded header", remoteAddr: "198.51.100.8:42000",
			forwarded: []string{"203.0.113.9"}, want: "198.51.100.8",
		},
		{
			name: "trusted peer supplies one address", remoteAddr: "172.20.0.4:51000",
			forwarded: []string{"2001:db8::8"}, want: "2001:db8::8",
		},
		{
			name: "trusted peer comma chain is rejected", remoteAddr: "172.20.0.4:51000",
			forwarded: []string{"203.0.113.9, 172.20.0.4"},
		},
		{
			name: "trusted peer duplicate headers are rejected", remoteAddr: "172.20.0.4:51000",
			forwarded: []string{"203.0.113.9", "203.0.113.10"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "http://peergo.test/login", nil)
			request.RemoteAddr = test.remoteAddr
			for _, value := range test.forwarded {
				request.Header.Add("X-Forwarded-For", value)
			}
			response := httptest.NewRecorder()
			CaptureClientAddress(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				address := clientAddressFromContext(request.Context())
				if address.IsValid() {
					response.Header().Set("Observed-Address", address.String())
				}
			})).ServeHTTP(response, request)
			if got := response.Header().Get("Observed-Address"); got != test.want {
				t.Fatalf("observed address = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCaptureClientAddressUnmapsIPv4MappedAddresses(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "http://peergo.test/login", nil)
	request.RemoteAddr = "[::ffff:192.0.2.8]:443"
	address, ok := requestClientAddress(request, nil)
	if !ok || address.String() != "192.0.2.8" {
		t.Fatalf("requestClientAddress() = %q, %v", address, ok)
	}
}
