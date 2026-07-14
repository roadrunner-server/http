package handler

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"log/slog"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/pool/v2/payload"
	"github.com/roadrunner-server/pool/v2/pool"
	staticPool "github.com/roadrunner-server/pool/v2/pool/static_pool"
	"github.com/roadrunner-server/pool/v2/worker"
)

// mockPool satisfies api.Pool. Only Exec is exercised by the tests below.
type mockPool struct{ execErr error }

func (m *mockPool) Workers() []*worker.Process           { return nil }
func (m *mockPool) RemoveWorker(_ context.Context) error { return nil }
func (m *mockPool) AddWorker() error                     { return nil }
func (m *mockPool) Exec(_ context.Context, _ *payload.Payload, _ chan struct{}) (chan *staticPool.PExec, error) {
	return nil, m.execErr
}
func (m *mockPool) Reset(_ context.Context) error { return nil }
func (m *mockPool) Destroy(_ context.Context)     {}

func newTestHandler(t *testing.T, cfg *config.Config, p api.Pool) *Handler {
	t.Helper()
	h, err := NewHandler(cfg, p, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func defaultCfg() *config.Config {
	return &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}
}

// ── Group A: ServeHTTP errors before pool.Exec (nil pool is safe) ────────────

func TestServeHTTP_InvalidMultipart_Returns400(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	// Boundary declared in header but body has no valid multipart parts → EOF.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=1111")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestServeHTTP_StreamBody_MaxBytesExceeded_Returns413(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("this body is too long"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	// Wrap body so that reading more than 5 bytes returns *http.MaxBytesError.
	req.Body = http.MaxBytesReader(rr, req.Body, 5)

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rr.Code)
	}
}

