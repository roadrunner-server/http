package tests

import (
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"

	"tests/helpers"

	"github.com/roadrunner-server/pool/v2/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Echo(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "echo", "pipes"}, nil, nil)

	helpers.AssertGet(t, s.URL+"/?hello=world", 201, "WORLD")
}

func TestHandler_Headers(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "header", "pipes"}, nil, nil)

	// the worker uppercases the input header and mirrors the query into a response header
	r := getResponse(t, s.URL+"/?hello=world", func(req *http.Request) {
		req.Header.Add("input", "sample")
	})

	assert.Equal(t, 200, r.code)
	assert.Equal(t, "world", r.header.Get("Header"))
	assert.Equal(t, "SAMPLE", r.body)
}

func TestHandler_User_Agent(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "user-agent", "pipes"}, nil, nil)

	for _, tt := range []struct {
		name      string
		userAgent string
		want      string
	}{
		// an empty value makes the transport drop the header, so the worker sees none
		{"empty user agent", "", ""},
		{"custom user agent", "go-agent", "go-agent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := getResponse(t, s.URL+"/?hello=world", func(req *http.Request) {
				req.Header.Add("User-Agent", tt.userAgent)
			})

			assert.Equal(t, 200, r.code)
			assert.Equal(t, tt.want, r.body)
		})
	}
}

func TestHandler_Cookies(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "cookie", "pipes"}, nil, nil)

	r := getResponse(t, s.URL, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "input", Value: "input-value"})
	})

	assert.Equal(t, 200, r.code)
	assert.Equal(t, "INPUT-VALUE", r.body)

	for _, c := range r.cookies {
		assert.Equal(t, "output", c.Name)
		assert.Equal(t, "cookie-output", c.Value)
	}
}

func TestHandler_IP(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "ip", "pipes"}, nil, nil)

	// the worker echoes REMOTE_ADDR, which is the loopback the test client dials from
	helpers.AssertGet(t, s.URL+"/", 200, "127.0.0.1")
}

func TestHandler_Error(t *testing.T) {
	// "error" throws from the handler, "error2" exits the worker mid-request
	for _, worker := range []string{"error", "error2"} {
		t.Run(worker, func(t *testing.T) {
			s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", worker, "pipes"}, nil, nil)

			code, _ := helpers.GetBody(t, s.URL+"/?hello=world")
			assert.Equal(t, 500, code)
		})
	}
}

// A header the worker announces in Trailer travels behind the body, so the client
// only sees it once the body is read out.
func TestHandler_Trailers(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "trailers", "pipes"}, nil, nil)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	// the announced header is neither a response header nor a trailer yet
	assert.Empty(t, resp.Header.Get("X-Checksum"))
	assert.Empty(t, resp.Trailer.Get("X-Checksum"))

	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "checksummed", string(b))

	assert.Equal(t, "abc", resp.Trailer.Get("X-Checksum"))
}

// response is a finished response: the body is already read and closed.
type response struct {
	code    int
	header  http.Header
	cookies []*http.Cookie
	body    string
}

// getResponse issues a GET, letting mutate customize the request first, and
// reads the response out.
func getResponse(t *testing.T, url string, mutate func(*http.Request)) response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	mutate(req)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = r.Body.Close()
	}()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	return response{code: r.StatusCode, header: r.Header, cookies: r.Cookies(), body: string(b)}
}

func BenchmarkHandler_Listen_Echo(b *testing.B) {
	s := helpers.ServeHandler(b, []string{"php_test_files/http/client.php", "echo", "pipes"}, nil, &pool.Config{
		NumWorkers:      uint64(runtime.NumCPU()),
		AllocateTimeout: time.Second * 1000,
		DestroyTimeout:  time.Second * 1000,
	})

	req, err := http.NewRequestWithContext(b.Context(), http.MethodGet, s.URL+"/?hello=world", nil)
	require.NoError(b, err)

	client := &http.Client{}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		r, err := client.Do(req)
		require.NoError(b, err)

		br, err := io.ReadAll(r.Body)
		require.NoError(b, err)
		require.NoError(b, r.Body.Close())

		if string(br) != "WORLD" {
			b.Fail()
		}
	}
}
