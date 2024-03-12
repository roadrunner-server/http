package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	httpV1Beta "github.com/roadrunner-server/api/v4/build/http/v1beta"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/sdk/v4/payload"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

const (
	defaultMaxMemory = 32 << 20 // 32 MB
	contentNone      = iota + 900
	contentStream
	contentMultipart
	contentURLEncoded
)

// Request maps net/http requests to PSR7 compatible structure and managed state of temporary uploaded files.
type Request struct {
	// RemoteAddr contains ip address of a client, make sure to check X-Real-Ip and X-Forwarded-For for real client address.
	RemoteAddr string `json:"remoteAddr"`
	// Protocol includes HTTP protocol version.
	Protocol string `json:"protocol"`
	// Method contains the name of HTTP method used for the request.
	Method string `json:"method"`
	// URI contains full request URI with a scheme and query.
	URI string `json:"uri"`
	// Header contains list of request headers.
	Header http.Header `json:"headers"`
	// Cookies contains list of request cookies.
	Cookies map[string]string `json:"cookies"`
	// RawQuery contains non-parsed query string (to be parsed on php end).
	RawQuery string `json:"rawQuery"`
	// Parsed indicates that request body has been parsed on RR end.
	Parsed bool `json:"parsed"`
	// Uploads contain a list of uploaded files, their names, sized and associations with temporary files.
	Uploads *Uploads `json:"uploads"`
	// Attributes can be set by chained mdwr to safely pass value from Golang to PHP. See: GetAttribute, SetAttribute functions.
	Attributes map[string][]string `json:"attributes"`
	// request body can be parsedData or []byte
	body any
}

func FetchIP(pair string, log *zap.Logger) string {
	if !strings.ContainsRune(pair, ':') {
		return pair
	}

	addr, _, err := net.SplitHostPort(pair)
	if err == nil {
		return addr
	}

	ip := net.ParseIP(pair)
	if ip == nil {
		log.Warn("remote address parsing failure", zap.Error(err)) // error from the SplitHostPort
		return ""
	}

	return ip.String()
}

func request(r *http.Request, req *Request, uid, gid int, sendRawBody bool) error {
	for _, c := range r.Cookies() {
		if v, err := url.QueryUnescape(c.Value); err == nil {
			req.Cookies[c.Name] = v
		}
	}

	switch req.contentType() {
	case contentNone:
		return nil

	case contentStream:
		var err error
		req.body, err = io.ReadAll(r.Body)
		if err != nil {
			return err
		}

		return nil

	case contentMultipart:
		if sendRawBody {
			var err error
			req.body, err = io.ReadAll(r.Body)
			if err != nil {
				return err
			}

			return nil
		}

		err := r.ParseMultipartForm(defaultMaxMemory)
		if err != nil {
			return err
		}

		req.Uploads, err = parseUploads(r, uid, gid)
		if err != nil {
			return err
		}

		req.body, err = parseMultipartData(r)
		if err != nil {
			return err
		}

		req.Parsed = true
	case contentURLEncoded:
		if sendRawBody {
			var err error
			req.body, err = io.ReadAll(r.Body)
			if err != nil {
				return err
			}

			return nil
		}

		err := r.ParseForm()
		if err != nil {
			return err
		}

		req.body, err = parsePostForm(r)
		if err != nil {
			return err
		}
	}

	req.Parsed = true
	return nil
}

// Open moves all uploaded files to temporary directory so it can be given to php later.
func (r *Request) Open(log *zap.Logger, dir string, forbid, allow map[string]struct{}) {
	if r.Uploads == nil {
		return
	}

	r.Uploads.Open(log, dir, forbid, allow)
}

// Close clears all temp file uploads
func (r *Request) Close(log *zap.Logger, hr *http.Request) {
	if r.Uploads == nil {
		return
	}

	r.Uploads.Clear(log)
	if hr.MultipartForm != nil {
		_ = hr.MultipartForm.RemoveAll()
	}
}

