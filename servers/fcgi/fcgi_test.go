package fcgi

import (
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func testServer(handler http.Handler) *Server {
	return NewFCGIServer(handler, &FCGI{Address: "malformed-address"}, slog.New(slog.DiscardHandler), log.New(io.Discard, "", 0)).(*Server)
}

func TestServe_MalformedAddress_ReturnsListenerError(t *testing.T) {
	srv := testServer(http.NotFoundHandler())

	err := srv.Serve(nil, nil)
	if err == nil {
		t.Fatal("expected an error from CreateListener")
	}
	if !strings.Contains(err.Error(), "serve_fcgi") {
		t.Errorf("error = %v, want it to carry the serve_fcgi op", err)
	}
}

func TestServe_AppliesMiddlewareBeforeListening(t *testing.T) {
	var trace []string
	srv := testServer(http.NotFoundHandler())

	mdwr := map[string]api.Middleware{"outer": recordingMiddleware{name: "outer", trace: &trace}}
	if err := srv.Serve(mdwr, []string{"outer"}); err == nil {
		t.Fatal("expected an error from CreateListener")
	}

	srv.fcgi.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if len(trace) != 1 || trace[0] != "outer" {
		t.Errorf("trace = %v, want [outer]", trace)
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
	srv := testServer(http.NotFoundHandler())

	if _, ok := srv.Server().(*http.Server); !ok {
		t.Fatalf("Server() = %T, want *http.Server", srv.Server())
	}
}

func TestStop_IsIdempotent(t *testing.T) {
	srv := testServer(http.NotFoundHandler())

	srv.Stop()
	srv.Stop()
}
