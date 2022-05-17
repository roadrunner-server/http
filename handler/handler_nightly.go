//go:build nightly

package handler

import (
	stderr "errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/roadrunner-server/api/v2/payload"
	"github.com/roadrunner-server/api/v2/pool"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/goridge/v3/pkg/frame"
	"github.com/roadrunner-server/http/v2/attributes"
	httpConf "github.com/roadrunner-server/http/v2/http"
	uploadsConf "github.com/roadrunner-server/http/v2/uploads"
	"go.uber.org/zap"
)

const (
	noWorkers string = "No-Workers"
	trueStr   string = "true"
)

type uploads struct {
	dir    string
	allow  map[string]struct{}
	forbid map[string]struct{}
}

// Handler serves http connections to underlying PHP application using PSR-7 protocol. Context will include request headers,
// parsed files and query, payload will include parsed form dataTree (if any).
type Handler struct {
	uploads *uploads
	log     *zap.Logger
	pool    pool.Pool

	accessLogs       bool
	internalHTTPCode uint64

	// internal
	reqPool  sync.Pool
	respPool sync.Pool
	pldPool  sync.Pool
	errPool  sync.Pool
}

// NewHandler return handle interface implementation
func NewHandler(httpCfg *httpConf.Config, uploadsCfg *uploadsConf.Uploads, pool pool.Pool, log *zap.Logger) (*Handler, error) {
	return &Handler{
		uploads: &uploads{
			dir:    uploadsCfg.Dir,
			allow:  uploadsCfg.Allowed,
			forbid: uploadsCfg.Forbidden,
		},
		pool:             pool,
		log:              log,
		internalHTTPCode: httpCfg.InternalErrorCode,
		accessLogs:       httpCfg.AccessLogs,
		errPool: sync.Pool{
			New: func() any {
				return make(chan error, 1)
			},
		},
		reqPool: sync.Pool{
			New: func() any {
				return &Request{
					Attributes: make(map[string]interface{}),
					Cookies:    make(map[string]string),
					body:       nil,
				}
			},
		},
		respPool: sync.Pool{
			New: func() interface{} {
				return &Response{
					Headers: make(map[string][]string),
					Status:  -1,
				}
			},
		},
		pldPool: sync.Pool{
			New: func() interface{} {
				return &payload.Payload{
					Body:    make([]byte, 0, 100),
					Context: make([]byte, 0, 100),
					Codec:   frame.CodecJSON,
				}
			},
		},
	}, nil
}

// ServeHTTP transform original request to the PSR-7 passed then to the underlying application. Attempts to serve static files first if enabled.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const op = errors.Op("serve_http")
	start := time.Now()

	var bw bodyWrapper
	if r.Body != nil {
		bw.ReadCloser = r.Body
		r.Body = &bw
	}

	req := h.getReq(r)
	err := request(r, req)
	if err != nil {
		// if pipe is broken, there is no sense to write the header
		// in this case we just report about error
		if stderr.Is(err, errEPIPE) {
			req.Close(h.log, r)
			h.putReq(req)
			h.log.Error("write response error", zap.Time("start", start), zap.Duration("elapsed", time.Since(start)), zap.Error(err))
			return
		}

		req.Close(h.log, r)
		h.putReq(req)
		http.Error(w, errors.E(op, err).Error(), 500)
		h.log.Error("request forming error", zap.Time("start", start), zap.Duration("elapsed", time.Since(start)), zap.Error(err))
		return
	}

	req.Open(h.log, h.uploads.dir, h.uploads.forbid, h.uploads.allow)
	// get payload from the pool
	pld := h.getPld()

	err = req.Payload(pld)
	if err != nil {
		req.Close(h.log, r)
		h.putReq(req)
		h.putPld(pld)
		h.handleError(w, err)
		h.log.Error("payload forming error", zap.Time("start", start), zap.Duration("elapsed", time.Since(start)), zap.Error(err))
		return
	}

	resp := make(chan *payload.Payload, 1)
	errCh := h.getErrCh()
	defer h.putErrCh(errCh)
	var once bool

	go func() {
		// todo(rustatian): temporary solution, channel should be used to send a stop signal when connection was
		// broken on the RR side
		errS := h.pool.(pool.Streamer).ExecStream(pld, resp, make(chan struct{}))
		if errS != nil {
			errCh <- errS
			return
		}
	}()

	for {
		select {
		case e := <-errCh:
			req.Close(h.log, r)
			h.putReq(req)
			h.handleError(w, e)
			h.log.Error("execute", zap.Time("start", start), zap.Duration("elapsed", time.Since(start)), zap.Error(e))
			go func() {
				// read response till the end
				h.log.Warn("draining response, error occurred, worker is in use")
				for range resp {
				}
				h.log.Warn("draining response: finished, worker is ready to handle next request")
			}()
			return

		case p, cl := <-resp:
			if !cl {
				goto log
			}

			err = h.writeStream(p, w, once)
			if err != nil {
				req.Close(h.log, r)
				h.putReq(req)
				h.handleError(w, err)
				h.log.Error("execute", zap.Time("start", start), zap.Duration("elapsed", time.Since(start)), zap.Error(err))
				go func() {
					h.log.Warn("draining response, error occurred, worker is in use")
					// read response till the end
					for range resp {
					}

					h.log.Warn("draining response: finished, worker is ready to handle next request")
				}()
				return
			}

			once = true
		}
	}
	// log request after data have been written
