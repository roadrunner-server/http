//go:build linux || darwin || freebsd

package tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"tests/helpers"
	mocklogger "tests/mock"

	rrconfig "github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestUnixSocketConfig(t *testing.T) {
	for _, key := range []string{"http.unix_socket", "http.fcgi.unix_socket"} {
		for _, tt := range []struct {
			name, address, options, wantErr string
		}{
			{name: "TCP defaults", address: "127.0.0.1:0"},
			{name: "UNIX defaults", address: "unix://listener.sock"},
			{name: "disabled defaults"},
			{name: "empty options", address: "unix://listener.sock", options: "{}"},
			{name: "TCP empty options", address: "127.0.0.1:0", options: "{}"},
			{name: "disabled empty options", options: "{}"},
			{name: "mode only", address: "unix://listener.sock", options: `{mode: "0600"}`},
			{name: "explicit zero", address: "unix://listener.sock", options: `{mode: "0000", uid: 0, gid: 0}`},
			{name: "unset mode", address: "unix://listener.sock", options: "{uid: 0, gid: 0}"},
			{name: "TCP options", address: "127.0.0.1:0", options: `{mode: "0600"}`, wantErr: "filesystem unix:// address"},
			{name: "disabled options", options: `{mode: "0600"}`, wantErr: "filesystem unix:// address"},
			{name: "empty UNIX path", address: "unix://", options: `{mode: "0600"}`, wantErr: "filesystem unix:// address"},
			{name: "short mode", address: "unix://listener.sock", options: `{mode: "600"}`, wantErr: "invalid unix socket mode"},
			{name: "unquoted mode", address: "unix://listener.sock", options: "{mode: 0660}", wantErr: "invalid unix socket mode"},
			{name: "scalar options", address: "unix://listener.sock", options: "false", wantErr: "expected a map"},
			{name: "negative UID", address: "unix://listener.sock", options: "{uid: -1}", wantErr: "invalid unix socket uid"},
			{name: "negative GID", address: "unix://listener.sock", options: "{gid: -1}", wantErr: "invalid unix socket gid"},
			{name: "reserved UID", address: "unix://listener.sock", options: "{uid: 4294967295}", wantErr: "invalid unix socket uid"},
			{name: "reserved GID", address: "unix://listener.sock", options: "{gid: 4294967295}", wantErr: "invalid unix socket gid"},
		} {
			t.Run(key+"/"+tt.name, func(t *testing.T) {
				yaml := fmt.Sprintf(`version: "3"
http:
  fcgi: {address: unix://fcgi.sock}
  address: %q
`, tt.address)
				indent := "  "
				if key == "http.fcgi.unix_socket" {
					yaml = fmt.Sprintf(`version: "3"
http:
  address: unix://http.sock
  fcgi:
    address: %q
`, tt.address)
					indent = "    "
				}
				if tt.options != "" {
					yaml += indent + "unix_socket: " + tt.options + "\n"
				}
				path := filepath.Join(t.TempDir(), ".rr.yaml")
				require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
				provider := &rrconfig.Plugin{Path: path}
				require.NoError(t, provider.Init())
				logger := mocklogger.NewLogger(slog.New(slog.DiscardHandler))
				err := new(httpPlugin.Plugin).Init(provider, logger, new(server.Plugin))
				if tt.wantErr != "" {
					require.ErrorContains(t, err, "http_plugin_init")
					require.ErrorContains(t, err, tt.wantErr)
					return
				}
				require.NoError(t, err)
			})
		}
	}
}

