package handler

import (
	"encoding/json"
	stderr "errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v6/attributes"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/proxy"
)

const (
	Trailer   = "Trailer"
	HTTP2Push = "Http2-Push"
)

type uploadsCfg struct {
	dir    string
	allow  map[string]struct{}
	forbid map[string]struct{}
}

// Handler serves HTTP requests by handing them off to PHP workers connected
// over ConnectRPC (via proxy.Queue). It does not own worker lifecycle.
type Handler struct {
	queue   *proxy.Queue
	uploads *uploadsCfg
	log     *slog.Logger

	internalHTTPCode uint64
	requestTimeout   time.Duration
	debugMode        bool

	uid int
	gid int

	// reqPool reuses *HttpHandlerRequest envelopes across requests.
	reqPool sync.Pool
}

var _ http.Handler = (*Handler)(nil)

func NewHandler(cfg *config.Config, queue *proxy.Queue, log *slog.Logger) *Handler {
	return &Handler{
		queue: queue,
		log:   log,
		uploads: &uploadsCfg{
			dir:    cfg.Uploads.Dir,
			allow:  cfg.Uploads.Allowed,
			forbid: cfg.Uploads.Forbidden,
		},
		internalHTTPCode: cfg.InternalErrorCode,
		requestTimeout:   cfg.Proxy.RequestTimeout,
		debugMode:        cfg.Proxy.DebugMode,
		uid:              cfg.UID,
		gid:              cfg.GID,
		reqPool: sync.Pool{
			New: func() any { return &httpV2.HttpHandlerRequest{} },
		},
	}
}

func (h *Handler) getReq() *httpV2.HttpHandlerRequest {
	return h.reqPool.Get().(*httpV2.HttpHandlerRequest)
}

func (h *Handler) putReq(req *httpV2.HttpHandlerRequest) {
	req.Reset()
	h.reqPool.Put(req)
}

// ServeHTTP builds a HttpHandlerRequest, submits it to the queue, and blocks
// on the per-request response channel. Multipart uploads are extracted to the
// configured tmpdir; everything else has its body passed through raw.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const op = errors.Op("serve_http")
	start := time.Now()

	id, err := uuid.NewV7()
	if err != nil {
		h.handleError(w, errors.E(op, err))
		h.log.Error("uuid", "elapsed", time.Since(start).Milliseconds(), "error", err)
		return
	}

	req := h.getReq()
	defer h.putReq(req)

	req.Id = id.String()
	req.Method = r.Method
	req.Uri = URI(r)
	req.Protocol = r.Proto
	req.RemoteAddr = FetchIP(r.RemoteAddr, h.log)
	req.Header = convert(r.Header)
	req.Cookies = convertCookies(extractCookies(r))
	req.RawQuery = cleanRawQuery(r.URL.RawQuery)
	req.Attributes = convertAttributes(attributes.All(r))

	ups, err := populateBody(r, req, h.uid, h.gid)
	if err != nil {
		h.handleRequestErr(w, r, ups, err, start)
		return
	}
	if ups != nil {
		// Open mutates each FileUpload (Error / Size / TempFilename) — so we
		// marshal req.Uploads AFTER, not before, to capture the final state.
		ups.Open(h.log, h.uploads.dir, h.uploads.forbid, h.uploads.allow)
		if req.Uploads, err = json.Marshal(ups); err != nil {
			h.handleRequestErr(w, r, ups, err, start)
			return
		}
	}

	respCh, err := h.queue.Submit(req)
	if err != nil {
		clearUploads(h.log, r, ups)
		h.handleSubmitErr(w, err)
		h.log.Error("queue submit",
			"id", req.GetId(),
			"elapsed", time.Since(start).Milliseconds(),
			"error", err,
		)
		return
	}

	timeout := time.NewTimer(h.requestTimeout)
	defer timeout.Stop()

	reqID := req.GetId()

	select {
	case resp := <-respCh:
		h.writeResponse(w, resp, start, reqID)
	case <-r.Context().Done():
		h.queue.Cancel(reqID)
		h.log.Debug("client disconnected",
			"id", reqID,
			"elapsed", time.Since(start).Milliseconds(),
		)
	case <-timeout.C:
		h.queue.Cancel(reqID)
		http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
		h.log.Warn("request timeout",
			"id", reqID,
			"elapsed", time.Since(start).Milliseconds(),
		)
	}

	clearUploads(h.log, r, ups)
}

