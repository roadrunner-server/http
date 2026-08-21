package tests

import (
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"runtime"
	"testing"

	"tests/helpers"
	testplugins "tests/test_plugins"

	httpPlugin "github.com/roadrunner-server/http/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
)

// The worker replies with an empty body, the configured internal_error_code (444)
// is returned instead of a 500.
func TestHTTPIssue659(t *testing.T) {
	helpers.Start(t, "configs/.rr-issue659.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:32552"))

	helpers.AssertGet(t, "http://127.0.0.1:32552", 444, "")
}

// An uncaught PHP exception in a debug-mode pool is logged once.
func TestBug1843(t *testing.T) {
	// a TCP probe instead of a request one: a probe request would spawn a second
	// worker and log the fatal error twice
	rr, stop := helpers.Start(t, "configs/.rr-bug1843.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithObservedLogger(), helpers.WithTCPProbe("127.0.0.1:16322"))

	code, body := helpers.GetBody(t, "http://127.0.0.1:16322")
	require.Equal(t, 500, code)

	// on darwin pipes behave differently, the error also lands in the response body
	if runtime.GOOS == "darwin" {
		require.Contains(t, body, "goridge_frame_receive: validation failed on the message sent to STDOUT")
	}

	stop()

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("PHP Fatal error:  Uncaught RuntimeException").Len())
}

func TestHTTPIssue2381(t *testing.T) {
	helpers.Start(t, "configs/.rr-issue2381.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:19984"))

	// control request without a hint frame
	helpers.AssertGet(t, "http://127.0.0.1:19984/plain", http.StatusNotFound, "body")

	var hints []hint
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			hints = append(hints, hint{code, http.Header(header).Clone()})
			return nil
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:19984/hint", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "body", string(body))
	require.Equal(t, "probe", resp.Header.Get("X-Marker"))
	require.Empty(t, resp.Header.Get("Link"), "the hint Link header leaked into the final response")

	require.Len(t, hints, 1)
	require.Equal(t, http.StatusEarlyHints, hints[0].code)
	require.Equal(t, "</a.css>; rel=preload", hints[0].header.Get("Link"))
}

func TestHttpBrokenPipes(t *testing.T) {
	_ = helpers.StartExpectServeError(t, "configs/.rr-broken-pipes.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&testplugins.PluginMiddleware{},
		&testplugins.PluginMiddleware2{},
	})
}
