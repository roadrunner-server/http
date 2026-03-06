package middleware

import (
	"net/http"
	"net/http/httptest"
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
		{"ipv6 with port, default ssl port", "[::1]:8080", false, 443, "[::1]"},
		{"ipv6 with port, force port", "[::1]:8080", true, 443, "[::1]:443"},
		{"ipv6 with port, non-default ssl port", "[::1]:8080", false, 8443, "[::1]:8443"},

		// IPv6 without port (bare bracketed form from r.Host)
		{"ipv6 no port, default ssl port", "[::1]", false, 443, "[::1]"},
		{"ipv6 no port, force port", "[::1]", true, 443, "[::1]:443"},

		// Edge cases — degenerate inputs
		{"empty host", "", false, 443, ""},
		{"empty host forced port", "", true, 443, ":443"},
		{"host with trailing colon", "example.com:", false, 443, "example.com"},
		{"ip literal with zone", "[fe80::1%25eth0]:80", false, 443, "[fe80::1%eth0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TLSAddr(tt.host, tt.forcePort, tt.sslPort)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRedirect(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		host     string
		path     string
		query    string
		sslPort  int
		wantLoc  string
		wantCode int
	}{
		{
			name:     "basic http to https",
			method:   http.MethodGet,
			host:     "example.com",
			path:     "/page",
			sslPort:  443,
			wantLoc:  "https://example.com/page",
			wantCode: http.StatusPermanentRedirect,
		},
		{
			name:     "preserves query string",
			method:   http.MethodGet,
			host:     "example.com",
			path:     "/search",
			query:    "q=hello&lang=en",
			sslPort:  443,
			wantLoc:  "https://example.com/search?q=hello&lang=en",
			wantCode: http.StatusPermanentRedirect,
		},
		{
			name:     "non-default ssl port",
			method:   http.MethodGet,
			host:     "example.com",
			path:     "/",
			sslPort:  8443,
			wantLoc:  "https://example.com:8443/",
			wantCode: http.StatusPermanentRedirect,
		},
		{
			name:     "POST gets 308 not 301",
			method:   http.MethodPost,
			host:     "example.com",
			path:     "/api",
			sslPort:  443,
			wantLoc:  "https://example.com/api",
			wantCode: http.StatusPermanentRedirect,
		},
		{
			name:     "IPv6 host",
			method:   http.MethodGet,
			host:     "[::1]:8080",
			path:     "/",
			sslPort:  443,
			wantLoc:  "https://[::1]/",
			wantCode: http.StatusPermanentRedirect,
		},
		{
			name:     "empty path",
			method:   http.MethodGet,
			host:     "example.com",
			path:     "",
			sslPort:  443,
			wantLoc:  "https://example.com",
			wantCode: http.StatusPermanentRedirect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/", nil)
			req.Host = tt.host
			req.URL.Path = tt.path
			req.URL.RawQuery = tt.query

			called := false
			inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				called = true
			})

			rr := httptest.NewRecorder()
			Redirect(inner, tt.sslPort).ServeHTTP(rr, req)

			assert.Equal(t, tt.wantCode, rr.Code)
			assert.Equal(t, tt.wantLoc, rr.Header().Get("Location"))
			assert.NotEmpty(t, rr.Header().Get("Strict-Transport-Security"), "STS header must be set")
			assert.False(t, called, "inner handler should not be called")
		})
	}
}
