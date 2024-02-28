package handler

import (
	"context"
	stderr "errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/roadrunner-server/http/v4/common"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/goridge/v3/pkg/frame"
	"github.com/roadrunner-server/http/v4/attributes"
	"github.com/roadrunner-server/http/v4/config"
	"github.com/roadrunner-server/sdk/v4/payload"
	"go.uber.org/zap"
)

const (
	noWorkers string = "No-Workers"
	trueStr   string = "true"
)

var _ http.Handler = (*Handler)(nil)

type uploads struct {
	dir    string
	allow  map[string]struct{}
	forbid map[string]struct{}
}

// Handler serves http connections to underlying PHP application using PSR-7 protocol. Context will include request headers,
// parsed files and query, payload will include parsed form dataTree (if any).
type Handler struct {
	uploads     *uploads
	log         *zap.Logger
	pool        common.Pool
	internalCtx context.Context

	internalHTTPCode uint64
	sendRawBody      bool
	debugMode        bool

	// permissions
	uid int
	gid int

	// internal
	reqPool    sync.Pool
	respPool   sync.Pool
	pldPool    sync.Pool
	stopChPool sync.Pool
}

// NewHandler return handle interface implementation
func NewHandler(cfg *config.Config, pool common.Pool, log *zap.Logger) (*Handler, error) {
	return &Handler{
		uploads: &uploads{
			dir:    cfg.Uploads.Dir,
			allow:  cfg.Uploads.Allowed,
			forbid: cfg.Uploads.Forbidden,
		},
		pool:             pool,
		debugMode:        checkDebug(cfg),
		log:              log,
		internalHTTPCode: cfg.InternalErrorCode,
		sendRawBody:      cfg.RawBody,
		internalCtx:      context.Background(),

		// permissions
		uid: cfg.UID,
		gid: cfg.GID,

		stopChPool: sync.Pool{
			New: func() any {
				return make(chan struct{}, 1)
			},
		},
		reqPool: sync.Pool{
			New: func() any {
				return &Request{
					Attributes: make(map[string][]string),
					Cookies:    make(map[string]string),
					body:       nil,
				}
			},
		},
		respPool: sync.Pool{
			New: func() any {
				return &Response{
					Headers: make(map[string][]string),
					Status:  -1,
				}
			},
		},
		pldPool: sync.Pool{
			New: func() any {
				return &payload.Payload{
					Body:    make([]byte, 0, 100),
					Context: make([]byte, 0, 100),
					Codec:   frame.CodecProto,
				}
			},
		},
	}, nil
}

// ServeHTTP transform original request to the PSR-7 passed then to the underlying application. Attempts to serve static files first if enabled.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const op = errors.Op("serve_http")
	start := time.Now()

	req := h.getReq(r)
	err := request(r, req, h.uid, h.gid, h.sendRawBody)
	if err != nil {
		// if the pipe is broken, there is no sense to write the header
		// in this case, we just report about error
		if stderr.Is(err, errEPIPE) {
			req.Close(h.log, r)
			h.putReq(req)
			h.log.Error(
				"write response error",
				zap.Time("start", start),
				zap.Int64("elapsed", time.Since(start).Milliseconds()),
				zap.Error(err),
			)
			return
		}

		req.Close(h.log, r)
		h.putReq(req)
		http.Error(w, errors.E(op, err).Error(), 500)
		h.log.Error(
			"request forming error",
			zap.Time("start", start),
			zap.Int64("elapsed", time.Since(start).Milliseconds()),
			zap.Error(err),
		)
		return
	}

	req.Open(h.log, h.uploads.dir, h.uploads.forbid, h.uploads.allow)
	// get payload from the pool
	pld := h.getPld()

	err = req.Payload(pld, h.sendRawBody)
	if err != nil {
		req.Close(h.log, r)
		h.putReq(req)
		h.putPld(pld)
		h.handleError(w, err)
		h.log.Error(
			"payload forming error",
			zap.Time("start", start),
			zap.Int64("elapsed", time.Since(start).Milliseconds()),
			zap.Error(err),
		)
		return
	}

	stopCh := h.getCh()
	wResp, err := h.pool.Exec(h.internalCtx, pld, stopCh)
	if err != nil {
		req.Close(h.log, r)
		h.putReq(req)
		h.putPld(pld)
		h.putCh(stopCh)
		h.handleError(w, err)
		h.log.Error("execute", zap.Time("start", start), zap.Int64("elapsed", time.Since(start).Milliseconds()), zap.Error(err))
		return
	}
	// return payload to the pool
	h.putPld(pld)

	for recv := range wResp {
		if recv.Error() != nil {
			req.Close(h.log, r)
			h.putReq(req)
			h.putCh(stopCh)
			w.WriteHeader(int(h.internalHTTPCode))
			h.log.Error("read stream",
				zap.Time("start", start),
				zap.Int64("elapsed", time.Since(start).Milliseconds()),
				zap.Error(recv.Error()))
			return
		}

		err = h.Write(recv.Payload(), w)
		if err != nil {
			// send stop signal to the worker pool
			select {
			case stopCh <- struct{}{}:
			default:
			}

			// we should not exit from the loop here, since after sending close signal, it should be closed from the SDK side
			h.log.Error("write response (chunk) error",
				zap.Time("start", start),
				zap.Int64("elapsed", time.Since(start).Milliseconds()),
				zap.Error(err))
		}
	}

	req.Close(h.log, r)
	h.putReq(req)
	h.putCh(stopCh)
}

// handleError will handle internal RR errors and return 500
func (h *Handler) handleError(w http.ResponseWriter, err error) {
	// write an internal server error
	w.WriteHeader(int(h.internalHTTPCode))

	// if there are no free workers -> write a special header
	if errors.Is(errors.NoFreeWorkers, err) {
		// set header for the prometheus
		w.Header().Set(noWorkers, trueStr)
	}

	// in debug mode, write all output into the browser/curl/any_tool
	if h.debugMode {
		_, _ = fmt.Fprintln(w, err)
	}
}

func (h *Handler) putReq(req *Request) {
	req.RemoteAddr = ""
	req.Protocol = ""
	req.Method = ""
	req.URI = ""
	req.Header = nil
	req.Cookies = nil
	req.RawQuery = ""
	req.Parsed = false
	req.Uploads = nil
	req.Attributes = nil
	req.body = nil

	h.reqPool.Put(req)
}

func (h *Handler) getReq(r *http.Request) *Request {
	req := h.reqPool.Get().(*Request)

	rq := r.URL.RawQuery
	rq = strings.ReplaceAll(rq, "\n", "")
	rq = strings.ReplaceAll(rq, "\r", "")

	req.RawQuery = rq
	req.RemoteAddr = FetchIP(r.RemoteAddr, h.log)
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
	pld.Codec = frame.CodecProto
	return pld
}

func (h *Handler) getCh() chan struct{} {
	return h.stopChPool.Get().(chan struct{})
}

func (h *Handler) putCh(ch chan struct{}) {
	// just check if the chan is not empty
	select {
	case <-ch:
	default:
	}
	h.stopChPool.Put(ch)
}

func checkDebug(cfg *config.Config) bool {
	if cfg != nil && cfg.Pool != nil {
		return cfg.Pool.Debug
	}

	return false
}
