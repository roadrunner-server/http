package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpV1proto "github.com/roadrunner-server/api-go/v6/http/v1"
	"github.com/roadrunner-server/goridge/v4/pkg/frame"
	"github.com/roadrunner-server/pool/v2/payload"
	"google.golang.org/protobuf/proto"
)

// pushRecorder adds http.Pusher to httptest.ResponseRecorder so the HTTP/2 push
// block in handlePROTOresponse can be driven without a real h2 connection.
type pushRecorder struct {
	*httptest.ResponseRecorder
	pushed  []string
	pushErr error
}

func (p *pushRecorder) Push(target string, _ *http.PushOptions) error {
	p.pushed = append(p.pushed, target)
	return p.pushErr
}

func headerValue(values ...string) *httpV1proto.HeaderValue {
	hv := &httpV1proto.HeaderValue{}
	for _, v := range values {
		hv.Value = append(hv.Value, []byte(v))
	}
	return hv
}

// marshalRsp encodes a worker response the way the PHP side sends it back.
func marshalRsp(t *testing.T, status int64, headers map[string]*httpV1proto.HeaderValue) []byte {
	t.Helper()

	data, err := proto.Marshal(&httpV1proto.Response{Status: status, Headers: headers})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWrite_JSONCodec_NotSupported(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	err := h.Write(&payload.Payload{Codec: frame.CodecJSON}, httptest.NewRecorder())
	if err == nil {
		t.Fatal("expected an error for the JSON codec")
	}
	if !strings.Contains(err.Error(), "JSON codec is not supported") {
		t.Errorf("error = %v, want a JSON codec error", err)
	}
}

func TestWrite_UnknownCodec_ReturnsCodecInError(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	err := h.Write(&payload.Payload{Codec: 99}, httptest.NewRecorder())
	if err == nil {
		t.Fatal("expected an error for an unknown codec")
	}
	if !strings.Contains(err.Error(), "unknown payload type: 99") {
		t.Errorf("error = %v, want an unknown payload type error", err)
	}
}

func TestWrite_MalformedContext_ReturnsUnmarshalError(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{Codec: frame.CodecProto, Context: []byte{0xff, 0xff, 0xff}}
	if err := h.Write(pld, httptest.NewRecorder()); err == nil {
		t.Fatal("expected an error from proto.Unmarshal")
	}
}

func TestWrite_HeadersAndStatus_NoBody(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{
		Codec:   frame.CodecProto,
		Context: marshalRsp(t, http.StatusCreated, map[string]*httpV1proto.HeaderValue{"X-A": headerValue("1", "2")}),
	}

	rr := httptest.NewRecorder()
	if err := h.Write(pld, rr); err != nil {
		t.Fatal(err)
	}

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusCreated)
	}
	if got := rr.Header().Values("X-A"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("X-A = %v, want [1 2]", got)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rr.Body.String())
	}
}

func TestWrite_Body_WrittenAfterHeaders(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{
		Codec:   frame.CodecProto,
		Context: marshalRsp(t, http.StatusOK, nil),
		Body:    []byte("hello"),
	}

	rr := httptest.NewRecorder()
	if err := h.Write(pld, rr); err != nil {
		t.Fatal(err)
	}

	if rr.Body.String() != "hello" {
		t.Errorf("body = %q, want %q", rr.Body.String(), "hello")
	}
	if !rr.Flushed {
		t.Error("the recorder was not flushed")
	}
}

