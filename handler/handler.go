package handler

import (
	"context"
	stderr "errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/roadrunner-server/http/v4/common"

	httpV1proto "github.com/roadrunner-server/api/v4/build/http/v1"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/goridge/v3/pkg/frame"
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
	reqPool       sync.Pool
	protoRespPool sync.Pool
	protoReqPool  sync.Pool
	pldPool       sync.Pool
	stopChPool    sync.Pool
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
		protoRespPool: sync.Pool{
			New: func() any {
				return &httpV1proto.Response{
					Headers: make(map[string]*httpV1proto.HeaderValue),
					Status:  -1,
				}
			},
		},
		protoReqPool: sync.Pool{
			New: func() any {
				return &httpV1proto.Request{}
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
	// get proto request from the pool
	reqproto := h.getProtoReq(req)
	err = req.Payload(pld, h.sendRawBody, reqproto)
	h.putProtoReq(reqproto)
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
