package trusted

import (
	"net/http"
	"testing"
)

type headerTable struct {
	key      string // header key
	val      string // header val
	expected string // expected result
}

func TestIP(t *testing.T) {
	headers := []headerTable{
		{xff, "8.8.8.8", "8.8.8.8"},                                   // Single address
		{xff, "8.8.8.8, 8.8.4.4", "8.8.8.8"},                          // Multiple
		{xff, "[2001:db8:cafe::17]:4711", "[2001:db8:cafe::17]:4711"}, // IPv6 address
		{xff, "", ""},                                                  // None
		{xrip, "8.8.8.8", "8.8.8.8"},                                   // Single address
		{xrip, "8.8.8.8, 8.8.4.4", "8.8.8.8, 8.8.4.4"},                 // Multiple
		{xrip, "[2001:db8:cafe::17]:4711", "[2001:db8:cafe::17]:4711"}, // IPv6 address
		{xrip, "", ""},                                                 // None
		{cfip, "8.8.8.8", "8.8.8.8"},                                   // Single address
		{tcip, "8.8.8.8", "8.8.8.8"},                                   // Single address
		{forwarded, `for="_foo"`, "_foo"},                              // Hostname
		{forwarded, `For="[2001:db8:cafe::17]:4711`, `[2001:db8:cafe::17]:4711`},      // IPv6 address
		{forwarded, `for=192.0.2.60;proto=http;by=203.0.113.43`, `192.0.2.60`},        // Multiple params
		{forwarded, `for=192.0.2.43, for=198.51.100.17`, "192.0.2.43"},                // Multiple params
		{forwarded, `for="workstation.local",for=198.51.100.17`, "workstation.local"}, // Hostname
	}

	tr := NewTrustedResolver(nil)
	for _, v := range headers {
		req := &http.Request{
			Header: http.Header{
				v.key: []string{v.val},
			}}
		res := tr.resolveIP(req.Header)
		if res != v.expected {
			t.Fatalf("wrong header for %s: got %s want %s", v.key, res,
				v.expected)
		}
	}
}
