package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/handler"
	httpMw "github.com/roadrunner-server/http/v6/middleware"
	"github.com/roadrunner-server/http/v6/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tests/helpers"
	"tests/testLog"
)

// defaultCfg returns a Config suitable for direct Handler unit tests.
// Tweak it inline (e.g. shorter RequestTimeout) when a test needs it.
func defaultCfg() *config.Config {
	return &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Proxy: &config.Proxy{
			Address:        "127.0.0.1:0",
			RequestTimeout: 5 * time.Second,
			InboxSize:      16,
		},
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}
}

// handlerEnv bundles the per-test plumbing: queue + Handler + a local
// http.Server + the fake-worker stop func.
type handlerEnv struct {
	q    *proxy.Queue
	hs   *http.Server
	stop func()
}

func newHandlerEnv(t testing.TB, addr string, cfg *config.Config, respond helpers.Responder) *handlerEnv {
	t.Helper()
	q := proxy.NewQueue(cfg.Proxy.InboxSize)
	stop := helpers.StartFakeWorker(t.Context(), q, respond)

	h := handler.NewHandler(cfg, q, testLog.SlogLogger())

	hs := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: time.Minute}
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	// give the listener a moment to bind
	time.Sleep(10 * time.Millisecond)

	return &handlerEnv{q: q, hs: hs, stop: stop}
}

func (e *handlerEnv) close(t testing.TB) {
	t.Helper()
	e.stop()
	if err := e.hs.Shutdown(context.Background()); err != nil {
		t.Errorf("error during the shutdown: error %v", err)
	}
}

// ---------- common responders mimicking php_test_files/http/*.php ----------

// echoUpperResponder mimics echo.php — uppercase the ?hello= query param.
func echoUpperResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	u, _ := url.Parse(r.GetUri())
	v := ""
	if u != nil {
		v = u.Query().Get("hello")
	}
	return helpers.MakeResp(201, []byte(strings.ToUpper(v)), nil)
}

// headerResponder mimics header.php — uppercase Input header in body, echo
// ?hello= back as a "Header" response header.
func headerResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	in := firstHeader(r, "Input")
	hello := ""
	if u, _ := url.Parse(r.GetUri()); u != nil {
		hello = u.Query().Get("hello")
	}
	return helpers.MakeResp(200, []byte(strings.ToUpper(in)), map[string][]string{"Header": {hello}})
}

// userAgentResponder mimics user-agent.php — body is the User-Agent header.
func userAgentResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	return helpers.MakeResp(200, []byte(firstHeader(r, "User-Agent")), nil)
}

// cookieResponder mimics cookie.php — uppercase the `input` cookie in body
// and set an `output=cookie-output` Set-Cookie header.
func cookieResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	in := ""
	if c := r.GetCookies()["input"]; c != nil && len(c.GetValues()) > 0 {
		in = c.GetValues()[0]
	}
	return helpers.MakeResp(200, []byte(strings.ToUpper(in)),
		map[string][]string{"Set-Cookie": {"output=cookie-output"}})
}

// payloadFlipResponder mimics payload.php — JSON body in, k↔v swapped JSON out.
func payloadFlipResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	if firstHeader(r, "Content-Type") != "application/json" {
		return helpers.MakeResp(200, []byte("invalid content-type"), nil)
	}
	var p map[string]string
	if err := json.Unmarshal(r.GetBody(), &p); err != nil {
		return helpers.MakeResp(200, []byte(err.Error()), nil)
	}
	flipped := make(map[string]string, len(p))
	for k, v := range p {
		flipped[v] = k
	}
	out, _ := json.Marshal(flipped)
	return helpers.MakeResp(200, out, nil)
}

// rawEchoResponder mimics psr-worker-echo.php — body in, body out.
func rawEchoResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	return helpers.MakeResp(200, r.GetBody(), nil)
}

// urlencodedTreeResponder mimics data.php for urlencoded bodies — parses the
// raw form-encoded body Go-side into a nested PHP-style array and JSON-encodes it.
func urlencodedTreeResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	tree := helpers.DecodeURLEncodedTree(r.GetBody())
	out, _ := json.Marshal(tree)
	return helpers.MakeResp(200, out, nil)
}

