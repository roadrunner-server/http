package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleError_WritesBodyAndHeaders(t *testing.T) {
	cases := []struct {
		name      string
		debugMode bool
		wantBody  string // substring match (http.Error appends a trailing newline)
	}{
		{name: "non-debug uses StatusText", debugMode: false, wantBody: http.StatusText(http.StatusInternalServerError)},
		{name: "debug exposes error message", debugMode: true, wantBody: "boom"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{
				log:              slog.New(slog.NewTextHandler(io.Discard, nil)),
				internalHTTPCode: http.StatusInternalServerError,
				debugMode:        tc.debugMode,
			}
			rec := httptest.NewRecorder()

			h.handleError(rec, errors.New("boom"))

			resp := rec.Result()
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusInternalServerError)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Errorf("Content-Type: got %q, want text/plain prefix", ct)
			}
			if nosniff := resp.Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
				t.Errorf("X-Content-Type-Options: got %q, want nosniff", nosniff)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tc.wantBody) {
				t.Errorf("body: got %q, want substring %q", body, tc.wantBody)
			}
		})
	}
}
