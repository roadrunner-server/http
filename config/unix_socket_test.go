package config

import (
	"runtime"
	"testing"

	"github.com/roadrunner-server/http/v6/servers/fcgi"
	"github.com/roadrunner-server/http/v6/servers/proxyprotocol"
	"github.com/roadrunner-server/tcplisten"
	"github.com/stretchr/testify/require"
)

func TestUnixSocketValidation(t *testing.T) {
	negative := -1
	for _, field := range []string{"http.unix_socket", "http.fcgi.unix_socket"} {
		for _, tt := range []struct {
			name, address string
			options       *tcplisten.UnixSocketOptions
			invalid       bool
		}{
			{"nil_tcp", "127.0.0.1:0", nil, false},
			{"nil_unix", "unix://http.sock", nil, false},
			{"nil_disabled", "", nil, false},
			{"empty_options", "unix://http.sock", &tcplisten.UnixSocketOptions{}, false},
			{"mode_and_zero_ids", "unix://http.sock", &tcplisten.UnixSocketOptions{Mode: "0660", UID: new(int), GID: new(int)}, false},
			{"tcp", "tcp://127.0.0.1:0", &tcplisten.UnixSocketOptions{}, true},
			{"disabled", "", &tcplisten.UnixSocketOptions{}, true},
			{"missing_path", "unix://", &tcplisten.UnixSocketOptions{}, true},
			{"short_mode", "unix://http.sock", &tcplisten.UnixSocketOptions{Mode: "660"}, true},
			{"invalid_mode", "unix://http.sock", &tcplisten.UnixSocketOptions{Mode: "0999"}, true},
			{"negative_uid", "unix://http.sock", &tcplisten.UnixSocketOptions{UID: &negative}, true},
			{"negative_gid", "unix://http.sock", &tcplisten.UnixSocketOptions{GID: &negative}, true},
			{"abstract", "unix://@http", &tcplisten.UnixSocketOptions{}, runtime.GOOS == "linux"},
		} {
			t.Run(field+"/"+tt.name, func(t *testing.T) {
				cfg := &Config{Address: "127.0.0.1:0", FCGIConfig: &fcgi.FCGI{Address: "127.0.0.1:0"}}
				if field == "http.unix_socket" {
					cfg.Address, cfg.UnixSocket = tt.address, tt.options
				} else {
					cfg.FCGIConfig.Address, cfg.FCGIConfig.UnixSocket = tt.address, tt.options
				}
				err := cfg.InitDefaults()
				if tt.invalid || (runtime.GOOS == "windows" && tt.options != nil) {
					require.ErrorContains(t, err, field)
					require.ErrorContains(t, cfg.Valid(), field)
				} else {
					require.NoError(t, err)
					require.NoError(t, cfg.Valid())
				}
			})
		}
	}
}

func TestUnixSocketIndependentOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UNIX socket attributes are not supported on Windows")
	}
	for _, tt := range []struct {
		name       string
		http, fcgi *tcplisten.UnixSocketOptions
	}{
		{"omitted", nil, nil},
		{"http_only", &tcplisten.UnixSocketOptions{Mode: "0600"}, nil},
		{"fcgi_only", nil, &tcplisten.UnixSocketOptions{Mode: "0660"}},
		{"both", &tcplisten.UnixSocketOptions{Mode: "0600"}, &tcplisten.UnixSocketOptions{Mode: "0660"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Address: "unix://http.sock", UnixSocket: tt.http,
				FCGIConfig: &fcgi.FCGI{Address: "unix://fcgi.sock", UnixSocket: tt.fcgi},
				UID:        123, GID: 456,
			}
			require.NoError(t, cfg.InitDefaults())
			require.Same(t, tt.http, cfg.UnixSocket)
			require.Same(t, tt.fcgi, cfg.FCGIConfig.UnixSocket)
			for _, options := range []*tcplisten.UnixSocketOptions{cfg.UnixSocket, cfg.FCGIConfig.UnixSocket} {
				if options != nil {
					require.Nil(t, options.UID)
					require.Nil(t, options.GID)
				}
			}
		})
	}
}

func TestUnixSocketRejectsProxyProtocol(t *testing.T) {
	cfg := &Config{
		Address:       "unix://http.sock",
		UnixSocket:    &tcplisten.UnixSocketOptions{Mode: "0660"},
		ProxyProtocol: &proxyprotocol.Config{TrustedProxies: []string{"127.0.0.1"}},
	}
	err := cfg.InitDefaults()
	require.ErrorContains(t, err, "http.proxy_protocol")
	require.ErrorContains(t, err, "TCP listen")
}