// multipartEchoResponder mimics data.php for multipart — the handler has
// already parsed the multipart form fields into req.Body as JSON, so we just
// echo the body straight back.
func multipartEchoResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	return helpers.MakeResp(200, r.GetBody(), nil)
}

// errorResponder mimics error.php — return 500.
func errorResponder(_ *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	return helpers.MakeResp(500, nil, nil)
}

// dropResponder simulates a worker that takes the request but never produces
// a response — exercises the producer's RequestTimeout path.
func dropResponder(_ *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	return nil
}

// delayedEchoUpperResponder mimics echoDelay.php — sleep, then echo-upper.
func delayedEchoUpperResponder(d time.Duration) helpers.Responder {
	return func(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
		time.Sleep(d)
		return echoUpperResponder(r)
	}
}

// ipResponder mimics ip.php — body is the remote address as the plugin parsed it.
func ipResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	return helpers.MakeResp(200, []byte(r.GetRemoteAddr()), nil)
}

func firstHeader(r *httpV2.HttpHandlerRequest, name string) string {
	if h := r.GetHeader()[name]; h != nil && len(h.GetValues()) > 0 {
		return h.GetValues()[0]
	}
	return ""
}

// ---------- tests ----------

func TestHandler_Echo(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:19177", defaultCfg(), echoUpperResponder)
	defer env.close(t)

	body, r, err := helpers.Get("http://127.0.0.1:19177/?hello=world")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", body)
}

func TestHandler_Headers(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8078", defaultCfg(), headerResponder)
	defer env.close(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8078?hello=world", nil)
	require.NoError(t, err)
	req.Header.Add("input", "sample")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "world", r.Header.Get("Header"))
	assert.Equal(t, "SAMPLE", string(b))
}

func TestHandler_Empty_User_Agent(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:19658", defaultCfg(), userAgentResponder)
	defer env.close(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:19658?hello=world", nil)
	require.NoError(t, err)
	req.Header.Add("user-agent", "")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "", string(b))
}

func TestHandler_User_Agent(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:25688", defaultCfg(), userAgentResponder)
	defer env.close(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:25688?hello=world", nil)
	require.NoError(t, err)
	req.Header.Add("User-Agent", "go-agent")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "go-agent", string(b))
}

func TestHandler_Cookies(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8079", defaultCfg(), cookieResponder)
	defer env.close(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8079", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{
		Name:     "input",
		Value:    "input-value",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "INPUT-VALUE", string(b))

	for _, c := range r.Cookies() {
		assert.Equal(t, "output", c.Name)
		assert.Equal(t, "cookie-output", c.Value)
	}
}

func TestHandler_JsonPayload_POST(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8090", defaultCfg(), payloadFlipResponder)
	defer env.close(t)

	doJSONFlip(t, http.MethodPost, "http://127.0.0.1:8090")
}

func TestHandler_JsonPayload_PUT(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8081", defaultCfg(), payloadFlipResponder)
	defer env.close(t)

	doJSONFlip(t, http.MethodPut, "http://127.0.0.1:8081")
}

func TestHandler_JsonPayload_PATCH(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8082", defaultCfg(), payloadFlipResponder)
	defer env.close(t)

	doJSONFlip(t, http.MethodPatch, "http://127.0.0.1:8082")
}

func doJSONFlip(t *testing.T, method, url string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewBufferString(`{"key":"value"}`))
	require.NoError(t, err)
	req.Header.Add("Content-Type", "application/json")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, `{"value":"key"}`, string(b))
}

func TestHandler_UrlEncoded_POST_DELETE(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:10084", defaultCfg(), rawEchoResponder)
	defer env.close(t)

	bodyStr := "arr[x][y][e]=f&arr[c]p=l&arr[c]z=&key=value&name[]=name1&name[]=name2&name[]=name3&arr[x][y][z]=y"

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		req, err := http.NewRequestWithContext(t.Context(), method, "http://127.0.0.1:10084", strings.NewReader(bodyStr))
		require.NoError(t, err)
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

		r, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		b, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		require.NoError(t, err)

		assert.Equal(t, 200, r.StatusCode, "method %s", method)
		assert.Equal(t, bodyStr, string(b), "method %s", method)
	}
}

func TestHandler_FormData_POST(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:10094", defaultCfg(), urlencodedTreeResponder)
	defer env.close(t)

	res := doFormPost(t, http.MethodPost, "http://127.0.0.1:10094", standardForm())
	assertStandardFormTree(t, res, "value")
}