// Payload request marshaled RoadRunner payload based on PSR7 data. values encode method is JSON. Make sure to open
// files prior to calling this method.
func (r *Request) Payload(p *payload.Payload, sendRawBody bool, req *httpV1Beta.Request) error {
	const op = errors.Op("marshal_payload")

	var err error
	p.Context, err = proto.Marshal(req)
	if err != nil {
		return errors.E(op, err)
	}

	// if user wanted to get a raw body, just send it
	if sendRawBody {
		// should always
		switch raw := r.body.(type) {
		case []byte:
			err = packRaw(p, raw)
			if err != nil {
				return errors.E(op, err)
			}

			return nil
		default:
			return errors.E(op, errors.Errorf("type is not []byte: %T", raw))
		}
	}

	// check if body was already parsed
	if r.Parsed {
		switch bdy := r.body.(type) {
		case []byte:
			err = packRaw(p, bdy)
			if err != nil {
				return errors.E(op, err)
			}

			return nil
		case dataTree:
			err = packDataTree(bdy, p)
			if err != nil {
				return errors.E(op, err)
			}

			return nil
		default:
			return errors.E(op, errors.Errorf("unknown body type: %T", bdy))
		}
	}

	// assume raw, but check
	if r.body != nil {
		switch t := r.body.(type) {
		case []byte:
			err = packRaw(p, t)
			if err != nil {
				return errors.E(op, err)
			}
		case dataTree:
			err = packDataTree(t, p)
			if err != nil {
				return errors.E(op, err)
			}
		default:
			return errors.Errorf("unknown body type: %T", t)
		}
	}

	return nil
}

// contentType returns the payload content type.
func (r *Request) contentType() int {
	if r.Method == "HEAD" || r.Method == "OPTIONS" {
		return contentNone
	}

	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		return contentURLEncoded
	}

	if strings.Contains(ct, "multipart/form-data") {
		return contentMultipart
	}

	return contentStream
}

// URI fetches full uri from request in a form of string (including https scheme if TLS connection is enabled).
func URI(r *http.Request) string {
	// CWE: https://github.com/spiral/roadrunner-plugins/pull/184/checks?check_run_id=4635904339
	uri := r.URL.String()
	uri = strings.ReplaceAll(uri, "\n", "")
	uri = strings.ReplaceAll(uri, "\r", "")

	if r.URL.Host != "" {
		return uri
	}

	if r.TLS != nil {
		return fmt.Sprintf("https://%s%s", r.Host, uri)
	}

	return fmt.Sprintf("http://%s%s", r.Host, uri)
}

func packRaw(p *payload.Payload, body []byte) error {
	bd := &httpV1Beta.Body{
		Data: &httpV1Beta.Body_Body{
			Body: body,
		},
	}

	var err error
	p.Body, err = proto.Marshal(bd)
	if err != nil {
		return err
	}

	return nil
}

func packDataTree(t dataTree, p *payload.Payload) error {
	bodyHeader := &httpV1Beta.Body_Header{
		Header: &httpV1Beta.Header{
			Header: make(map[string]*httpV1Beta.HeaderValue),
		},
	}

	for k, v := range t {
		switch t := v.(type) {
		case string:
			if bodyHeader.Header.Header[k] == nil {
				bodyHeader.Header.Header[k] = &httpV1Beta.HeaderValue{
					Value: make([]string, 0, 2),
				}
			}

			bodyHeader.Header.Header[k].Value = append(bodyHeader.Header.Header[k].Value, t)
		case []string:
			if bodyHeader.Header.Header[k] == nil {
				bodyHeader.Header.Header[k] = &httpV1Beta.HeaderValue{
					Value: make([]string, 0, 2),
				}
			}

			bodyHeader.Header.Header[k].Value = append(bodyHeader.Header.Header[k].Value, t...)
		}
	}

	bd := &httpV1Beta.Body{
		Data: bodyHeader,
	}

	var err error
	p.Body, err = proto.Marshal(bd)
	if err != nil {
		return err
	}

	return nil
}
