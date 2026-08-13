package tests

import (
	"net/http"
	"os"
	"testing"
	"time"

	"tests/helpers"

	"github.com/roadrunner-server/fileserver/v6"
	"github.com/roadrunner-server/gzip/v6"
	"github.com/roadrunner-server/headers/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/send/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/roadrunner-server/static/v6"
	"github.com/stretchr/testify/require"
)

// staticSourceFile is served from the repository itself: the static configs allow
// ".php", so the middleware answers with the source instead of running it.
const staticSourceFile = "php_test_files/client.php"

// The static middleware serves the allowed extensions from disk, adds the
// configured response header and revalidates with an etag.
func TestStatic(t *testing.T) {
	const base = "http://127.0.0.1:21603"

	helpers.Start(t, "configs/.rr-http-static.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	}, helpers.WithProbe(base))

	t.Run("serveSample", func(t *testing.T) {
		requireSampleServed(t, base+"/sample.txt")
	})

	t.Run("serveSource", func(t *testing.T) {
		requireSourceServed(t, base+"/"+staticSourceFile)
	})

	t.Run("etag", func(t *testing.T) {
		requireEtagRevalidation(t, base+"/sample.txt")
	})

	// the static headers belong to the files the middleware serves, a response the
	// worker produced carries none of them
	t.Run("noStaticHeadersFromWorker", func(t *testing.T) {
		res := clientGet(t, http.DefaultClient, base)
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.NotContains(t, res.Header["Input"], "custom-header")
		require.NotContains(t, res.Header["Output"], "output-header")
	})
}

// The same static configuration over a 40MB file, served through the sendfile
// middleware.
func TestStaticBigFile(t *testing.T) {
	const base = "http://127.0.0.1:21604"

	helpers.Start(t, "configs/.rr-http-static-big-file.yaml", []any{
		&server.Plugin{},
		&headers.Plugin{},
		&send.Plugin{},
		&httpPlugin.Plugin{},
		&static.Plugin{},
	}, helpers.WithConfigVersion("2023.1.5"), helpers.WithTCPProbe("127.0.0.1:21604"))

	t.Run("serveSample", func(t *testing.T) {
		requireSampleServed(t, base+"/sample-big.txt")
	})

	t.Run("serveSource", func(t *testing.T) {
		requireSourceServed(t, base+"/"+staticSourceFile)
	})
}

// A path pointing outside the served directory is never answered with a file.
func TestStaticPathTraversal(t *testing.T) {
	const addr = "127.0.0.1:21603"

	helpers.Start(t, "configs/.rr-http-static-security.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	}, helpers.WithProbe("http://"+addr))

	// none of these reaches a middleware: the path carries no leading slash and the
	// client escapes the percent signs again, so the request target no longer parses
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "encodedDotsStrayPercent", path: "%2e%2e%/tests/"},
		{name: "encodedDotsBackslash", path: "%2e%2e%5ctests/"},
		{name: "dotsEncodedSlash", path: "..%2ftests/"},
		{name: "encodedDotsEncodedSlash", path: "%2e%2e%2ftests/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, http.StatusBadRequest, getRawPath(t, "http://"+addr, tt.path))
		})
	}

	// plain dot segments do reach the middleware, which refuses to leave its root
	t.Run("dotSegments", func(t *testing.T) {
		code, _ := helpers.GetBody(t, "http://"+addr+"/../../sample.txt")
		require.Equal(t, http.StatusForbidden, code)
	})
}

// A static dir that does not exist fails the container init.
func TestStaticDisabled_Error(t *testing.T) {
	_ = helpers.StartExpectInitError(t, "configs/.rr-http-static-disabled.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	})
}

// Without the static middleware every request goes to the worker, the source of
// the requested file included.
func TestStaticFilesDisabled(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-static-files-disable.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:45877"))

	helpers.AssertGet(t, "http://127.0.0.1:45877/"+staticSourceFile+"?hello=world", 201, "WORLD")
}

// The static middleware passes on what it will not serve: a path outside its
// dir, an extension outside the allow list and a forbidden extension. Only the
// last one is worth a log record.
func TestStaticFilesForbid(t *testing.T) {
	// A TCP probe instead of a request one: a probe request would add an "http log" record.
	rr, stop := helpers.Start(t, "configs/.rr-http-static-files.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithTCPProbe("127.0.0.1:34653"))

	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "outsideStaticDir", path: "/http"},
		{name: "extensionNotAllowed", path: "/client.XXX"},
		{name: "extensionForbidden", path: "/client.php"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			helpers.AssertGet(t, "http://127.0.0.1:34653"+tt.path+"?hello=world", 201, "WORLD")
		})
	}

	stop()

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("http server was started").Len())
	require.Equal(t, 3, rr.Logs.FilterMessageSnippet("http log").Len())
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("file extension is forbidden").Len())
}

// The fileserver plugin serves its prefix without the http plugin and
// revalidates with an etag of its own.
func TestFileServer(t *testing.T) {
	const url = "http://127.0.0.1:10101/foo/sample.txt"

	helpers.Start(t, "configs/.rr-http-static-new.yaml", []any{
		&fileserver.Plugin{},
	}, helpers.WithGracefulTimeout(time.Second*30), helpers.WithProbe(url))

	requireEtagRevalidation(t, url)
}

// The sendfile middleware streams the file the worker named in X-Sendfile and
// drops the header on the way out.
func TestHTTPXSendFile(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-sendfile.yaml", []any{
		&server.Plugin{},
		&send.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithGracefulTimeout(time.Minute), helpers.WithTCPProbe("127.0.0.1:41134"))

	want, err := os.ReadFile("php_test_files/well")
	require.NoError(t, err)

	res := clientGet(t, http.DefaultClient, "http://127.0.0.1:41134")
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Empty(t, res.Header.Get("X-Sendfile"))
	require.Len(t, res.Body, len(want))
}

// requireSampleServed requires that url serves one of the sample fixtures.
func requireSampleServed(t *testing.T, url string) {
	t.Helper()

	code, body := helpers.GetBody(t, url)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, "sample")
}

// requireSourceServed requires that url serves staticSourceFile byte for byte, with
// the configured static response header.
func requireSourceServed(t *testing.T, url string) {
	t.Helper()

	want, err := os.ReadFile(staticSourceFile)
	require.NoError(t, err)

	res := clientGet(t, http.DefaultClient, url)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "output-header", res.Header.Get("Output"))
	require.Equal(t, string(want), res.Body)
}

// requireEtagRevalidation requires that url serves a sample file with an etag,
// and answers a request carrying that etag with a 304.
func requireEtagRevalidation(t *testing.T, url string) {
	t.Helper()

	res := clientGet(t, http.DefaultClient, url)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Contains(t, res.Body, "sample")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	req.Header.Set("If-None-Match", res.Header.Get("Etag"))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusNotModified, resp.StatusCode)
}

// getRawPath issues a GET with path written straight into the request URL,
// bypassing the parsing a url string goes through, and returns the status code.
func getRawPath(t *testing.T, base, path string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base, nil)
	require.NoError(t, err)

	req.URL.Path = path

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return resp.StatusCode
}
