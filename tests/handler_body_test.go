package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"tests/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// urlEncoded is the content type the form-data tests post.
const urlEncoded = "application/x-www-form-urlencoded"

func TestHandler_JsonPayload(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "payload", "pipes"}, nil, nil)

	// the worker swaps the key with the value
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			code, body := sendBody(t, method, s.URL, "application/json", strings.NewReader(`{"key":"value"}`))

			assert.Equal(t, 200, code)
			assert.Equal(t, `{"value":"key"}`, body)
		})
	}
}

func TestHandler_UrlEncoded_POST_DELETE(t *testing.T) {
	cfg := helpers.DefaultHandlerConfig()
	cfg.RawBody = true

	s := helpers.ServeHandler(t, []string{"php_test_files/workers/psr-worker-echo.php"}, cfg, nil)

	const body = "arr[x][y][e]=f&arr[c]p=l&arr[c]z=&key=value&name[]=name1&name[]=name2&name[]=name3&arr[x][y][z]=y"

	// raw_body hands the worker the body unparsed, so it comes back verbatim
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			code, got := sendBody(t, method, s.URL, urlEncoded, strings.NewReader(body))

			assert.Equal(t, 200, code)
			assert.Equal(t, body, got)
		})
	}
}

func TestHandler_FormData(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "data", "pipes"}, nil, nil)

	for _, tt := range []struct {
		name        string
		method      string
		contentType string
		// duplicateKey posts "key" twice, so the last value has to win
		duplicateKey bool
		wantKey      string
	}{
		{"POST", http.MethodPost, urlEncoded, false, "value"},
		{"POST with a duplicate key", http.MethodPost, urlEncoded, true, "value2"},
		{"POST with a charset", http.MethodPost, urlEncoded + "; charset=UTF-8", false, "value"},
		{"PUT", http.MethodPut, urlEncoded, false, "value"},
		{"PATCH", http.MethodPatch, urlEncoded, false, "value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			form := formValues()
			if tt.duplicateKey {
				form.Add("key", "value2")
			}

			code, body := sendBody(t, tt.method, s.URL, tt.contentType, strings.NewReader(form.Encode()))

			assert.Equal(t, 200, code)
			assertParsedForm(t, body, tt.wantKey)
		})
	}
}

func TestHandler_Multipart(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "data", "pipes"}, nil, nil)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			mb, contentType := multipartForm(t)

			code, body := sendBody(t, method, s.URL, contentType, mb)

			assert.Equal(t, 200, code)
			assertParsedForm(t, body, "value")
		})
	}
}

// formValues is the form the data worker parses: a flat key, a repeated name[]
// array and nested arr[...] keys.
func formValues() url.Values {
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

// multipartForm writes the same fields as formValues into a multipart body and
// returns it with its content type. "key" is written twice with the same value.
func multipartForm(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	for _, f := range []struct{ name, value string }{
		{"key", "value"},
		{"key", "value"},
		{"name[]", "name1"},
		{"name[]", "name2"},
		{"name[]", "name3"},
		{"arr[x][y][z]", "y"},
		{"arr[x][y][e]", "f"},
		{"arr[c]p", "l"},
		{"arr[c]z", ""},
	} {
		require.NoError(t, w.WriteField(f.name, f.value))
	}

	require.NoError(t, w.Close())

	return &mb, w.FormDataContentType()
}

// assertParsedForm checks the form the data worker echoes back as JSON.
func assertParsedForm(t *testing.T, body, wantKey string) {
	t.Helper()

	var res map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &res))

	arr := res["arr"].(map[string]any)

	c := arr["c"].(map[string]any)
	assert.Equal(t, "l", c["p"])
	assert.Equal(t, "", c["z"])

	y := arr["x"].(map[string]any)["y"].(map[string]any)
	assert.Equal(t, "y", y["z"])
	assert.Equal(t, "f", y["e"])

	assert.Equal(t, wantKey, res["key"])
	assert.Equal(t, []any{"name1", "name2", "name3"}, res["name"])
}

// sendBody sends body with the given method and content type and returns the
// response status code and body.
func sendBody(t *testing.T, method, url, contentType string, body io.Reader) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, body)
	require.NoError(t, err)

	req.Header.Set("Content-Type", contentType)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = r.Body.Close()
	}()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	return r.StatusCode, string(b)
}