log:
	switch h.accessLogs {
	case false:
		h.log.Info("http log",
			zap.Int("status", 200),
			zap.String("method", req.Method),
			zap.String("URI", req.URI),
			zap.String("remote_address", req.RemoteAddr),
			zap.Int("read_bytes", bw.read),
			zap.Time("start", start),
			zap.Duration("elapsed", time.Since(start)))
	case true:
		// external/cwe/cwe-117
		usrA := r.UserAgent()
		usrA = strings.ReplaceAll(usrA, "\n", "")
		usrA = strings.ReplaceAll(usrA, "\r", "")

		rfr := r.Referer()
		rfr = strings.ReplaceAll(rfr, "\n", "")
		rfr = strings.ReplaceAll(rfr, "\r", "")

		h.log.Info("http access log",
			zap.Int("read_bytes", bw.read),
			zap.Int("status", 200),
			zap.String("method", req.Method),
			zap.String("URI", req.URI),
			zap.String("remote_address", req.RemoteAddr),
			zap.String("query", req.RawQuery),
			zap.Int64("content_len", r.ContentLength),
			zap.String("host", r.Host),
			zap.String("user_agent", usrA),
			zap.String("referer", rfr),
			zap.String("time_local", time.Now().Format("02/Jan/06:15:04:05 -0700")),
			zap.Time("request_time", time.Now()),
			zap.Time("start", start),
			zap.Duration("elapsed", time.Since(start)))
	}

	h.putPld(pld)
	req.Close(h.log, r)
	h.putReq(req)
}

func (h *Handler) Dispose() {}

// handleError will handle internal RR errors and return 500
func (h *Handler) handleError(w http.ResponseWriter, err error) {
	if errors.Is(errors.NoFreeWorkers, err) {
		// set header for the prometheus
		w.Header().Set(noWorkers, trueStr)
		// write an internal server error
		w.WriteHeader(int(h.internalHTTPCode))
	}
	// internal error types, user should not see them
	if errors.Is(errors.SoftJob, err) ||
		errors.Is(errors.WatcherStopped, err) ||
		errors.Is(errors.WorkerAllocate, err) ||
		errors.Is(errors.ExecTTL, err) ||
		errors.Is(errors.IdleTTL, err) ||
		errors.Is(errors.TTL, err) ||
		errors.Is(errors.Encode, err) ||
		errors.Is(errors.Decode, err) ||
		errors.Is(errors.Network, err) {
		// write an internal server error
		w.WriteHeader(int(h.internalHTTPCode))
	}
}

func (h *Handler) putReq(req *Request) {
	req.RawQuery = ""
	req.RemoteAddr = ""
	req.Protocol = ""
	req.Method = ""
	req.URI = ""
	req.Header = nil
	req.Cookies = nil
	req.Attributes = nil
	req.Parsed = false
	req.body = nil
	h.reqPool.Put(req)
}

func (h *Handler) getReq(r *http.Request) *Request {
	req := h.reqPool.Get().(*Request)

	rq := r.URL.RawQuery
	rq = strings.ReplaceAll(rq, "\n", "")
	rq = strings.ReplaceAll(rq, "\r", "")

	req.RawQuery = rq
	req.RemoteAddr = FetchIP(r.RemoteAddr)
	req.Protocol = r.Proto
	req.Method = r.Method
	req.URI = URI(r)
	req.Header = r.Header
	req.Cookies = make(map[string]string)
	req.Attributes = attributes.All(r)

	req.Parsed = false
	req.body = nil
	return req
}

func (h *Handler) putRsp(rsp *Response) {
	rsp.Headers = nil
	rsp.Status = -1
	h.respPool.Put(rsp)
}

func (h *Handler) getRsp() *Response {
	return h.respPool.Get().(*Response)
}

func (h *Handler) putPld(pld *payload.Payload) {
	pld.Body = nil
	pld.Context = nil
	h.pldPool.Put(pld)
}

func (h *Handler) getPld() *payload.Payload {
	pld := h.pldPool.Get().(*payload.Payload)
	pld.Codec = frame.CodecJSON
	return pld
}
