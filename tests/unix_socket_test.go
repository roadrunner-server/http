//go:build linux || darwin || freebsd

package tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tests/helpers"
	mocklogger "tests/mock"

	rrconfig "github.com/roadrunner-server/config/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/servers/fcgi"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestUnixSocketConfigDecode(t *testing.T) {
	for _, tt := range []struct {
		name, httpOptions, fcgiOptions, httpMode, fcgiMode, errorField string
		flags                                                          []string
		zeroIDs                                                        bool
	}{
		{name: "omitted"},
		{name: "http_only", httpOptions: `{mode: "0660"}`, httpMode: "0660"},
		{name: "fcgi_only", fcgiOptions: `{mode: "0600"}`, fcgiMode: "0600"},
		{
			name: "numeric_zero_ids", httpOptions: `{mode: "0660", uid: 0, gid: 0}`, fcgiOptions: `{mode: "0600", uid: 0, gid: 0}`,
			httpMode: "0660", fcgiMode: "0600", zeroIDs: true,
		},
		{
			name: "string_cli_overrides", httpOptions: `{mode: "0600"}`, fcgiOptions: `{mode: "0660"}`,
			flags: []string{
				"http.unix_socket.mode=0660", "http.unix_socket.uid=0", "http.unix_socket.gid=0",
				"http.fcgi.unix_socket.mode=0600", "http.fcgi.unix_socket.uid=0", "http.fcgi.unix_socket.gid=0",
			},
			httpMode: "0660", fcgiMode: "0600", zeroIDs: true,
		},
		{name: "invalid_http_mode", httpOptions: `{mode: "660"}`, errorField: "http.unix_socket"},
		{name: "invalid_fcgi_mode", fcgiOptions: `{mode: "660"}`, errorField: "http.fcgi.unix_socket"},
		{name: "numeric_mode", httpOptions: `{mode: 0660}`, errorField: "http.unix_socket"},
		{name: "http_empty_options_tcp", httpOptions: `{}`, flags: []string{"http.address=127.0.0.1:0"}, errorField: "http.unix_socket"},
		{name: "fcgi_empty_options_tcp", fcgiOptions: `{}`, flags: []string{"http.fcgi.address=127.0.0.1:0"}, errorField: "http.fcgi.unix_socket"},
		{name: "disabled_http", httpOptions: `{}`, flags: []string{`http.address=""`}, errorField: "http.unix_socket"},
		{name: "disabled_fcgi", fcgiOptions: `{}`, flags: []string{`http.fcgi.address=""`}, errorField: "http.fcgi.unix_socket"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "version: \"3\"\nhttp:\n  address: unix://http.sock\n"
			if tt.httpOptions != "" {
				yaml += "  unix_socket: " + tt.httpOptions + "\n"
			}
			yaml += "  fcgi:\n    address: unix://fcgi.sock\n"
			if tt.fcgiOptions != "" {
				yaml += "    unix_socket: " + tt.fcgiOptions + "\n"
			}
			path := filepath.Join(t.TempDir(), ".rr.yaml")
			require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
			provider := &rrconfig.Plugin{Path: path, Flags: tt.flags}
			require.NoError(t, provider.Init())
			var cfg config.Config
			require.NoError(t, provider.UnmarshalKey("http", &cfg))
			require.NoError(t, provider.UnmarshalKey("http.fcgi", &cfg.FCGIConfig))
			if tt.errorField != "" {
				// Invalid options must fail before logger and worker access.
				require.ErrorContains(t, new(httpPlugin.Plugin).Init(provider, nil, nil), tt.errorField)
				return
			}
			require.NoError(t, cfg.InitDefaults())
			for i, mode := range []string{tt.httpMode, tt.fcgiMode} {
				options := cfg.UnixSocket
				if i == 1 {
					options = cfg.FCGIConfig.UnixSocket
				}
				if mode == "" {
					require.Nil(t, options)
					continue
				}
				require.NotNil(t, options)
				require.Equal(t, mode, options.Mode)
				if tt.zeroIDs {
					require.NotNil(t, options.UID)
					require.NotNil(t, options.GID)
					require.Zero(t, *options.UID)
					require.Zero(t, *options.GID)
				} else {
					require.Nil(t, options.UID)
					require.Nil(t, options.GID)
				}
			}
		})
	}
}

