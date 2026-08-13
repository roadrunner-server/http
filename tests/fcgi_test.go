package tests

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"tests/helpers"

	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/roadrunner-server/static/v6"
	"github.com/stretchr/testify/require"
	"github.com/yookoala/gofast"
)

const (
	listenerTimeout = time.Second * 15
	listenerTick    = time.Millisecond * 20
)

// The fastcgi frontend answers over tcp and over a unix socket, and passes the
// request uri through to the worker.
func TestFastCGI(t *testing.T) {
	for _, tt := range []struct {
		name    string
		cfg     string
		network string
		addr    string
		// wantBody, when set, has to appear in the response the worker produced.
		wantBody string
	}{
		{name: "tcp", cfg: "configs/.rr-fcgi.yaml", network: "tcp", addr: "127.0.0.1:6920"},
		{name: "unix", cfg: "configs/.rr-fcgi-unix.yaml", network: "unix", addr: "rr.sock"},
		{name: "requestURI", cfg: "configs/.rr-fcgi-request-uri.yaml", network: "tcp", addr: "127.0.0.1:6921", wantBody: "ddddd"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.network == "unix" {
				// registered first, so it runs after the container is stopped
				t.Cleanup(func() { _ = os.Remove(tt.addr) })
			}

			helpers.Start(t, tt.cfg, []any{
				&server.Plugin{},
				&httpPlugin.Plugin{},
				&static.Plugin{},
			})

			code, body := fcgiGet(t, tt.network, tt.addr, "http://site.local/hello-world")
			require.Equal(t, 200, code)

			if tt.wantBody != "" {
				require.Contains(t, body, tt.wantBody)
			}
		})
	}
}

// fcgiGet issues a GET through the fastcgi frontend at addr and returns the
// status code and the body.
func fcgiGet(t *testing.T, network, addr, url string) (int, string) {
	t.Helper()

	waitListener(t, network, addr)

	fcgiHandler := gofast.NewHandler(
		gofast.BasicParamsMap(gofast.BasicSession),
		gofast.SimpleClientFactory(gofast.SimpleConnFactory(network, addr)),
	)

	w := httptest.NewRecorder()
	fcgiHandler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))

	resp := w.Result()

	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

// waitListener waits until addr accepts a connection. The plain, tls and fastcgi
// frontends of one config bind in parallel, so the readiness of the one Start
// probed says nothing about the others.
func waitListener(t *testing.T, network, addr string) {
	t.Helper()

	require.Eventually(t, func() bool {
		var d net.Dialer

		conn, err := d.DialContext(t.Context(), network, addr)
		if err != nil {
			return false
		}

		return conn.Close() == nil
	}, listenerTimeout, listenerTick, "listener %s did not start", addr)
}
