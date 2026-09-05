package proxyprotocol_test

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roadrunner-server/http/v6/handler"
	"github.com/roadrunner-server/http/v6/middleware"
	"github.com/roadrunner-server/http/v6/servers/proxyprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

func TestHTTPRejectsPeers(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		for _, tt := range []struct {
			name, preface      string
			untrusted, request bool
		}{
			{"malformed", "PROXY TCP4 invalid 198.51.100.2 12345 443\r\n", false, true},
			{"headerless", "", false, true},
			{"untrusted", proxyLine, true, true},
			{"silent", "", false, false},
			{"partialv2", v2Frame(0x21, 0x11, strings.Repeat("\x00", 12))[:20], false, false},
		} {
			t.Run(scheme+"/"+tt.name, func(t *testing.T) {
				var calls atomic.Int32
				ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.WriteHeader(http.StatusNoContent)
				}))
				t.Cleanup(ts.Close)
				ts.Config.ErrorLog = log.New(io.Discard, "", 0)
				cfg := &proxyprotocol.Config{TrustedProxies: []string{"127.0.0.1", "::1"}, ReadHeaderTimeout: 100 * time.Millisecond}
				if tt.untrusted {
					// Trust applies to the socket peer, not the address in the header.
					cfg.TrustedProxies = []string{"192.0.2.1"}
				}
				require.NoError(t, cfg.InitDefaults(ts.Listener.Addr().String()))
				var err error
				ts.Listener, err = cfg.Wrap(ts.Listener)
				require.NoError(t, err)
				if scheme == "https" {
					ts.StartTLS()
				} else {
					ts.Start()
				}
				conn := dialHTTPPeer(t, ts)
				wire := tt.preface
				if tt.request && scheme == "http" {
					wire += payload
				}
				if wire != "" {
					_, err = io.WriteString(conn, wire)
					if !tt.untrusted { // An untrusted socket can be closed before its first write.
						require.NoError(t, err)
					}
				}
				if tt.request && scheme == "https" {
					err = tlsHTTPPeer(t, ts, conn).HandshakeContext(t.Context())
					require.Error(t, err, "PROXY must precede TLS, not be parsed inside it")
				}
				assertPeerClosed(t, conn)
				ts.Close()
				assert.Zero(t, calls.Load())
			})
		}
	}
}

func TestHTTPSlowPeerAndClose(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			accepted, finished := make(chan struct{}, 2), make(chan struct{}, 2)
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Remote-Addr", r.RemoteAddr)
				w.Header().Set("X-Request-URI", handler.URI(r))
			}))
			t.Cleanup(ts.Close)
			ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				// RemoteAddr would block the accept loop until the header arrives.
				if state == http.StateNew {
					accepted <- struct{}{}
				}
				if state == http.StateClosed {
					finished <- struct{}{}
				}
			}
			cfg := &proxyprotocol.Config{TrustedProxies: []string{"127.0.0.1", "::1"}, ReadHeaderTimeout: 30 * time.Second}
			require.NoError(t, cfg.InitDefaults(ts.Listener.Addr().String()))
			var err error
			ts.Listener, err = cfg.Wrap(ts.Listener)
			require.NoError(t, err)
			if scheme == "https" {
				ts.StartTLS()
			} else {
				ts.Start()
			}
			slow := dialHTTPPeer(t, ts)
			select {
			case <-accepted:
			case <-time.After(5 * time.Second):
				t.Fatal("silent peer was not accepted")
			}
			// This request must finish before the silent peer's 30s header timeout.
			assertHTTPResponse(t, proxyHTTPPeer(t, ts))
			select {
			case <-finished:
				t.Fatal("a connection closed before Server.Close")
			default:
			}
			closed := make(chan error, 1)
			go func() { closed <- ts.Config.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("Server.Close blocked on a pending PROXY header")
			}
			assertPeerClosed(t, slow)
			for range 2 {
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("connection goroutine did not finish after Server.Close")
				}
			}
		})
	}
}