func TestServeHTTP_TruncatedMultipart_Returns400(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	// Multipart body with an open part but no closing boundary → ErrUnexpectedEOF.
	body := "--1111\r\nContent-Disposition: form-data; name=\"f\"\r\n\r\nval"
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=1111")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// ── Group B: Direct calls to handleError, URI, FetchIP ───────────────────────

func TestHandleError_NoFreeWorkers_SetsNoWorkersHeader(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	rr := httptest.NewRecorder()
	h.handleError(rr, errors.E(errors.NoFreeWorkers))

	if got := rr.Header().Get(noWorkers); got != trueStr {
		t.Errorf("expected No-Workers: true, got %q", got)
	}
	if rr.Code != 500 {
		t.Errorf("expected status 500, got %d", rr.Code)
	}
}

func TestHandleError_CustomInternalCode(t *testing.T) {
	cfg := defaultCfg()
	cfg.InternalErrorCode = 503
	h := newTestHandler(t, cfg, nil)

	rr := httptest.NewRecorder()
	h.handleError(rr, fmt.Errorf("boom"))

	if rr.Code != 503 {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleError_DebugMode_WritesEscapedError(t *testing.T) {
	cfg := defaultCfg()
	cfg.Pool = &pool.Config{Debug: true}
	h := newTestHandler(t, cfg, nil)

	rr := httptest.NewRecorder()
	h.handleError(rr, fmt.Errorf("boom<script>"))

	body := rr.Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("response body contains unescaped <script> tag (XSS risk)")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("expected HTML-escaped error in body, got: %q", body)
	}
}

func TestURI_PlainHTTP(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/path?q=1", nil)
	r.Host = "example.com"

	got := URI(r)
	want := "http://example.com/path?q=1"
	if got != want {
		t.Errorf("URI() = %q, want %q", got, want)
	}
}

func TestURI_TLSRequest_HTTPSScheme(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/path?q=1", nil)
	r.Host = "example.com"
	r.TLS = &tls.ConnectionState{}

	got := URI(r)
	want := "https://example.com/path?q=1"
	if got != want {
		t.Errorf("URI() = %q, want %q", got, want)
	}
}

func TestURI_StripsCRLFInjection(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/path", nil)
	r.Host = "example.com"
	// Inject CRLF into the raw query — a classic HTTP response-splitting vector.
	r.URL.RawQuery = "param=value\r\nX-Injected: true"

	got := URI(r)
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("URI() result contains CRLF characters: %q", got)
	}
}

func TestFetchIP_StripPortFromIPv4(t *testing.T) {
	got := FetchIP("127.0.0.1:8080", slog.New(slog.DiscardHandler))
	if got != "127.0.0.1" {
		t.Errorf("FetchIP() = %q, want %q", got, "127.0.0.1")
	}
}

func TestFetchIP_BareIPv6_NoPort(t *testing.T) {
	// "::1" contains colons but is not host:port — SplitHostPort fails,
	// ParseIP succeeds.
	got := FetchIP("::1", slog.New(slog.DiscardHandler))
	if got != "::1" {
		t.Errorf("FetchIP() = %q, want %q", got, "::1")
	}
}

// ── Group C: mockPool tests ───────────────────────────────────────────────────

func TestServeHTTP_PoolExecError_Returns500(t *testing.T) {
	mp := &mockPool{execErr: fmt.Errorf("worker died")}
	h := newTestHandler(t, defaultCfg(), mp)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"key":"val"}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 500 {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// ── Group B′: FetchIP edge cases ─────────────────────────────────────────────

func TestFetchIP_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"ipv4 no port", "10.0.0.1", "10.0.0.1"},
		{"ipv6 bracketed with port", "[::1]:8080", "::1"},
		{"ipv6 full address bare", "2001:db8::1", "2001:db8::1"},
		{"garbage with colons", "not:a:valid:thing", ""},
		{"port only", ":8080", ""},
		{"ipv4 with empty port", "127.0.0.1:", "127.0.0.1"},
		{"ipv6 full with port", "[2001:db8::1]:443", "2001:db8::1"},
	}

	log := slog.New(slog.DiscardHandler)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FetchIP(tt.input, log)
			if got != tt.want {
				t.Errorf("FetchIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ── Group B″: URI edge cases ─────────────────────────────────────────────────

func TestURI_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		setup func() *http.Request
		want  string
	}{
		{
			name: "empty host",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				r.Host = ""
				return r
			},
			want: "http:///",
		},
		{
			name: "host with port",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/p", nil)
				r.Host = "example.com:8080"
				return r
			},
			want: "http://example.com:8080/p",
		},
		{
			name: "url already has host set",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
				r.URL.Host = "other.com"
				return r
			},
			want: "//other.com/x",
		},
		{
			name: "root path only",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				r.Host = "example.com"
				return r
			},
			want: "http://example.com/",
		},
		{
			name: "query but no path",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				r.Host = "example.com"
				r.URL.Path = ""
				r.URL.RawQuery = "a=1"
				return r
			},
			want: "http://example.com?a=1",
		},
		{
			name: "encoded CRLF in path preserved",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/foo%0D%0Abar", nil)
				r.Host = "example.com"
				return r
			},
			want: "http://example.com/foo%0D%0Abar",
		},
		{
			name: "tab in query not stripped",
			setup: func() *http.Request {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				r.Host = "example.com"
				r.URL.RawQuery = "x=1\tX-Bad: true"
				return r
			},
			want: "http://example.com/?x=1\tX-Bad: true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := URI(tt.setup())
			if got != tt.want {
				t.Errorf("URI() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── Group C: mockPool tests ───────────────────────────────────────────────────

func TestServeHTTP_NoFreeWorkers_SetsHeader(t *testing.T) {
	mp := &mockPool{execErr: errors.E(errors.NoFreeWorkers)}
	h := newTestHandler(t, defaultCfg(), mp)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"key":"val"}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get(noWorkers); got != trueStr {
		t.Errorf("expected No-Workers: true, got %q", got)
	}
	if rr.Code != 500 {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}