func TestWrite_StatusOutOfRange_Returns500(t *testing.T) {
	tests := []struct {
		name   string
		status int64
	}{
		{"below the 1xx range", 42},
		{"above the 5xx range", 600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t, defaultCfg(), nil)

			pld := &payload.Payload{Codec: frame.CodecProto, Context: marshalRsp(t, tt.status, nil)}
			rr := httptest.NewRecorder()

			err := h.Write(pld, rr)
			if err == nil {
				t.Fatal("expected an unknown status code error")
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("unknown status code from worker: %d", tt.status)) {
				t.Errorf("error = %v, want an unknown status code error", err)
			}
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestWrite_Push_TargetsForwardedToPusher(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{
		Codec: frame.CodecProto,
		Context: marshalRsp(t, http.StatusOK, map[string]*httpV1proto.HeaderValue{
			HTTP2Push: headerValue("/a.css", "/b.js"),
		}),
	}

	rr := &pushRecorder{ResponseRecorder: httptest.NewRecorder()}
	if err := h.Write(pld, rr); err != nil {
		t.Fatal(err)
	}

	want := []string{"/a.css", "/b.js"}
	if len(rr.pushed) != len(want) {
		t.Fatalf("pushed = %v, want %v", rr.pushed, want)
	}
	for i := range want {
		if rr.pushed[i] != want[i] {
			t.Errorf("pushed[%d] = %q, want %q", i, rr.pushed[i], want[i])
		}
	}
}

func TestWrite_PushError_Propagated(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{
		Codec: frame.CodecProto,
		Context: marshalRsp(t, http.StatusOK, map[string]*httpV1proto.HeaderValue{
			HTTP2Push: headerValue("/a.css"),
		}),
	}

	rr := &pushRecorder{ResponseRecorder: httptest.NewRecorder(), pushErr: fmt.Errorf("push refused")}

	err := h.Write(pld, rr)
	if err == nil {
		t.Fatal("expected the pusher error to be returned")
	}
	if !strings.Contains(err.Error(), "push refused") {
		t.Errorf("error = %v, want the pusher error", err)
	}
}

func TestWrite_PlainWriter_IgnoresPushHeader(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{
		Codec: frame.CodecProto,
		Context: marshalRsp(t, http.StatusOK, map[string]*httpV1proto.HeaderValue{
			HTTP2Push: headerValue("/a.css"),
		}),
	}

	rr := httptest.NewRecorder()
	if err := h.Write(pld, rr); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleProtoTrailers_RenamesAnnouncedHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]*httpV1proto.HeaderValue
		want    map[string]string
		gone    []string
	}{
		{
			name: "comma separated list",
			headers: map[string]*httpV1proto.HeaderValue{
				Trailer: headerValue("X-A, X-B"),
				"X-A":   headerValue("1"),
				"X-B":   headerValue("2"),
			},
			want: map[string]string{"Trailer:X-A": "1", "Trailer:X-B": "2"},
			gone: []string{Trailer, "X-A", "X-B"},
		},
		{
			name: "surrounding tabs and spaces trimmed",
			headers: map[string]*httpV1proto.HeaderValue{
				Trailer: headerValue("\tX-A ,  X-B\t"),
				"X-A":   headerValue("1"),
				"X-B":   headerValue("2"),
			},
			want: map[string]string{"Trailer:X-A": "1", "Trailer:X-B": "2"},
			gone: []string{Trailer, "X-A", "X-B"},
		},
		{
			name: "announced header that was never sent",
			headers: map[string]*httpV1proto.HeaderValue{
				Trailer: headerValue("X-Missing"),
				"X-A":   headerValue("1"),
			},
			want: map[string]string{"X-A": "1"},
			gone: []string{Trailer, "Trailer:X-Missing"},
		},
		{
			name: "multiple Trailer values",
			headers: map[string]*httpV1proto.HeaderValue{
				Trailer: headerValue("X-A", "X-B"),
				"X-A":   headerValue("1"),
				"X-B":   headerValue("2"),
			},
			want: map[string]string{"Trailer:X-A": "1", "Trailer:X-B": "2"},
			gone: []string{Trailer, "X-A", "X-B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handleProtoTrailers(tt.headers)

			for k, v := range tt.want {
				hv, ok := tt.headers[k]
				if !ok {
					t.Fatalf("header %q is missing", k)
				}
				if len(hv.GetValue()) != 1 || string(hv.GetValue()[0]) != v {
					t.Errorf("header %q = %q, want %q", k, hv.GetValue(), v)
				}
			}
			for _, k := range tt.gone {
				if _, ok := tt.headers[k]; ok {
					t.Errorf("header %q is still present", k)
				}
			}
		})
	}
}

func TestWrite_Trailers_RenamedOnTheWire(t *testing.T) {
	h := newTestHandler(t, defaultCfg(), nil)

	pld := &payload.Payload{
		Codec: frame.CodecProto,
		Context: marshalRsp(t, http.StatusOK, map[string]*httpV1proto.HeaderValue{
			Trailer:      headerValue("X-Checksum"),
			"X-Checksum": headerValue("abc"),
		}),
	}

	rr := httptest.NewRecorder()
	if err := h.Write(pld, rr); err != nil {
		t.Fatal(err)
	}

	if got := rr.Header().Get("Trailer:X-Checksum"); got != "abc" {
		t.Errorf("Trailer:X-Checksum = %q, want %q", got, "abc")
	}
	if got := rr.Header().Get("X-Checksum"); got != "" {
		t.Errorf("X-Checksum = %q, want it to be renamed away", got)
	}
}