func TestHandler_FormData_POST_Overwrite(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8083", defaultCfg(), urlencodedTreeResponder)
	defer env.close(t)

	form := standardForm()
	form.Add("key", "value2") // duplicate key — last wins per PHP $_POST semantics

	res := doFormPost(t, http.MethodPost, "http://127.0.0.1:8083", form)
	assertStandardFormTree(t, res, "value2")
}

func TestHandler_FormData_POST_Form_UrlEncoded_Charset(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8085", defaultCfg(), urlencodedTreeResponder)
	defer env.close(t)

	form := standardForm()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:8085", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	require.NoError(t, json.Unmarshal(b, &res))
	assertStandardFormTree(t, res, "value")
}

func TestHandler_FormData_PUT(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:17834", defaultCfg(), urlencodedTreeResponder)
	defer env.close(t)

	res := doFormPost(t, http.MethodPut, "http://127.0.0.1:17834", standardForm())
	assertStandardFormTree(t, res, "value")
}

func TestHandler_FormData_PATCH(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8086", defaultCfg(), urlencodedTreeResponder)
	defer env.close(t)

	res := doFormPost(t, http.MethodPatch, "http://127.0.0.1:8086", standardForm())
	assertStandardFormTree(t, res, "value")
}

func standardForm() url.Values {
	form := url.Values{}
	form.Add("key", "value")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")
	return form
}

func doFormPost(t *testing.T, method, urlStr string, form url.Values) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, urlStr, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	require.NoError(t, json.Unmarshal(b, &res))
	return res
}

func assertStandardFormTree(t *testing.T, res map[string]any, expectedKey string) {
	t.Helper()
	assert.Equal(t, "l", res["arr"].(map[string]any)["c"].(map[string]any)["p"])
	assert.Equal(t, "", res["arr"].(map[string]any)["c"].(map[string]any)["z"])
	assert.Equal(t, "y", res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"])
	assert.Equal(t, "f", res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"])
	assert.Equal(t, expectedKey, res["key"])
	assert.Equal(t, []any{"name1", "name2", "name3"}, res["name"])
}

func TestHandler_Multipart_POST(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8019", defaultCfg(), multipartEchoResponder)
	defer env.close(t)

	res := doMultipartFormPost(t, http.MethodPost, "http://127.0.0.1:8019")
	assertStandardFormTree(t, res, "value")
}

func TestHandler_Multipart_PUT(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8020", defaultCfg(), multipartEchoResponder)
	defer env.close(t)

	res := doMultipartFormPost(t, http.MethodPut, "http://127.0.0.1:8020")
	assertStandardFormTree(t, res, "value")
}

func TestHandler_Multipart_PATCH(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:34432", defaultCfg(), multipartEchoResponder)
	defer env.close(t)

	res := doMultipartFormPost(t, http.MethodPatch, "http://127.0.0.1:34432")
	assertStandardFormTree(t, res, "value")
}

// TestHandler_NonMultipart_OversizeBody guards against the regression where a
// non-multipart body exceeding MaxRequestSize returned 400 instead of 413,
// because the original handleRequestErr's explicit MaxBytesError case had
// been collapsed into the 400 default. classifyParseErr now promotes the
// MaxBytesError on the io.ReadAll path back to 413.
func TestHandler_NonMultipart_OversizeBody(t *testing.T) {
	const maxBytes = 64
	cfg := defaultCfg()
	q := proxy.NewQueue(cfg.Proxy.InboxSize)
	stop := helpers.StartFakeWorker(t.Context(), q, multipartEchoResponder)
	t.Cleanup(stop)

	h := handler.NewHandler(cfg, q, testLog.SlogLogger())
	// Wrap with the same MaxRequestSize middleware the plugin applies in
	// production (init.go) — without it ReadAll never sees MaxBytesError.
	hs := &http.Server{
		Addr:              "127.0.0.1:8190",
		Handler:           httpMw.MaxRequestSize(h, maxBytes),
		ReadHeaderTimeout: time.Minute,
	}
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("listen: %v", err)
		}
	}()
	t.Cleanup(func() { _ = hs.Shutdown(context.Background()) })
	time.Sleep(10 * time.Millisecond)

	body := strings.Repeat("x", int(maxBytes)*4)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:8190/", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	assert.Equal(t, http.StatusRequestEntityTooLarge, r.StatusCode)
}

