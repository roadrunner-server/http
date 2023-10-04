package handler

import (
	stderr "errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/goccy/go-json"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/sdk/v4/payload"
)

const (
	Trailer   string = "Trailer"
	HTTP2Push string = "Http2-Push"
)

// Response handles PSR7 response logic.
type Response struct {
	// Status contains response status.
	Status int `json:"status"`
	// Header contains a list of response headers.
	Headers map[string][]string `json:"headers"`
}

// Write writes response headers, status and body into ResponseWriter.
func (h *Handler) Write(pld *payload.Payload, w http.ResponseWriter) error {
	rsp := h.getRsp()
	defer h.putRsp(rsp)

	if len(pld.Context) != 0 {
		// unmarshal context into response
		err := json.Unmarshal(pld.Context, rsp)
		if err != nil {
			return err
		}

		// handle push headers
		if len(rsp.Headers[HTTP2Push]) != 0 {
			push := rsp.Headers[HTTP2Push]

			if pusher, ok := w.(http.Pusher); ok {
				for i := 0; i < len(push); i++ {
					err = pusher.Push(rsp.Headers[HTTP2Push][i], nil)
					if err != nil {
						return err
					}
				}
			}
		}

		if len(rsp.Headers[Trailer]) != 0 {
			handleTrailers(rsp.Headers)
		}

		// write all headers from the response to the writer
		for k := range rsp.Headers {
			for kk := range rsp.Headers[k] {
				w.Header().Add(k, rsp.Headers[k][kk])
			}
		}

		// The provided code must be a valid HTTP 1xx-5xx status code.
		if rsp.Status < 100 || rsp.Status >= 600 {
			http.Error(w, fmt.Sprintf("unknown status code from worker: %d", rsp.Status), 500)
			return errors.Errorf("unknown status code from worker: %d", rsp.Status)
		}

		w.WriteHeader(rsp.Status)
	}

	// do not write body if it is empty
	if len(pld.Body) == 0 {
		return nil
	}

	_, err := w.Write(pld.Body)
	if err != nil {
		return err
	}

	rw := http.NewResponseController(w) //nolint:bodyclose
	err = rw.Flush()
	if stderr.Is(err, http.ErrNotSupported) {
		h.log.Warn("flushing is not supported by the response writer, using buffered writer")
	}

	return nil
}

func handleTrailers(h map[string][]string) {
	for _, tr := range h[Trailer] {
		for _, n := range strings.Split(tr, ",") {
			n = strings.Trim(n, "\t ")
			if v, ok := h[n]; ok {
				h["Trailer:"+n] = v

				delete(h, n)
			}
		}
	}

	delete(h, Trailer)
}
