package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTLSAddr(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		forcePort bool
		sslPort   int
		want      string
	}{
		// IPv4 with port — standard cases
		{"ipv4 with port, default ssl port", "example.com:8080", false, 443, "example.com"},
		{"ipv4 with port, force port", "example.com:8080", true, 443, "example.com:443"},
		{"ipv4 with port, non-default ssl port", "example.com:8080", false, 8443, "example.com:8443"},

		// IPv4 without port
		{"ipv4 no port, default ssl port", "example.com", false, 443, "example.com"},
		{"ipv4 no port, force port", "example.com", true, 443, "example.com:443"},

		// IPv6 with port — core fix
		{"ipv6 with port, default ssl port", "[::1]:8080", false, 443, "::1"},
		{"ipv6 with port, force port", "[::1]:8080", true, 443, "[::1]:443"},
		{"ipv6 with port, non-default ssl port", "[::1]:8080", false, 8443, "[::1]:8443"},

		// IPv6 without port (bare bracketed form from r.Host)
		{"ipv6 no port, default ssl port", "[::1]", false, 443, "::1"},
		{"ipv6 no port, force port", "[::1]", true, 443, "[::1]:443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TLSAddr(tt.host, tt.forcePort, tt.sslPort)
			assert.Equal(t, tt.want, got)
		})
	}
}