// TestHandler_Multipart_SemicolonInQuery covers issue #2353: a malformed
// query string causes ParseMultipartForm (which internally parses the URL
// query) to fail with "invalid semicolon separator in query". The response
// must be 400 Bad Request, not the historical 500.
func TestHandler_Multipart_SemicolonInQuery(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8189", defaultCfg(), multipartEchoResponder)
	defer env.close(t)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)
	require.NoError(t, w.WriteField("key", "value"))
	require.NoError(t, w.Close())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:8189/?a=b;c", &mb)
	require.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, r.StatusCode)
}

func doMultipartFormPost(t *testing.T, method, urlStr string) map[string]any {
	t.Helper()
	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)
	for _, kv := range []struct{ k, v string }{
		{"key", "value"},
		{"key", "value"}, // duplicate — PHP $_POST keeps the last
		{"name[]", "name1"},
		{"name[]", "name2"},
		{"name[]", "name3"},
		{"arr[x][y][z]", "y"},
		{"arr[x][y][e]", "f"},
		{"arr[c]p", "l"},
		{"arr[c]z", ""},
	} {
		require.NoError(t, w.WriteField(kv.k, kv.v))
	}
	require.NoError(t, w.Close())

	req, err := http.NewRequestWithContext(t.Context(), method, urlStr, &mb)
	require.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	require.NoError(t, json.Unmarshal(b, &res))
	return res
}

func TestHandler_Error(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8177", defaultCfg(), errorResponder)
	defer env.close(t)

	_, r, err := helpers.Get("http://127.0.0.1:8177/?hello=world")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	assert.Equal(t, 500, r.StatusCode)
}

// TestHandler_Error2 originally tested PHP's `exit()` mid-request, which
// crashed the in-process worker. Workers are external now, so the equivalent
// failure mode is "worker never responds" — RequestTimeout fires and the
// client should get exactly 504 Gateway Timeout.
func TestHandler_Error2(t *testing.T) {
	cfg := defaultCfg()
	cfg.Proxy.RequestTimeout = 2 * time.Second // keep the test snappy

	env := newHandlerEnv(t, "127.0.0.1:8178", cfg, dropResponder)
	defer env.close(t)

	_, r, err := helpers.Get("http://127.0.0.1:8178/?hello=world")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	assert.Equal(t, http.StatusGatewayTimeout, r.StatusCode)
}

func TestHandler_ResponseDuration(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8180", defaultCfg(), echoUpperResponder)
	defer env.close(t)

	body, r, err := helpers.Get("http://127.0.0.1:8180/?hello=world")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", body)
}

// TestHandler_ResponseDurationDelayed originally measured PHP-side sleep(1).
// We sleep on the Go responder side; the producer's wait covers it identically.
func TestHandler_ResponseDurationDelayed(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8181", defaultCfg(), delayedEchoUpperResponder(time.Second))
	defer env.close(t)

	start := time.Now()
	body, r, err := helpers.Get("http://127.0.0.1:8181/?hello=world")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", body)
	assert.GreaterOrEqual(t, time.Since(start), time.Second)
}

func TestHandler_ErrorDuration(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8182", defaultCfg(), errorResponder)
	defer env.close(t)

	_, r, err := helpers.Get("http://127.0.0.1:8182/?hello=world")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	assert.Equal(t, 500, r.StatusCode)
}

func TestHandler_IP(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:8183", defaultCfg(), ipResponder)
	defer env.close(t)

	body, r, err := helpers.Get("http://127.0.0.1:8183/")
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "127.0.0.1", body)
}

func BenchmarkHandler_Listen_Echo(b *testing.B) {
	env := newHandlerEnv(b, "127.0.0.1:8188", defaultCfg(), echoUpperResponder)
	defer env.close(b)

	req, err := http.NewRequestWithContext(b.Context(), http.MethodGet, "http://127.0.0.1:8188/?hello=world", nil)
	require.NoError(b, err)
	client := &http.Client{}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		r, err := client.Do(req)
		require.NoError(b, err)
		body, err := io.ReadAll(r.Body)
		require.NoError(b, err)
		_ = r.Body.Close()
		if string(body) != "WORLD" {
			b.Fail()
		}
	}
}
