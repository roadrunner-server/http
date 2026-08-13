package http

import (
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/servers/https"
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

func testServer(cfg *config.Config) *Server {
	return NewHTTPServer(http.NotFoundHandler(), cfg, log.New(io.Discard, "", 0), slog.New(slog.DiscardHandler)).(*Server)
}

func TestNewHTTPServer_PlainConfig(t *testing.T) {
	srv := testServer(&config.Config{Address: "127.0.0.1:8080"})

	if srv.address != "127.0.0.1:8080" {
		t.Errorf("address = %q, want %q", srv.address, "127.0.0.1:8080")
	}
	if srv.redirect {
		t.Error("redirect is on without an ssl config")
	}
	if srv.http.Protocols != nil {
		t.Error("protocols are set without an h2c config")
	}
}

func TestNewHTTPServer_H2CEnablesUnencryptedHTTP2(t *testing.T) {
	srv := testServer(&config.Config{
		Address:     "127.0.0.1:8080",
		HTTP2Config: &https.HTTP2{H2C: true, MaxConcurrentStreams: 42},
	})

	if srv.http.Protocols == nil || !srv.http.Protocols.UnencryptedHTTP2() {
		t.Fatal("unencrypted http2 is not enabled")
	}
	if srv.http.HTTP2 == nil || srv.http.HTTP2.MaxConcurrentStreams != 42 {
		t.Errorf("MaxConcurrentStreams = %v, want 42", srv.http.HTTP2)
	}
}

func TestNewHTTPServer_RedirectTakenFromSSLConfig(t *testing.T) {
	srv := testServer(&config.Config{
		Address:   "127.0.0.1:8080",
		SSLConfig: &https.SSL{Redirect: true, Port: 8443},
	})

	if !srv.redirect || srv.redirectPort != 8443 {
		t.Errorf("redirect = %v, port = %d, want true and 8443", srv.redirect, srv.redirectPort)
	}
}

func TestServe_MalformedAddress_ReturnsListenerError(t *testing.T) {
	srv := testServer(&config.Config{Address: "malformed-address"})

	err := srv.Serve(nil, nil)
	if err == nil {
		t.Fatal("expected an error from CreateListener")
	}
	if !strings.Contains(err.Error(), "serveHTTP") {
		t.Errorf("error = %v, want it to carry the serveHTTP op", err)
	}
}

func TestServe_AppliesMiddlewareAndRedirectBeforeListening(t *testing.T) {
	var trace []string
	srv := testServer(&config.Config{
		Address:   "malformed-address",
		SSLConfig: &https.SSL{Redirect: true, Port: 8443},
	})

	mdwr := map[string]api.Middleware{"outer": recordingMiddleware{name: "outer", trace: &trace}}
	if err := srv.Serve(mdwr, []string{"outer"}); err == nil {
		t.Fatal("expected an error from CreateListener")
	}

	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	// The redirect middleware wraps the chain, so nothing downstream runs.
	if rr.Code != http.StatusPermanentRedirect {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusPermanentRedirect)
	}
	if len(trace) != 0 {
		t.Errorf("trace = %v, want the redirect to short-circuit the chain", trace)
	}
}

func TestApplyMiddleware_OrderPreservedAndUnknownSkipped(t *testing.T) {
	var trace []string

	server := &http.Server{Handler: http.NotFoundHandler()} //nolint:gosec
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

func TestServer_ExposesUnderlyingHTTPServer(t *testing.T) {
	srv := testServer(&config.Config{Address: "127.0.0.1:8080"})

	if _, ok := srv.Server().(*http.Server); !ok {
		t.Fatalf("Server() = %T, want *http.Server", srv.Server())
	}
}

func TestStop_IsIdempotent(t *testing.T) {
	srv := testServer(&config.Config{Address: "127.0.0.1:8080"})

	srv.Stop()
	srv.Stop()
}
