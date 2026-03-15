package handler

import (
	"net/http"
	"strings"

	httpV2proto "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/goridge/v4/pkg/frame"
	"github.com/roadrunner-server/http/v6/attributes"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/pool/payload"
)

func (h *Handler) getProtoReq(r *Request) *httpV2proto.HttpRequest {
	req := h.protoReqPool.Get().(*httpV2proto.HttpRequest)

	req.RemoteAddr = r.RemoteAddr
	req.Protocol = r.Protocol
	req.Method = r.Method
	req.Uri = r.URI
	req.Header = convert(r.Header)
	req.Cookies = convertCookies(r.Cookies)
	req.RawQuery = r.RawQuery
	req.Parsed = r.Parsed
	req.Attributes = convert(r.Attributes)

	return req
}

func (h *Handler) putProtoReq(req *httpV2proto.HttpRequest) {
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

func (h *Handler) putProtoRsp(rsp *httpV2proto.HttpResponse) {
	rsp.Headers = nil
	rsp.Status = -1
	h.protoRespPool.Put(rsp)
}

func (h *Handler) getProtoRsp() *httpV2proto.HttpResponse {
	return h.protoRespPool.Get().(*httpV2proto.HttpResponse)
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
