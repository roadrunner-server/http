package https

import (
	"path/filepath"
	"testing"

	"github.com/roadrunner-server/http/v6/acme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testKeyPath  = "../../tests/test-certs/localhost+2-key.pem"
	testCertPath = "../../tests/test-certs/localhost+2.pem"
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
		{"ipv4 no port default ssl", "127.0.0.1", false, 443, "127.0.0.1"},
		{"host with port default ssl", "example.com:8080", false, 443, "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tlsAddr(tt.host, tt.forcePort, tt.sslPort)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHTTP2_InitDefaults(t *testing.T) {
	tests := []struct {
		name     string
		streams  uint32
		expected uint32
	}{
		{"zero gets the default", 0, 128},
		{"explicit value is kept", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &HTTP2{MaxConcurrentStreams: tt.streams}

			require.NoError(t, cfg.InitDefaults())
			assert.Equal(t, tt.expected, cfg.MaxConcurrentStreams)
		})
	}
}

func TestSSL_InitDefaultsAddress(t *testing.T) {
	empty := &SSL{}
	require.NoError(t, empty.InitDefaults())
	assert.Equal(t, "127.0.0.1:443", empty.Address)

	explicit := &SSL{Address: "0.0.0.0:8443"}
	require.NoError(t, explicit.InitDefaults())
	assert.Equal(t, "0.0.0.0:8443", explicit.Address)
}

func TestSSL_InitDefaultsACME(t *testing.T) {
	invalid := &SSL{Acme: &acme.Config{}}
	err := invalid.InitDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email could not be empty")

	valid := &SSL{Acme: &acme.Config{Email: "user@example.com", Domains: []string{"example.com"}}}
	require.NoError(t, valid.InitDefaults())
	assert.Equal(t, "rr_cache_dir", valid.Acme.CacheDir)
}

func TestSSL_EnableACME(t *testing.T) {
	assert.False(t, (*SSL)(nil).EnableACME())
	assert.False(t, (&SSL{}).EnableACME())
	assert.True(t, (&SSL{Acme: &acme.Config{}}).EnableACME())
}

// Key exists, so Valid must reach and reject the cert check.
func TestSSL_ValidCertMissing(t *testing.T) {
	conf := &SSL{
		Address: "127.0.0.1:8443",
		Key:     testKeyPath,
		Cert:    filepath.Join(t.TempDir(), "absent.crt"),
	}

	err := conf.Valid()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cert file")
	assert.Contains(t, err.Error(), "does not exists")
}

func TestSSL_ValidRootCAMissing(t *testing.T) {
	conf := &SSL{
		Address: "127.0.0.1:8443",
		Key:     testKeyPath,
		Cert:    testCertPath,
		RootCA:  filepath.Join(t.TempDir(), "absent.pem"),
	}

	err := conf.Valid()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root ca path provided")
}

func TestSSL_ValidFullChain(t *testing.T) {
	conf := &SSL{
		Address: "127.0.0.1:8443",
		Key:     testKeyPath,
		Cert:    testCertPath,
		RootCA:  rootCAPath,
	}

	require.NoError(t, conf.Valid())
	assert.Equal(t, "127.0.0.1", conf.host)
	assert.Equal(t, 8443, conf.Port)
}

// With ACME enabled the certificates are issued at runtime, so the key/cert
// stat checks are skipped.
func TestSSL_ValidACMESkipsCertChecks(t *testing.T) {
	conf := &SSL{
		Address: ":8443",
		Acme:    &acme.Config{Email: "user@example.com", Domains: []string{"example.com"}},
	}

	require.NoError(t, conf.Valid())
	assert.Equal(t, "127.0.0.1", conf.host)
}