func TestUnixSocketPluginServe(t *testing.T) {
	dir, err := os.MkdirTemp("", "rr-http-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	uid, gid := os.Geteuid(), os.Getegid()
	if uid == 0 {
		uid, gid = 1, 1
	} else {
		groups, err := os.Getgroups()
		require.NoError(t, err)
		for _, group := range groups {
			if group != gid {
				gid = group
				break
			}
		}
	}
	t.Setenv("RR_HTTP_TEST_SOCKET_UID", strconv.Itoa(uid))
	t.Setenv("RR_HTTP_TEST_SOCKET_GID", strconv.Itoa(gid))
	for _, protocol := range []string{"http1", "h2c"} {
		t.Run(protocol, func(t *testing.T) {
			httpPath, fcgiPath := filepath.Join(dir, "http.sock"), filepath.Join(dir, "fcgi.sock")
			yaml := fmt.Sprintf(`version: "3"
server:
  command: "php php_test_files/http/client.php echo pipes"
  relay: pipes
http:
  address: unix://%s
  unix_socket: {mode: "0660", uid: "${RR_HTTP_TEST_SOCKET_UID}", gid: "${RR_HTTP_TEST_SOCKET_GID}"}
  http2: {h2c: %t}
  pool: {num_workers: 1, allocate_timeout: 5s, destroy_timeout: 1s}
  fcgi:
    address: unix://%s
    unix_socket: {mode: "0600", uid: %d, gid: %d}
`, httpPath, protocol == "h2c", fcgiPath, uid, gid)
			configPath := filepath.Join(dir, ".rr.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))
			_, stop := helpers.Start(t, configPath, []any{&server.Plugin{}, &httpPlugin.Plugin{}}, helpers.WithObservedLogger())
			helpers.WaitListener(t, "unix", httpPath)
			dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
				return new(net.Dialer).DialContext(ctx, "unix", httpPath)
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
			response := clientGet(t, client, "http://localhost/?hello=world")
			require.Equal(t, http.StatusCreated, response.StatusCode)
			require.Equal(t, "WORLD", response.Body)
			require.Equal(t, major, response.ProtoMajor)
			code, body := fcgiGet(t, "unix", fcgiPath, "http://localhost/?hello=world")
			require.Equal(t, http.StatusCreated, code)
			require.Equal(t, "WORLD", body)
			for path, mode := range map[string]os.FileMode{httpPath: 0o660, fcgiPath: 0o600} {
				info, err := os.Stat(path)
				require.NoError(t, err)
				require.NotZero(t, info.Mode()&os.ModeSocket)
				require.Equal(t, mode, info.Mode().Perm())
				stat := info.Sys().(*syscall.Stat_t)
				require.EqualValues(t, uid, stat.Uid)
				require.EqualValues(t, gid, stat.Gid)
			}
			stop()
			for _, path := range []string{httpPath, fcgiPath} {
				_, err := os.Stat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}

func TestUnixSocketOwnershipError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("Requires an unprivileged process.")
	}
	groups, err := os.Getgroups()
	require.NoError(t, err)
	otherGID := 0
	for otherGID == os.Getegid() || slices.Contains(groups, otherGID) {
		otherGID++
	}
	dir, err := os.MkdirTemp("", "rr-http-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	for _, protocol := range []string{"http", "fcgi"} {
		for _, tt := range []struct {
			field string
			id    int
		}{
			{field: "uid", id: 0},
			{field: "gid", id: otherGID},
		} {
			t.Run(protocol+"/"+tt.field, func(t *testing.T) {
				t.Setenv("RR_HTTP_TEST_SOCKET_ID", strconv.Itoa(tt.id))
				path := filepath.Join(dir, "ownership.sock")
				yaml := `version: "3"
server:
  command: "php php_test_files/http/client.php echo pipes"
  relay: pipes
http:
  pool: {num_workers: 1, allocate_timeout: 5s, destroy_timeout: 1s}
`
				indent := "  "
				if protocol == "fcgi" {
					yaml += "  fcgi:\n"
					indent = "    "
				}
				yaml += fmt.Sprintf("%saddress: %q\n%sunix_socket: {%s: \"${RR_HTTP_TEST_SOCKET_ID}\"}\n", indent, "unix://"+path, indent, tt.field)
				configPath := filepath.Join(dir, ".rr.yaml")
				require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))
				provider := &rrconfig.Plugin{Path: configPath}
				cont := endure.New(slog.LevelError)
				logger, _ := mocklogger.SlogTestLogger(slog.LevelError)
				require.NoError(t, cont.RegisterAll(provider, logger, &server.Plugin{}, &httpPlugin.Plugin{}))
				require.NoError(t, cont.Init())
				errCh, err := cont.Serve()
				require.NoError(t, err)
				stop := sync.OnceValue(cont.Stop)
				t.Cleanup(func() { require.NoError(t, stop()) })
				select {
				case result := <-errCh:
					require.NotNil(t, result)
					require.ErrorContains(t, result.Error, "chown unix socket")
					require.ErrorContains(t, result.Error, path)
				case <-time.After(5 * time.Second):
					t.Fatal("No socket ownership error.")
				}
				_, err = os.Stat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
				require.NoError(t, stop())
			})
		}
	}
}
