package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
)

const (
	defaultMaxMemory = 32 << 20 // 32 MB

	contentNone = iota + 900
	contentMultipart
	contentOther
)

// FetchIP extracts the client IP from net/http's RemoteAddr ("host:port"
// or a bare IP). Returns the empty string for unparseable input.
func FetchIP(pair string, log *slog.Logger) string {
	addr, _, err := net.SplitHostPort(pair)
	if err == nil {
		return addr
	}

	ip := net.ParseIP(pair)
	if ip == nil {
		log.Warn("remote address parsing failure", "error", err)
		return ""
	}
	return ip.String()
}

var crlfReplacer = strings.NewReplacer("\n", "", "\r", "") //nolint:gochecknoglobals

// URI returns the fully-qualified request URI, stripping CR/LF to prevent
// header smuggling via the URL.
func URI(r *http.Request) string {
	uri := crlfReplacer.Replace(r.URL.String())

	if r.URL.Host != "" {
		return uri
	}

	if r.TLS != nil {
		return fmt.Sprintf("https://%s%s", r.Host, uri)
	}
	return fmt.Sprintf("http://%s%s", r.Host, uri)
}

// requestKind classifies r by content-type for the body-handling switch.
// HEAD/OPTIONS get a "none" kind because they never carry a meaningful body.
func requestKind(r *http.Request) int {
	if r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return contentNone
	}
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		return contentMultipart
	}
	return contentOther
}

// extractCookies builds a flat map of cookies (name → URL-unescaped value).
// Used by convertCookies to populate HttpHandlerRequest.Cookies.
func extractCookies(r *http.Request) map[string]string {
	cookies := r.Cookies()
	if len(cookies) == 0 {
		return nil
	}
	out := make(map[string]string, len(cookies))
	for _, c := range cookies {
		if v, err := url.QueryUnescape(c.Value); err == nil {
			out[c.Name] = v
		}
	}
	return out
}

// cleanRawQuery strips CR/LF from the URL raw query before exposing it to PHP.
func cleanRawQuery(q string) string {
	return crlfReplacer.Replace(q)
}

// populateBody fills req.Body / req.Parsed based on the request content-type.
// For multipart requests, it parses uploads and form fields; the returned
// *Uploads must be Open'd by the caller (which writes files to tmpdir and
// populates each FileUpload's Error/Size/TempFilename fields). The caller
// is then responsible for marshaling req.Uploads — *after* Open — so the
// serialized bytes reflect the final per-file state. For everything else,
// the raw body bytes go straight into req.Body.
func populateBody(r *http.Request, req *httpV2.HttpHandlerRequest, uid, gid int) (*Uploads, error) {
	switch requestKind(r) {
	case contentNone:
		return nil, nil

	case contentMultipart:
		// Bounded by the bundled MaxRequestSize middleware applied at the
		// plugin level (plugin.applyBundledMiddleware), so gosec's
		// "unbounded form parsing" warning is a false positive here.
		if err := r.ParseMultipartForm(defaultMaxMemory); err != nil { //nolint:gosec // G120: bounded upstream
			return nil, classifyParseErr(err)
		}
		ups, err := parseUploads(r, uid, gid)
		if err != nil {
			return nil, err
		}
		tree, err := parseMultipartData(r)
		if err != nil {
			return ups, err
		}
		if len(tree) > 0 {
			if req.Body, err = json.Marshal(tree); err != nil {
				return ups, err
			}
		}
		req.Parsed = true
		return ups, nil

	default:
		// r.Body is wrapped by middleware/maxRequest.go's MaxBytesReader, so
		// ReadAll can fail with *http.MaxBytesError on payload overflow —
		// classifyParseErr promotes that to 413 (otherwise it would fall to
		// handleRequestErr's 400 default).
		var err error
		req.Body, err = io.ReadAll(r.Body)
		return nil, classifyParseErr(err)
	}
}