func TestHTTPWebSocket(t *testing.T) {
	for _, scheme := range []string{"ws", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			done := make(chan error, 1)
			wsHandler := websocket.Handler(func(ws *websocket.Conn) {
				err := ws.SetDeadline(time.Now().Add(5 * time.Second))
				var text string
				if err == nil {
					err = websocket.Message.Receive(ws, &text)
				}
				if err == nil {
					err = websocket.Message.Send(ws, ws.Request().RemoteAddr+" "+text)
				}
				done <- err
			})
			ts := httptest.NewUnstartedServer(middleware.NewLogMiddleware(wsHandler, true, slog.New(slog.NewTextHandler(io.Discard, nil))))
			t.Cleanup(ts.Close)
			cfg := &proxyprotocol.Config{TrustedProxies: []string{"127.0.0.1", "::1"}}
			require.NoError(t, cfg.InitDefaults(ts.Listener.Addr().String()))
			var err error
			ts.Listener, err = cfg.Wrap(ts.Listener)
			require.NoError(t, err)
			if scheme == "wss" {
				ts.StartTLS()
			} else {
				ts.Start()
			}
			conn := proxyHTTPPeer(t, ts)
			wsCfg, err := websocket.NewConfig(scheme+"://"+ts.Listener.Addr().String()+"/", ts.URL)
			require.NoError(t, err)
			ws, err := websocket.NewClient(wsCfg, conn)
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = conn.Close()
				select {
				case err := <-done:
					assert.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Error("hijacked WebSocket handler did not finish")
				}
			})
			require.NoError(t, websocket.Message.Send(ws, "hello"))
			var text string
			require.NoError(t, websocket.Message.Receive(ws, &text))
			assert.Equal(t, "192.0.2.1:12345 hello", text)
		})
	}
}

func dialHTTPPeer(t *testing.T, ts *httptest.Server) net.Conn {
	t.Helper()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	return conn
}

func tlsHTTPPeer(t *testing.T, ts *httptest.Server, conn net.Conn) *tls.Conn {
	t.Helper()
	cfg := ts.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	var err error
	cfg.ServerName, _, err = net.SplitHostPort(ts.Listener.Addr().String())
	require.NoError(t, err)
	return tls.Client(conn, cfg)
}

func proxyHTTPPeer(t *testing.T, ts *httptest.Server) net.Conn {
	t.Helper()
	conn := dialHTTPPeer(t, ts)
	// The SSL TLV must not change TLS state or the request scheme.
	addresses := "\xc0\x00\x02\x01\xc6\x33\x64\x02\x30\x39\x01\xbb"
	_, err := io.WriteString(conn, v2Frame(0x21, 0x11, addresses+"\x20\x00\x05\x07\x00\x00\x00\x00"))
	require.NoError(t, err)
	if ts.TLS != nil {
		secure := tlsHTTPPeer(t, ts, conn)
		require.NoError(t, secure.HandshakeContext(t.Context()))
		conn = secure
	}
	return conn
}

func assertPeerClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	n, err := conn.Read(make([]byte, 1))
	require.Zero(t, n, "rejected peer received application bytes")
	require.Error(t, err)
	timeout, ok := errors.AsType[net.Error](err)
	require.False(t, ok && timeout.Timeout(), "client safety deadline is not a peer close: %v", err)
}

func assertHTTPResponse(t *testing.T, conn net.Conn) {
	t.Helper()
	_, err := io.WriteString(conn, payload)
	require.NoError(t, err)
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "192.0.2.1:12345", res.Header.Get("X-Remote-Addr"))
	scheme := "http"
	if _, ok := conn.(*tls.Conn); ok {
		scheme = "https"
	}
	assert.Equal(t, scheme+"://example.test/", res.Header.Get("X-Request-URI"))
	assert.Empty(t, body)
}
