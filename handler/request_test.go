package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpV1proto "github.com/roadrunner-server/api-go/v6/http/v1"
	"github.com/roadrunner-server/pool/v2/payload"
)

// newPSRRequest builds the *Request that request() fills in. ServeHTTP normally
// takes it from a sync.Pool with these two maps already allocated.
func newPSRRequest(r *http.Request) *Request {
	return &Request{
		Method:  r.Method,
		Header:  r.Header,
		Cookies: make(map[string]string),
	}
}

// multipartBody returns an encoded multipart body together with its content type.
func multipartBody(t *testing.T, field, filename, content string) (string, string) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = w.WriteField("field", "value"); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.String(), w.FormDataContentType()
}

func TestContentType_MethodAndHeaderTable(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		contentType string
		want        int
	}{
		{"HEAD ignores the content type", http.MethodHead, "application/json", contentNone},
		{"OPTIONS ignores the content type", http.MethodOptions, "multipart/form-data; boundary=x", contentNone},
		{"urlencoded with charset parameter", http.MethodPost, "application/x-www-form-urlencoded; charset=utf-8", contentURLEncoded},
		{"multipart with boundary", http.MethodPost, "multipart/form-data; boundary=x", contentMultipart},
		{"json falls back to stream", http.MethodPost, "application/json", contentStream},
		{"missing content type falls back to stream", http.MethodPost, "", contentStream},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr := http.Header{}
			if tt.contentType != "" {
				hdr.Set("Content-Type", tt.contentType)
			}

			req := &Request{Method: tt.method, Header: hdr}
			if got := req.contentType(); got != tt.want {
				t.Errorf("contentType() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRequest_RawBody_URLEncodedStaysUnparsed(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("a=1&b=2"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req := newPSRRequest(r)
	if err := request(r, req, 0, 0, true); err != nil {
		t.Fatal(err)
	}

	body, ok := req.body.([]byte)
	if !ok {
		t.Fatalf("body is %T, want []byte", req.body)
	}
	if string(body) != "a=1&b=2" {
		t.Errorf("body = %q, want %q", body, "a=1&b=2")
	}
	if req.Parsed {
		t.Error("Parsed is true, raw body must stay unparsed")
	}
}

func TestRequest_RawBody_MultipartKeepsEnvelopeAndSkipsUploads(t *testing.T) {
	raw, ct := multipartBody(t, "file", "upload.txt", "content")

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(raw))
	r.Header.Set("Content-Type", ct)

	req := newPSRRequest(r)
	if err := request(r, req, 0, 0, true); err != nil {
		t.Fatal(err)
	}

	body, ok := req.body.([]byte)
	if !ok {
		t.Fatalf("body is %T, want []byte", req.body)
	}
	if string(body) != raw {
		t.Error("body differs from the raw multipart envelope")
	}
	if req.Uploads != nil {
		t.Error("Uploads is set, raw body must not trigger upload parsing")
	}
	if req.Parsed {
		t.Error("Parsed is true, raw body must stay unparsed")
	}
}

func TestRequest_HeadWithBody_ReadsNothing(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/", strings.NewReader("ignored"))
	r.Header.Set("Content-Type", "application/json")

	req := newPSRRequest(r)
	if err := request(r, req, 0, 0, false); err != nil {
		t.Fatal(err)
	}

	if req.body != nil {
		t.Errorf("body = %v, want nil", req.body)
	}
	if req.Parsed {
		t.Error("Parsed is true, contentNone returns before parsing")
	}
}

func TestRequest_CookiesUnescapedAndBadEscapeDropped(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.Header.Add("Cookie", "a=b%20c; d=e; bad=%zz")

	req := newPSRRequest(r)
	if err := request(r, req, 0, 0, false); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{"a": "b c", "d": "e"}
	if len(req.Cookies) != len(want) {
		t.Fatalf("Cookies = %v, want %v", req.Cookies, want)
	}
	for k, v := range want {
		if req.Cookies[k] != v {
			t.Errorf("Cookies[%q] = %q, want %q", k, req.Cookies[k], v)
		}
	}
}

func TestRequest_URLEncodedInvalidEscape_ReturnsParseFormError(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("%zz=1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req := newPSRRequest(r)
	err := request(r, req, 0, 0, false)
	if err == nil {
		t.Fatal("expected an error from ParseForm")
	}
	if !strings.Contains(err.Error(), "invalid URL escape") {
		t.Errorf("error = %v, want an invalid URL escape error", err)
	}
}

func TestRequest_URLEncodedConflictingKeys_ReturnsTreeError(t *testing.T) {
	// "a" as a scalar and "a[b]" as a branch cannot coexist in the same tree.
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("a=1&a[b]=2"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req := newPSRRequest(r)
	err := request(r, req, 0, 0, false)
	if err == nil {
		t.Fatal("expected an error from parsePostForm")
	}
	if !strings.Contains(err.Error(), "invalid multiple values") {
		t.Errorf("error = %v, want an invalid multiple values error", err)
	}
}

func TestRequest_MultipartParsed_FillsUploadsAndTree(t *testing.T) {
	raw, ct := multipartBody(t, "file", "upload.txt", "content")

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(raw))
	r.Header.Set("Content-Type", ct)

	req := newPSRRequest(r)
	if err := request(r, req, 0, 0, false); err != nil {
		t.Fatal(err)
	}

	if req.Uploads == nil || len(req.Uploads.list) != 1 {
		t.Fatalf("Uploads = %v, want exactly one file", req.Uploads)
	}
	tree, ok := req.body.(dataTree)
	if !ok {
		t.Fatalf("body is %T, want dataTree", req.body)
	}
	if tree["field"] != "value" {
		t.Errorf("tree[field] = %v, want %q", tree["field"], "value")
	}
	if !req.Parsed {
		t.Error("Parsed is false, parsed multipart must set it")
	}
}

func TestPayload_RawBodyNil_LeavesBodyUntouched(t *testing.T) {
	req := &Request{}
	pld := &payload.Payload{}

	if err := req.Payload(pld, true, &httpV1proto.Request{Method: http.MethodPost}); err != nil {
		t.Fatal(err)
	}
	if pld.Body != nil {
		t.Errorf("Body = %q, want nil", pld.Body)
	}
	if len(pld.Context) == 0 {
		t.Error("Context is empty, the proto request must always be marshaled")
	}
}

func TestPayload_RawBodyBytes_CopiedAsIs(t *testing.T) {
	req := &Request{body: []byte("a=1&b=2")}
	pld := &payload.Payload{}

	if err := req.Payload(pld, true, &httpV1proto.Request{}); err != nil {
		t.Fatal(err)
	}
	if string(pld.Body) != "a=1&b=2" {
		t.Errorf("Body = %q, want %q", pld.Body, "a=1&b=2")
	}
}

func TestPayload_ParsedTree_MarshaledToJSON(t *testing.T) {
	req := &Request{Parsed: true, body: dataTree{"k": "v"}}
	pld := &payload.Payload{}

	if err := req.Payload(pld, false, &httpV1proto.Request{}); err != nil {
		t.Fatal(err)
	}
	if string(pld.Body) != `{"k":"v"}` {
		t.Errorf("Body = %q, want %q", pld.Body, `{"k":"v"}`)
	}
}

func TestPayload_ParsedEmptyTree_LeavesBodyUntouched(t *testing.T) {
	req := &Request{Parsed: true, body: dataTree{}}
	pld := &payload.Payload{}

	if err := req.Payload(pld, false, &httpV1proto.Request{}); err != nil {
		t.Fatal(err)
	}
	if pld.Body != nil {
		t.Errorf("Body = %q, want nil", pld.Body)
	}
}

func TestPayload_UnparsedBytes_CopiedAsIs(t *testing.T) {
	req := &Request{body: []byte(`{"k":"v"}`)}
	pld := &payload.Payload{}

	if err := req.Payload(pld, false, &httpV1proto.Request{}); err != nil {
		t.Fatal(err)
	}
	if string(pld.Body) != `{"k":"v"}` {
		t.Errorf("Body = %q, want %q", pld.Body, `{"k":"v"}`)
	}
}

func TestPayload_UnknownBodyType_ReturnsError(t *testing.T) {
	tests := []struct {
		name   string
		parsed bool
	}{
		{"parsed", true},
		{"unparsed", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{Parsed: tt.parsed, body: 42}
			pld := &payload.Payload{}

			err := req.Payload(pld, false, &httpV1proto.Request{})
			if err == nil {
				t.Fatal("expected an unknown body type error")
			}
			if !strings.Contains(err.Error(), "unknown body type") {
				t.Errorf("error = %v, want an unknown body type error", err)
			}
		})
	}
}

func TestPayload_Uploads_MarshaledIntoProtoRequest(t *testing.T) {
	req := &Request{Uploads: &Uploads{tree: fileTree{"f": &FileUpload{Name: "x.txt"}}}}
	protoReq := &httpV1proto.Request{}

	if err := req.Payload(&payload.Payload{}, false, protoReq); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(protoReq.GetUploads(), []byte("x.txt")) {
		t.Errorf("Uploads = %q, want the serialized file name", protoReq.GetUploads())
	}
}

func TestPackDataTree_EmptyTreeWritesNothing(t *testing.T) {
	pld := &payload.Payload{}

	if err := packDataTree(dataTree{}, pld); err != nil {
		t.Fatal(err)
	}
	if pld.Body != nil {
		t.Errorf("Body = %q, want nil", pld.Body)
	}
}
