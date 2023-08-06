package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/goccy/go-json"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/sdk/v4/payload"
	"go.uber.org/zap"
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
	// RemoteAddr contains ip address of client, make sure to check X-Real-Ip and X-Forwarded-For for real client address.
	RemoteAddr string `json:"remoteAddr"`

	// Protocol includes HTTP protocol version.
	Protocol string `json:"protocol"`

	// Method contains name of HTTP method used for the request.
	Method string `json:"method"`

	// URI contains full request URI with scheme and query.
	URI string `json:"uri"`

	// Header contains list of request headers.
	Header http.Header `json:"headers"`

	// Cookies contains list of request cookies.
	Cookies map[string]string `json:"cookies"`

	// RawQuery contains non parsed query string (to be parsed on php end).
	RawQuery string `json:"rawQuery"`

	// Parsed indicates that request body has been parsed on RR end.
	Parsed bool `json:"parsed"`

	// Uploads contains list of uploaded files, their names, sized and associations with temporary files.
	Uploads *Uploads `json:"uploads"`

	// Attributes can be set by chained mdwr to safely pass value from Golang to PHP. See: GetAttribute, SetAttribute functions.
	Attributes map[string]any `json:"attributes"`

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
			b, err := io.ReadAll(r.Body)
			if err != nil {
				return err
			}

			data, err := url.QueryUnescape(bytesToStr(b))
			if err != nil {
				return err
			}

			req.body = strToBytes(data)
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
func (r *Request) Payload(p *payload.Payload, sendRawBody bool) error {
	const op = errors.Op("marshal_payload")

	var err error
	p.Context, err = json.MarshalWithOption(r, json.UnorderedMap())
	if err != nil {
		return err
	}

	// if user wanted to get a raw body, just send it
	if sendRawBody {
		p.Body = r.body.([]byte)
		return nil
	}

	// check if body was already parsed
	if r.Parsed {
		p.Body, err = json.MarshalWithOption(r.body, json.UnorderedMap())
		if err != nil {
			return errors.E(op, errors.Encode, err)
		}

		return nil
	}

	if r.body != nil {
		p.Body = r.body.([]byte)
	}

	return nil
}

// contentType returns the payload content type.
func (r *Request) contentType() int {
	if r.Method == "HEAD" || r.Method == "OPTIONS" {
		return contentNone
	}

	ct := r.Header.Get("content-type")
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
