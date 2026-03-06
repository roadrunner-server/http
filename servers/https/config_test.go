package https

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSSL_Valid1(t *testing.T) {
	conf := &SSL{
		Address:  "",
		Redirect: false,
		Key:      "",
		Cert:     "",
		RootCA:   "",
		host:     "",
		Port:     0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

func TestSSL_Valid2(t *testing.T) {
	conf := &SSL{
		Address:  ":hello",
		Redirect: false,
		Key:      "",
		Cert:     "",
		RootCA:   "",
		host:     "",
		Port:     0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

func TestSSL_Valid3(t *testing.T) {
	conf := &SSL{
		Address:  ":555",
		Redirect: false,
		Key:      "",
		Cert:     "",
		RootCA:   "",
		host:     "",
		Port:     0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

func TestSSL_Valid4(t *testing.T) {
	conf := &SSL{
		Address:  ":555",
		Redirect: false,
		Key:      "../../tests/plugins/http/fixtures/server.key",
		Cert:     "../../tests/plugins/http/fixtures/server.crt",
		RootCA:   "",
		host:     "",
		// private
		Port: 0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

func TestSSL_Valid5(t *testing.T) {
	conf := &SSL{
		Address:  "a:b:c",
		Redirect: false,
		Key:      "../../../tests/plugins/http/fixtures/server.key",
		Cert:     "../../../tests/plugins/http/fixtures/server.crt",
		RootCA:   "",
		host:     "",
		// private
		Port: 0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

func TestSSL_Valid6(t *testing.T) {
	conf := &SSL{
		Address:  ":",
		Redirect: false,
		Key:      "../../../tests/plugins/http/fixtures/server.key",
		Cert:     "../../../tests/plugins/http/fixtures/server.crt",
		RootCA:   "",
		host:     "",
		// private
		Port: 0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

func TestSSL_Valid7(t *testing.T) {
	conf := &SSL{
		Address:  "127.0.0.1:555:1",
		Redirect: false,
		Key:      "../../../tests/plugins/http/fixtures/server.key",
		Cert:     "../../../tests/plugins/http/fixtures/server.crt",
		RootCA:   "",
		host:     "",
		// private
		Port: 0,
	}

	err := conf.Valid()
	assert.Error(t, err)
}

// Ensures ParseUint enforces 0–65535 (Atoi would have accepted 99999 silently).
func TestSSL_ValidPortOutOfRange(t *testing.T) {
	conf := &SSL{Address: ":99999"}
	assert.Error(t, conf.Valid())
}

// Ensures net.SplitHostPort correctly parses a bracketed IPv6 address.
// Valid() should reach the cert-check step, not fail at the parse step.
func TestSSL_ValidIPv6ParseOK(t *testing.T) {
	conf := &SSL{
		Address: "[::1]:443",
		Key:     "nonexistent.key",
		Cert:    "nonexistent.crt",
	}
	err := conf.Valid()
	// Must fail at cert-not-found, so host and Port were parsed correctly.
	assert.Error(t, err)
	assert.Equal(t, "::1", conf.host)
	assert.Equal(t, 443, conf.Port)
}

func TestTlsAddr(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		forcePort bool
		sslPort   int
		want      string
	}{
		{"ipv4 force port", "0.0.0.0:80", true, 443, "0.0.0.0:443"},
		{"ipv6 with port force port", "[::1]:80", true, 443, "[::1]:443"},
		{"ipv6 with port non-default ssl", "[::1]:80", false, 8443, "[::1]:8443"},
		{"ipv6 no port force port", "[::1]", true, 443, "[::1]:443"},
		{"ipv6 with port default ssl", "[::1]:443", false, 443, "[::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tlsAddr(tt.host, tt.forcePort, tt.sslPort)
			assert.Equal(t, tt.want, got)
		})
	}
}
