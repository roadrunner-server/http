package https

import (
	"bytes"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/roadrunner-server/http/v6/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rootCAPath = "../../tests/test-certs/rootCA.pem"

// markerHandler is comparable by pointer, which http.HandlerFunc is not.
type markerHandler struct{}

func (h *markerHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

// namedMiddleware replaces the wrapped handler with a fixed one so the test can
// assert by identity which handler ended up on the server.
type namedMiddleware struct {
	name        string
	replacement http.Handler
}

func (m *namedMiddleware) Name() string { return m.name }

func (m *namedMiddleware) Middleware(_ http.Handler) http.Handler { return m.replacement }

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func newTestServer(t *testing.T, cfg *SSL, cfgHTTP2 *HTTP2) *http.Server {
	t.Helper()

	srv, err := NewHTTPSServer(http.NotFoundHandler(), cfg, cfgHTTP2, nil, discardLogger())
	require.NoError(t, err)

	https, ok := srv.Server().(*http.Server)
	require.True(t, ok)

	return https
}

func TestNewHTTPSServerAuthType(t *testing.T) {
	tests := []struct {
		name     string
		authType ClientAuthType
		want     tls.ClientAuthType
	}{
		{"no client cert", NoClientCert, tls.NoClientCert},
		{"request client cert", RequestClientCert, tls.RequestClientCert},
		{"require any client cert", RequireAnyClientCert, tls.RequireAnyClientCert},
		{"verify client cert if given", VerifyClientCertIfGiven, tls.VerifyClientCertIfGiven},
		{"require and verify client cert", RequireAndVerifyClientCert, tls.RequireAndVerifyClientCert},
		{"unknown auth type falls back", ClientAuthType("garbage"), tls.NoClientCert},
		{"empty auth type falls back", "", tls.NoClientCert},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			https := newTestServer(t, &SSL{
				Address:  "127.0.0.1:8443",
				Port:     8443,
				RootCA:   rootCAPath,
				AuthType: tt.authType,
			}, nil)

			assert.NotNil(t, https.TLSConfig.ClientCAs)
			assert.Equal(t, tt.want, https.TLSConfig.ClientAuth)
		})
	}
}

// Without a root CA the client auth block is skipped entirely.
func TestNewHTTPSServerNoRootCA(t *testing.T) {
	https := newTestServer(t, &SSL{
		Address:  "127.0.0.1:8443",
		Port:     8443,
		AuthType: RequireAndVerifyClientCert,
	}, nil)

	assert.Nil(t, https.TLSConfig.ClientCAs)
	assert.Equal(t, tls.NoClientCert, https.TLSConfig.ClientAuth)
	assert.Equal(t, "127.0.0.1:8443", https.Addr)
}

func TestNewHTTPSServerRootCAErrors(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "garbage.pem")
	require.NoError(t, os.WriteFile(garbage, []byte("not a pem"), 0o600))

	tests := []struct {
		name    string
		rootCA  string
		wantErr string
	}{
		{"missing file", filepath.Join(t.TempDir(), "absent.pem"), "no such file or directory"},
		{"not a pem", garbage, "could not append Certs from PEM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := NewHTTPSServer(http.NotFoundHandler(), &SSL{
				Address: "127.0.0.1:8443",
				Port:    8443,
				RootCA:  tt.rootCA,
			}, nil, nil, discardLogger())

			require.Error(t, err)
			assert.Nil(t, srv)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNewHTTPSServerHTTP2(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *HTTP2
		wantH2C bool
	}{
		{"nil config", nil, false},
		{"h2c disabled", &HTTP2{H2C: false, MaxConcurrentStreams: 42}, false},
		{"h2c enabled", &HTTP2{H2C: true, MaxConcurrentStreams: 42}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			https := newTestServer(t, &SSL{Address: "127.0.0.1:8443", Port: 8443}, tt.cfg)

			assert.Equal(t, tt.wantH2C, tt.cfg.EnableHTTP2())
			assert.Equal(t, tt.wantH2C, slices.Contains(https.TLSConfig.NextProtos, "h2"))
		})
	}
}

func TestApplyMiddleware(t *testing.T) {
	replacement := &markerHandler{}
	known := &namedMiddleware{name: "known", replacement: replacement}

	t.Run("unknown name is skipped and logged", func(t *testing.T) {
		var logBuf bytes.Buffer
		original := &markerHandler{}
		srv := &http.Server{Handler: original, ReadHeaderTimeout: time.Minute}

		applyMiddleware(srv, map[string]api.Middleware{}, []string{"nope"}, slog.New(slog.NewTextHandler(&logBuf, nil)))

		assert.Same(t, original, srv.Handler)
		assert.Contains(t, logBuf.String(), "requested middleware does not exist")
	})

	t.Run("known name replaces the handler", func(t *testing.T) {
		srv := &http.Server{Handler: &markerHandler{}, ReadHeaderTimeout: time.Minute}

		applyMiddleware(srv, map[string]api.Middleware{"known": known}, []string{"known"}, discardLogger())

		assert.Same(t, replacement, srv.Handler)
	})
}

// A malformed DSN is rejected by the listener factory before anything binds.
func TestServeBadAddress(t *testing.T) {
	cfg := &SSL{Address: "invalid://127.0.0.1:8443", Port: 8443}

	srv, err := NewHTTPSServer(http.NotFoundHandler(), cfg, nil, nil, discardLogger())
	require.NoError(t, err)

	err = srv.Serve(map[string]api.Middleware{"known": &namedMiddleware{
		name:        "known",
		replacement: http.NotFoundHandler(),
	}}, []string{"known"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Protocol")
}
