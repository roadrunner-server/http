package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"tests/helpers"
	testplugins "tests/test_plugins"

	"github.com/roadrunner-server/gzip/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
)

func TestHTTPInit(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-init.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:15395"))
}

// A config without an http section boots and stops without an error, the plugin
// disables itself.
func TestHTTPNoConfigSection(t *testing.T) {
	rr, stop := helpers.Start(t, "configs/.rr-no-http.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	})

	stop()

	require.Empty(t, rr.Errs())
}

func TestHTTPAccessLogs(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-access-logs.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:58332"))

	helpers.AssertGet(t, "http://127.0.0.1:58332", 200, "hello world")
}

func TestHttpMiddleware(t *testing.T) {
	helpers.Start(t, "configs/.rr-http.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&testplugins.PluginMiddleware{},
		&testplugins.PluginMiddleware2{},
	}, helpers.WithProbe("http://127.0.0.1:18903"))

	helpers.AssertGet(t, "http://127.0.0.1:18903?hello=world", 201, "WORLD")
	// pluginMiddleware answers /halt itself, the request never reaches a worker
	helpers.AssertGet(t, "http://127.0.0.1:18903/halt", 500, "halted")
}

// The worker writes to stderr on every request; the response is unaffected.
func TestHttpEchoErr(t *testing.T) {
	cfg := `
rpc:
  listen: tcp://127.0.0.1:6003
  disabled: false

server:
  command: "php php_test_files/http/client.php echoerr pipes"
  relay: "pipes"
  relay_timeout: "20s"

http:
  debug: true
  address: 127.0.0.1:34999
  max_request_size: 1024
  middleware: [ "pluginMiddleware", "pluginMiddleware2" ]
  uploads:
    forbid: [ "" ]
  pool:
    num_workers: 2
    max_jobs: 0
    allocate_timeout: 60s
    destroy_timeout: 60s

logs:
  mode: development
  level: debug
`

	helpers.Start(t, "", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&testplugins.PluginMiddleware{},
		&testplugins.PluginMiddleware2{},
	}, helpers.WithInlineConfig(cfg), helpers.WithProbe("http://127.0.0.1:34999"))

	helpers.AssertGet(t, "http://127.0.0.1:34999?hello=world", 201, "WORLD")
}

func TestHTTPPost(t *testing.T) {
	helpers.Start(t, "configs/.rr-post-test.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:10084"))

	body, err := json.Marshal(struct {
		Name  string `json:"name"`
		Index int    `json:"index"`
	}{
		Name:  "foo",
		Index: 111,
	})
	require.NoError(t, err)

	// the first request carries no Content-Type, the rest declare JSON
	postEcho(t, "http://127.0.0.1:10084/", body, "")
	for range 20 {
		postEcho(t, "http://127.0.0.1:10084/", body, "application/json")
	}
}

// postEcho posts body to url and requires the worker to mirror it back.
func postEcho(t *testing.T, url string, body []byte, contentType string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, body, b)
}

// raw_body hands the worker the body exactly as it arrived: nothing is parsed out
// of it, uploads included.
func TestHTTPRawBody(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-raw-body.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:22377"))

	envelope, multipartType := rawMultipartForm(t)

	for _, tt := range []struct {
		name        string
		contentType string
		body        string
	}{
		{"urlencoded arrives as a literal string", urlEncoded, "a=1&b=2"},
		{"multipart arrives as its raw envelope", multipartType, envelope},
		{"json passes through untouched", "application/json", `{"k":"v"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:22377", strings.NewReader(tt.body))
			require.NoError(t, err)

			req.Header.Set("Content-Type", tt.contentType)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			b, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t, http.StatusOK, resp.StatusCode)
			// the worker writes back the body it was given, byte for byte
			require.Equal(t, tt.body, string(b))
			require.Equal(t, "0", resp.Header.Get("X-Uploads"))
		})
	}
}

// rawMultipartForm builds a multipart body with a single file part and returns it
// together with its content type.
func rawMultipartForm(t *testing.T) (string, string) {
	t.Helper()

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f, err := w.CreateFormFile("upload", "raw.txt")
	require.NoError(t, err)

	_, err = f.Write([]byte("file content"))
	require.NoError(t, err)

	require.NoError(t, w.Close())

	return mb.String(), w.FormDataContentType()
}

func TestHTTPIPv6Long(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-ipv6.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://[0:0:0:0:0:0:0:1]:10684"))

	helpers.AssertGet(t, "http://[0:0:0:0:0:0:0:1]:10684?hello=world", 201, "WORLD")
}

func TestHTTPIPv6Short(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-ipv6-2.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://[::1]:10784"))

	helpers.AssertGet(t, "http://[::1]:10784?hello=world", 201, "WORLD")
}

// A body over max_request_size (1MB here) is rejected before it reaches a worker.
func TestHTTPBigRequestSize(t *testing.T) {
	helpers.Start(t, "configs/.rr-big-req-size.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:10085"))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:10085?hello=world",
		strings.NewReader(strings.Repeat("a", 10*1024*1024)))
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	require.Equal(t, "serve_http: http: request body too large\n", string(b))
}

// Urlencoded bodies against three max_request_size settings.
func TestHTTPBigURLEncoded(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cfg      string
		addr     string
		bodySize int
		wantCode int
	}{
		{name: "limit1MB", cfg: "configs/.rr-http-urlencoded1.yaml", addr: "127.0.0.1:55777", bodySize: 11 * 1024 * 1024, wantCode: http.StatusRequestEntityTooLarge},
		{name: "limit30MB", cfg: "configs/.rr-http-urlencoded2.yaml", addr: "127.0.0.1:55778", bodySize: 28 * 1024 * 1024, wantCode: http.StatusOK},
		{name: "defaultLimit", cfg: "configs/.rr-http-urlencoded3.yaml", addr: "127.0.0.1:55779", bodySize: 11 * 1024 * 1024, wantCode: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			helpers.Start(t, tt.cfg, []any{
				&server.Plugin{},
				&gzip.Plugin{},
				&httpPlugin.Plugin{},
			}, helpers.WithProbe("http://"+tt.addr))

			body := "foo=" + strings.Repeat("a", tt.bodySize)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+tt.addr, strings.NewReader(body))
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t, tt.wantCode, resp.StatusCode)
		})
	}
}

// A status code the worker made up turns into a 500.
func TestHTTPNonExistingHTTPCode(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-code.yaml", []any{
		&server.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:44555"))

	code, _ := helpers.GetBody(t, "http://127.0.0.1:44555")
	require.Equal(t, 500, code)
}
