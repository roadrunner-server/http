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

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v5/api"
	"github.com/roadrunner-server/http/v5/config"
	"github.com/roadrunner-server/pool/payload"
	"github.com/roadrunner-server/pool/pool"
	staticPool "github.com/roadrunner-server/pool/pool/static_pool"
	"github.com/roadrunner-server/pool/worker"
	"go.uber.org/zap"
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
	h, err := NewHandler(cfg, p, zap.NewNop())
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
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=1111")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestServeHTTP_StreamBody_MaxBytesExceeded_Returns413(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this body is too long"))
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
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
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
	r := httptest.NewRequest(http.MethodGet, "/path?q=1", nil)
	r.Host = "example.com"

	got := URI(r)
	want := "http://example.com/path?q=1"
	if got != want {
		t.Errorf("URI() = %q, want %q", got, want)
	}
}

func TestURI_TLSRequest_HTTPSScheme(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/path?q=1", nil)
	r.Host = "example.com"
	r.TLS = &tls.ConnectionState{}

	got := URI(r)
	want := "https://example.com/path?q=1"
	if got != want {
		t.Errorf("URI() = %q, want %q", got, want)
	}
}

func TestURI_StripsCRLFInjection(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/path", nil)
	r.Host = "example.com"
	// Inject CRLF into the raw query — a classic HTTP response-splitting vector.
	r.URL.RawQuery = "param=value\r\nX-Injected: true"

	got := URI(r)
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("URI() result contains CRLF characters: %q", got)
	}
}

func TestFetchIP_StripPortFromIPv4(t *testing.T) {
	got := FetchIP("127.0.0.1:8080", zap.NewNop())
	if got != "127.0.0.1" {
		t.Errorf("FetchIP() = %q, want %q", got, "127.0.0.1")
	}
}

func TestFetchIP_BareIPv6_NoPort(t *testing.T) {
	// "::1" contains colons but is not host:port — SplitHostPort fails,
	// ParseIP succeeds.
	got := FetchIP("::1", zap.NewNop())
	if got != "::1" {
		t.Errorf("FetchIP() = %q, want %q", got, "::1")
	}
}

// ── Group C: mockPool tests ───────────────────────────────────────────────────

func TestServeHTTP_PoolExecError_Returns500(t *testing.T) {
	mp := &mockPool{execErr: fmt.Errorf("worker died")}
	h := newTestHandler(t, defaultCfg(), mp)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"key":"val"}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 500 {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestServeHTTP_NoFreeWorkers_SetsHeader(t *testing.T) {
	mp := &mockPool{execErr: errors.E(errors.NoFreeWorkers)}
	h := newTestHandler(t, defaultCfg(), mp)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"key":"val"}`))
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
