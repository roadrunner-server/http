package handler

import (
	"net/http"
	"strings"

	httpV1Beta "github.com/roadrunner-server/api/v4/build/http/v1beta"
	"github.com/roadrunner-server/goridge/v3/pkg/frame"
	"github.com/roadrunner-server/http/v4/attributes"
	"github.com/roadrunner-server/http/v4/config"
	"github.com/roadrunner-server/sdk/v4/payload"
)

func (h *Handler) getProtoReq(r *Request) *httpV1Beta.Request {
	req := h.protoReqPool.Get().(*httpV1Beta.Request)

	req.RemoteAddr = r.RemoteAddr
	req.Protocol = r.Protocol
	req.Method = r.Method
	req.Uri = r.URI
	req.Header = convert(r.Header)
	req.Cookies = convertCookies(r.Cookies)
	req.RawQuery = r.RawQuery
	req.Parsed = r.Parsed
	req.Uploads = convertUploads(r.Uploads)
	req.Attributes = convert(r.Attributes)

	return req
}

func (h *Handler) putProtoReq(req *httpV1Beta.Request) {
	req.RemoteAddr = ""
	req.Protocol = ""
	req.Method = ""
	req.Uri = ""
	req.Header = nil
	req.Cookies = nil
	req.RawQuery = ""
	req.Parsed = false
	req.Uploads = nil
	req.Attributes = nil

	h.protoReqPool.Put(req)
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

func (h *Handler) putProtoRsp(rsp *httpV1Beta.Response) {
	rsp.Headers = nil
	rsp.Status = -1
	h.protoRespPool.Put(rsp)
}

func (h *Handler) getProtoRsp() *httpV1Beta.Response {
	return h.protoRespPool.Get().(*httpV1Beta.Response)
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
