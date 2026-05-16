package helpers

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"

	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/http/v6/proxy"
)

// Responder produces a response for an incoming request. Returning nil drops
// the request — useful for exercising the producer's timeout / cancellation
// path without actually crashing a worker.
type Responder func(*httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse

// StartFakeWorker pulls requests off q in a loop and delivers responses
// produced by respond. Exits when ctx is canceled or the queue is closed.
// The returned stop function cancels and waits for the loop to exit.
func StartFakeWorker(ctx context.Context, q *proxy.Queue, respond Responder) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			req, err := q.Next(ctx)
			if err != nil {
				return
			}
			resp := respond(req)
			if resp == nil {
				continue
			}
			resp.Id = req.GetId()
			q.Deliver(req.GetId(), resp)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// MakeResp is a small constructor for HttpHandlerResponse used by responders.
func MakeResp(status int32, body []byte, headers map[string][]string) *httpV2.HttpHandlerResponse {
	resp := &httpV2.HttpHandlerResponse{Status: int64(status), Body: body}
	if len(headers) > 0 {
		resp.Headers = make(map[string]*httpV2.HttpHeaderValue, len(headers))
		for k, v := range headers {
			resp.Headers[k] = &httpV2.HttpHeaderValue{Values: v}
		}
	}
	return resp
}

// DecodeURLEncodedTree parses a PHP-style bracket-notation URL-encoded body
// into a nested map. Mirrors what PHP's $_POST + RR's PSR-7 worker would have
// surfaced for `data.php`:
//
//	arr[x][y]=v&name[]=a&name[]=b
//	→ {"arr": {"x": {"y": "v"}}, "name": ["a", "b"]}
//
// Same segmenting algorithm as handler/parse.go's fetchIndexes — duplicated
// here to keep the test harness independent of plugin internals.
func DecodeURLEncodedTree(body []byte) map[string]any {
	tree := make(map[string]any)
	parsed, _ := url.ParseQuery(string(body))
	for rawKey, vals := range parsed {
		keys := splitBracketKey(rawKey)
		insertTree(tree, keys, vals)
	}
	return tree
}

func splitBracketKey(s string) []string {
	// 3-state machine matching handler/parse.go's fetchIndexes:
	// stNormal=outside brackets in text; stOpen=just saw '['; stClose=just
	// saw ']'. Only append the buffer on '[' when we were in stNormal — this
	// avoids inserting an empty segment between adjacent brackets ("][").
	const (
		stNormal = iota
		stOpen
		stClose
	)
	keys := make([]string, 0, 4)
	var buf strings.Builder
	state := stNormal
	for _, c := range s {
		switch c {
		case ' ':
			continue
		case '[':
			if state == stNormal {
				keys = append(keys, buf.String())
				buf.Reset()
			}
			state = stOpen
		case ']':
			keys = append(keys, buf.String())
			buf.Reset()
			state = stClose
		default:
			buf.WriteRune(c)
			state = stNormal
		}
	}
	if buf.Len() > 0 || len(keys) == 0 {
		keys = append(keys, buf.String())
	}
	return keys
}

func insertTree(tree map[string]any, keys []string, vals []string) {
	for len(keys) > 0 {
		// Non-associative array: "name[]" → ["name", ""] terminal.
		if len(keys) == 2 && keys[1] == "" {
			anySlice := make([]any, len(vals))
			for i, v := range vals {
				anySlice[i] = v
			}
			tree[keys[0]] = anySlice
			return
		}
		// Scalar leaf: keys has exactly one segment left.
		if len(keys) == 1 {
			if len(vals) == 0 {
				tree[keys[0]] = ""
			} else {
				tree[keys[0]] = vals[len(vals)-1] // last wins, mirrors PHP $_POST
			}
			return
		}
		// Descend, creating an empty map if needed.
		next, ok := tree[keys[0]].(map[string]any)
		if !ok {
			next = make(map[string]any)
			tree[keys[0]] = next
		}
		tree = next
		keys = keys[1:]
	}
}

// DecodeUploadsTree decodes req.Uploads JSON into a nested tree. Leaves are
// map[string]any with FileUpload-shaped keys (name, mime, size, error, tmpName);
// branches are nested map[string]any. Callers walk the tree and inspect leaves
// by key.
func DecodeUploadsTree(uploads []byte) (map[string]any, error) {
	if len(uploads) == 0 {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(uploads, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// HashFileSHA512 returns hex-encoded SHA-512 of the file at path.
func HashFileSHA512(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