func TestUnixSocketOwnershipValidation(t *testing.T) {
	const env = "RR_HTTP_UNIX_SOCKET_TEST_ID"
	t.Setenv(env, "33")
	id := 33
	for _, key := range []string{"http.unix_socket", "http.fcgi.unix_socket"} {
		for _, field := range []string{"uid", "gid"} {
			for _, tt := range []struct {
				name, value             string
				want                    *int
				invalid, unsetEnv, json bool
			}{
				{name: "false", value: "false", invalid: true},
				{name: "true", value: "true", invalid: true},
				{name: "fraction", value: "1.9", invalid: true},
				{name: "negative_fraction", value: "-0.5", invalid: true},
				{name: "empty_string", value: `""`, invalid: true},
				{name: "unset_env", value: `"${RR_HTTP_UNIX_SOCKET_TEST_ID}"`, invalid: true, unsetEnv: true},
				{name: "over_range", value: "4294967295", invalid: true},
				{name: "unsigned_over_range", value: "18446744073709551615", invalid: true},
				{name: "string_over_range", value: `"4294967295"`, invalid: true},
				{name: "string_overflow", value: `"9223372036854775808"`, invalid: true},
				{name: "nan", value: ".nan", invalid: true},
				{name: "infinity", value: ".inf", invalid: true},
				{name: "sequence", value: "[33]", invalid: true},
				{name: "map", value: "{id: 33}", invalid: true},
				{name: "integer", value: "33", want: &id},
				{name: "populated_env", value: `"${RR_HTTP_UNIX_SOCKET_TEST_ID}"`, want: &id},
				{name: "zero", value: "0", want: new(int)},
				{name: "string_zero", value: `"0"`, want: new(int)},
				{name: "base_zero_string", value: `"0x21"`, want: &id},
				{name: "null", value: "null"},
				{name: "json_integer", value: "33.0", want: &id, json: true},
			} {
				t.Run(key+"/"+field+"/"+tt.name, func(t *testing.T) {
					if tt.unsetEnv {
						t.Setenv(env, "")
						require.NoError(t, os.Unsetenv(env))
					}
					dir := t.TempDir()
					socketPath := filepath.Join(dir, "listener.sock")
					contents, indent := "version: \"3\"\nhttp:\n", "  "
					if key == "http.fcgi.unix_socket" {
						contents += "  fcgi:\n"
						indent = "    "
					}
					contents += fmt.Sprintf("%saddress: %q\n%sunix_socket: {mode: \"0600\", %s: %s}\n", indent, "unix://"+socketPath, indent, field, tt.value)
					path := filepath.Join(dir, ".rr.yaml")
					if tt.json {
						section := fmt.Sprintf(`{"address":%q,"unix_socket":{"mode":"0600",%q:%s}}`, "unix://"+socketPath, field, tt.value)
						if key == "http.fcgi.unix_socket" {
							section = `{"fcgi":` + section + `}`
						}
						contents = `{"version":"3","http":` + section + `}`
						path = filepath.Join(dir, ".rr.json")
					}
					require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
					provider := &rrconfig.Plugin{Path: path}
					require.NoError(t, provider.Init())
					if tt.invalid {
						require.ErrorContains(t, new(httpPlugin.Plugin).Init(provider, nil, nil), key+"."+field)
					} else {
						logger := mocklogger.NewLogger(slog.New(slog.DiscardHandler))
						require.NoError(t, new(httpPlugin.Plugin).Init(provider, logger, new(server.Plugin)))
						var cfg config.Config
						require.NoError(t, provider.UnmarshalKey("http", &cfg))
						options := cfg.UnixSocket
						if key == "http.fcgi.unix_socket" {
							options = cfg.FCGIConfig.UnixSocket
						}
						require.NotNil(t, options)
						require.Equal(t, "0600", options.Mode)
						got, other := options.UID, options.GID
						if field == "gid" {
							got, other = other, got
						}
						require.Equal(t, tt.want, got)
						require.Nil(t, other)
					}
					_, err := os.Stat(socketPath)
					require.ErrorIs(t, err, os.ErrNotExist)
				})
			}
		}
	}
}

func TestUnixSocketFCGIRequest(t *testing.T) {
	dir, err := os.MkdirTemp("", "rr-fcgi-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	path := filepath.Join(dir, "fcgi.sock")
	provider := &rrconfig.Plugin{Type: "yaml", ReadInCfg: []byte("http:\n  fcgi:\n    address: unix://" + path + "\n    unix_socket: {mode: \"0600\"}\n")}
	require.NoError(t, provider.Init())
	var cfg config.Config
	require.NoError(t, provider.UnmarshalKey("http", &cfg))
	require.NoError(t, cfg.InitDefaults())
	srv := fcgi.NewFCGIServer(http.NotFoundHandler(), cfg.FCGIConfig, slog.New(slog.DiscardHandler), log.New(io.Discard, "", 0))
	done := make(chan error, 1)
	go func() { done <- srv.Serve(nil, nil) }()
	t.Cleanup(func() {
		srv.Stop()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("FCGI Serve did not stop")
		}
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
	code, body := fcgiGet(t, "unix", path, "http://localhost/")
	require.Equal(t, http.StatusNotFound, code)
	require.Equal(t, "404 page not found\n", body)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestUnixSocketPluginServe(t *testing.T) {
	dir, err := os.MkdirTemp("", "rr-http-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	for _, protocol := range []string{"http1", "h2c"} {
		t.Run(protocol, func(t *testing.T) {
			httpPath, fcgiPath := filepath.Join(dir, "http.sock"), filepath.Join(dir, "fcgi.sock")
			yaml := fmt.Sprintf(`version: "3"
server:
  command: "php php_test_files/http/client.php echo pipes"
  relay: pipes
http:
  address: unix://%s
  unix_socket: {mode: "0660", uid: %d, gid: %d}
  http2: {h2c: %t}
  pool: {num_workers: 1, allocate_timeout: 5s, destroy_timeout: 1s}
  fcgi:
    address: unix://%s
    unix_socket: {mode: "0600"}
`, httpPath, os.Geteuid(), os.Getegid(), protocol == "h2c", fcgiPath)
			_, stop := helpers.Start(t, "", []any{&server.Plugin{}, &httpPlugin.Plugin{}}, helpers.WithInlineConfig(yaml), helpers.WithObservedLogger())
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
				require.Equal(t, mode, info.Mode().Perm())
			}
			stop()
			for _, path := range []string{httpPath, fcgiPath} {
				_, err := os.Stat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}
