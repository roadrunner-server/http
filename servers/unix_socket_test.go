//go:build linux || darwin || freebsd

package servers_test

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/servers"
	"github.com/roadrunner-server/http/v6/servers/fcgi"
	httpServer "github.com/roadrunner-server/http/v6/servers/http11"
	"github.com/roadrunner-server/http/v6/servers/https"
	"github.com/roadrunner-server/tcplisten"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestServeUnixSocket(t *testing.T) {
	// A short path also fits the macOS UNIX socket address limit.
	dir, err := os.MkdirTemp("", "rr-http-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	for _, protocol := range []string{"http1", "h2c", "fcgi"} {
		for _, attributes := range []string{"default", "explicit"} {
			t.Run(protocol+"/"+attributes, func(t *testing.T) {
				path := filepath.Join(dir, "http.sock")
				uid, gid := os.Geteuid(), os.Getegid()
				var options *tcplisten.UnixSocketOptions
				if attributes == "explicit" {
					if uid == 0 {
						uid, gid = 1, 1
					}
					options = &tcplisten.UnixSocketOptions{Mode: "0660", UID: &uid, GID: &gid}
				}
				cfg := &config.Config{
					Address: "unix://" + path, UnixSocket: options,
					HTTP2Config: &https.HTTP2{H2C: protocol == "h2c"},
					UID:         123, GID: 456,
				}
				require.NoError(t, cfg.InitDefaults())
				logger, errLog := slog.New(slog.DiscardHandler), log.New(io.Discard, "", 0)
				var srv servers.InternalServer[any]
				if protocol == "fcgi" {
					srv = fcgi.NewFCGIServer(http.NotFoundHandler(), &fcgi.FCGI{Address: cfg.Address, UnixSocket: options}, logger, errLog)
				} else {
					srv = httpServer.NewHTTPServer(http.NotFoundHandler(), cfg, errLog, logger)
				}
				done := make(chan error, 1)
				go func() { done <- srv.Serve(nil, nil) }()
				t.Cleanup(func() {
					srv.Stop()
					srv.Stop()
					select {
					case err := <-done:
						require.NoError(t, err)
					case <-time.After(5 * time.Second):
						t.Fatal("Serve did not stop")
					}
					_, err := os.Stat(path)
					require.ErrorIs(t, err, os.ErrNotExist)
				})
				require.Eventually(t, func() bool {
					info, err := os.Stat(path)
					if err != nil {
						return false
					}
					stat := info.Sys().(*syscall.Stat_t)
					return info.Mode()&os.ModeSocket != 0 && int(stat.Uid) == uid && int(stat.Gid) == gid &&
						(options == nil || info.Mode().Perm() == 0o660)
				}, 5*time.Second, 10*time.Millisecond)
				dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
					return new(net.Dialer).DialContext(ctx, "unix", path)
				}
				if protocol == "fcgi" {
					conn, err := dial(t.Context(), "", "")
					require.NoError(t, err)
					require.NoError(t, conn.Close())
					return
				}
				client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: dial}}
				major := 1
				if protocol == "h2c" {
					major = 2
					client.Transport = &http2.Transport{
						AllowHTTP: true,
						DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
							return dial(ctx, network, addr)
						},
					}
				}
				t.Cleanup(client.CloseIdleConnections)
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/", nil)
				require.NoError(t, err)
				resp, err := client.Do(req)
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()
				require.Equal(t, http.StatusNotFound, resp.StatusCode)
				require.Equal(t, major, resp.ProtoMajor)
			})
		}
	}
}
