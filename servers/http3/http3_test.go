package http3

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	quicHTTP3 "github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/http/v6/api"
)

// recordingMiddleware appends its name to trace when the wrapped chain runs.
type recordingMiddleware struct {
	name  string
	trace *[]string
}

func (m recordingMiddleware) Name() string { return m.name }

func (m recordingMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*m.trace = append(*m.trace, m.name)
		next.ServeHTTP(w, r)
	})
}

func testServer(t *testing.T, cfg *Config) *Server {
	t.Helper()

	srv, err := NewHTTP3server(http.NotFoundHandler(), nil, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return srv.(*Server)
}

func TestNewHTTP3server_WithoutACME_UsesDefaultTLSConfig(t *testing.T) {
	srv := testServer(t, &Config{Address: "127.0.0.1:8443"})

	if srv.server.Addr != "127.0.0.1:8443" {
		t.Errorf("Addr = %q, want %q", srv.server.Addr, "127.0.0.1:8443")
	}
	if srv.server.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	for _, proto := range srv.server.TLSConfig.NextProtos {
		if proto == ACMETLS1Protocol {
			t.Errorf("NextProtos contains %q without an acme config", ACMETLS1Protocol)
		}
	}
}

func TestServe_MissingCertificate_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	srv := testServer(t, &Config{
		Address: "127.0.0.1:0",
		Cert:    filepath.Join(dir, "missing.pem"),
		Key:     filepath.Join(dir, "missing-key.pem"),
	})

	if err := srv.Serve(nil, nil); err == nil {
		t.Fatal("expected an error from LoadX509KeyPair")
	}
}

func TestServe_AppliesMiddlewareBeforeListening(t *testing.T) {
	var trace []string

	dir := t.TempDir()
	srv := testServer(t, &Config{
		Address: "127.0.0.1:0",
		Cert:    filepath.Join(dir, "missing.pem"),
		Key:     filepath.Join(dir, "missing-key.pem"),
	})

	mdwr := map[string]api.Middleware{"outer": recordingMiddleware{name: "outer", trace: &trace}}
	if err := srv.Serve(mdwr, []string{"outer"}); err == nil {
		t.Fatal("expected an error from LoadX509KeyPair")
	}

	srv.server.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if len(trace) != 1 || trace[0] != "outer" {
		t.Errorf("trace = %v, want [outer]", trace)
	}
}

func TestApplyMiddleware_OrderPreservedAndUnknownSkipped(t *testing.T) {
	var trace []string

	server := &quicHTTP3.Server{Handler: http.NotFoundHandler()}
	mdwr := map[string]api.Middleware{
		"first":  recordingMiddleware{name: "first", trace: &trace},
		"second": recordingMiddleware{name: "second", trace: &trace},
	}

	applyMiddleware(server, mdwr, []string{"first", "second", "missing"}, slog.New(slog.DiscardHandler))
	server.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if len(trace) != 2 || trace[0] != "first" || trace[1] != "second" {
		t.Errorf("trace = %v, want [first second]", trace)
	}
}

func TestServer_ExposesUnderlyingHTTP3Server(t *testing.T) {
	srv := testServer(t, &Config{Address: "127.0.0.1:8443"})

	if _, ok := srv.Server().(*quicHTTP3.Server); !ok {
		t.Fatalf("Server() = %T, want *http3.Server", srv.Server())
	}
}

func TestStop_IsIdempotent(t *testing.T) {
	srv := testServer(t, &Config{Address: "127.0.0.1:8443"})

	srv.Stop()
	srv.Stop()
}
