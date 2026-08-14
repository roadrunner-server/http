package tests

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"tests/helpers"

	"github.com/quic-go/quic-go/http3"
	httpPlugin "github.com/roadrunner-server/http/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

// One request over http/2 leaves one access-log record behind.
func TestHTTP2Req(t *testing.T) {
	skipWithoutTrustedCA(t)

	rr, stop := helpers.Start(t, "configs/.rr-h2-ssl.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithGracefulTimeout(time.Second*5),
		// a TCP probe instead of a request one: a probe request would add an "http log" record
		helpers.WithTCPProbe("127.0.0.1:23452"))

	client := &http.Client{Transport: &http2.Transport{TLSClientConfig: mtlsConfig(t)}}

	res := clientGet(t, client, "https://127.0.0.1:23452?hello=world")
	require.Equal(t, http.StatusCreated, res.StatusCode)
	require.Equal(t, "WORLD", res.Body)

	stop()

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("http server was started").Len())
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("http log").Len())
}

// h2c is prior-knowledge only, an upgrade request is served as plain HTTP/1.1.
func TestH2CUpgrade(t *testing.T) {
	rr, stop := helpers.Start(t, "configs/.rr-h2c.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithGracefulTimeout(time.Second*5),
		helpers.WithTCPProbe("127.0.0.1:8083"))

	req, err := http.NewRequestWithContext(t.Context(), "PRI", "http://127.0.0.1:8083", nil)
	require.NoError(t, err)

	req.Header.Add("Upgrade", "h2c")
	req.Header.Add("Connection", "HTTP2-Settings")
	req.Header.Add("Connection", "Upgrade")
	req.Header.Add("HTTP2-Settings", "AAMAAABkAARAAAAAAAIAAAAA")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	require.Equal(t, "201 Created", resp.Status)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	plain := clientGet(t, http.DefaultClient, "http://127.0.0.1:8083?hello=world")
	require.Equal(t, 1, plain.ProtoMajor)

	stop()

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("http server was started").Len())
}

// A client with prior knowledge speaks http/2 over the cleartext address.
func TestH2C(t *testing.T) {
	rr, stop := helpers.Start(t, "configs/.rr-h2c.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithTCPProbe("127.0.0.1:8083"))

	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				// h2c: dial without tls
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}

	res := clientGet(t, client, "http://127.0.0.1:8083?hello=world")
	require.Equal(t, "201 Created", res.Status)
	require.Equal(t, "WORLD", res.Body)
	require.Equal(t, 2, res.ProtoMajor)

	stop()

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("http server was started").Len())
}

// The http3 server is behind the experimental features flag.
func TestHttp3(t *testing.T) {
	skipWithoutTrustedCA(t)

	helpers.Start(t, "configs/.rr-http3.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithExperimentalFeatures(), helpers.WithTCPProbe("127.0.0.1:34554"))

	roundTripper := &http3.Transport{TLSClientConfig: mtlsConfig(t)}
	t.Cleanup(func() {
		if err := roundTripper.Close(); err != nil {
			t.Log(err)
		}
	})

	// quic listens on udp, which no dial can probe, so the request itself retries
	var (
		code int
		body string
	)
	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1:34555?hello=world", nil)
		if err != nil {
			return false
		}

		resp, err := roundTripper.RoundTrip(req)
		if err != nil {
			return false
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}

		code, body = resp.StatusCode, string(b)

		return true
	}, helpers.ListenerTimeout, helpers.ListenerTick, "http3 server did not answer")

	require.Equal(t, 201, code)
	require.Equal(t, "WORLD", body)
}

// mtlsConfig is the tls configuration of helpers.MTLSClient, for the transports
// that cannot reuse its client.
func mtlsConfig(t *testing.T) *tls.Config {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testClientCert, testClientKey)
	require.NoError(t, err)

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
}