func (h *Handler) writeResponse(w http.ResponseWriter, resp *httpV2.HttpHandlerResponse, start time.Time, id string) {
	status := int(resp.GetStatus())
	if status < 100 || status >= 600 {
		// Validate status BEFORE writing any worker-supplied headers — otherwise
		// the 500 we serve here would carry Set-Cookie / Location / etc. from
		// the bogus response.
		http.Error(w, fmt.Sprintf("unknown status code from worker: %d", status), http.StatusInternalServerError)
		h.log.Error("invalid worker status",
			"id", id,
			"status", status,
			"elapsed", time.Since(start).Milliseconds(),
		)
		return
	}

	headers := resp.GetHeaders()

	if push := headers[HTTP2Push]; push != nil {
		if pusher, ok := w.(http.Pusher); ok {
			for _, target := range push.GetValues() {
				if err := pusher.Push(target, nil); err != nil {
					h.log.Warn("http/2 push", "id", id, "target", target, "error", err)
				}
			}
		}
	}

	if headers[Trailer] != nil {
		handleProtoTrailers(headers)
	}

	for k, v := range headers {
		for _, vv := range v.GetValues() {
			w.Header().Add(k, vv)
		}
	}

	w.WriteHeader(status)

	body := resp.GetBody()
	if len(body) == 0 {
		return
	}
	if _, err := w.Write(body); err != nil {
		if stderr.Is(err, errEPIPE) {
			h.log.Debug("response write: broken pipe",
				"id", id,
				"elapsed", time.Since(start).Milliseconds(),
			)
			return
		}
		h.log.Error("response write",
			"id", id,
			"elapsed", time.Since(start).Milliseconds(),
			"error", err,
		)
	}

	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

func handleProtoTrailers(h map[string]*httpV2.HttpHeaderValue) {
	for _, tr := range h[Trailer].GetValues() {
		for n := range strings.SplitSeq(tr, ",") {
			n = strings.Trim(n, "\t ")
			if v, ok := h[n]; ok {
				h["Trailer:"+n] = v
				delete(h, n)
			}
		}
	}
	delete(h, Trailer)
}

func (h *Handler) handleRequestErr(w http.ResponseWriter, r *http.Request, ups *Uploads, err error, start time.Time) {
	clearUploads(h.log, r, ups)

	if stderr.Is(err, errEPIPE) {
		h.log.Error("request decode: broken pipe",
			"elapsed", time.Since(start).Milliseconds(),
			"error", err,
		)
		return
	}

	status := http.StatusInternalServerError
	switch {
	case isMaxBytesError(err):
		status = http.StatusRequestEntityTooLarge
	case stderr.Is(err, io.EOF), stderr.Is(err, io.ErrUnexpectedEOF):
		status = http.StatusBadRequest
	}

	http.Error(w, err.Error(), status)
	h.log.Error("request decode",
		"elapsed", time.Since(start).Milliseconds(),
		"error", err,
	)
}

func (h *Handler) handleSubmitErr(w http.ResponseWriter, err error) {
	if stderr.Is(err, proxy.ErrInboxFull) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	h.handleError(w, err)
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	// internalHTTPCode is a config-provided HTTP status, defaulted to 500 and
	// always within [100, 599] in practice. The cast is safe.
	w.WriteHeader(int(h.internalHTTPCode)) //nolint:gosec // G115: bounded HTTP status code
	if h.debugMode {
		template.HTMLEscape(w, []byte(err.Error()))
	}
}

func isMaxBytesError(err error) bool {
	_, ok := stderr.AsType[*http.MaxBytesError](err)
	return ok
}

func clearUploads(log *slog.Logger, r *http.Request, ups *Uploads) {
	if ups != nil {
		ups.Clear(log)
	}
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
}
