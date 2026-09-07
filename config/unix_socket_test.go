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
	for _, field := range []string{"http.unix_socket", "http.fcgi.unix_socket"} {
		op := "validation"
		if field == "http.fcgi.unix_socket" {
			op = field
		}
		for _, tt := range []struct {
			name, address string
			options       *tcplisten.UnixSocketOptions
			wantErr       string
		}{
			{name: "TCP defaults", address: "127.0.0.1:0"},
			{name: "UNIX defaults", address: "unix://http.sock"},
			{name: "disabled defaults"},
			{name: "empty options", address: "unix://http.sock", options: &tcplisten.UnixSocketOptions{}},
			{name: "mode and zero IDs", address: "unix://http.sock", options: &tcplisten.UnixSocketOptions{Mode: "0660", UID: new(int), GID: new(int)}},
			{name: "TCP options", address: "tcp://127.0.0.1:0", options: &tcplisten.UnixSocketOptions{}, wantErr: "filesystem unix:// address"},
			{name: "disabled options", options: &tcplisten.UnixSocketOptions{}, wantErr: "filesystem unix:// address"},
			{name: "empty UNIX path", address: "unix://", options: &tcplisten.UnixSocketOptions{}, wantErr: "filesystem unix:// address"},
			{name: "short mode", address: "unix://http.sock", options: &tcplisten.UnixSocketOptions{Mode: "660"}, wantErr: "invalid unix socket mode"},
			{name: "invalid octal mode", address: "unix://http.sock", options: &tcplisten.UnixSocketOptions{Mode: "0999"}, wantErr: "invalid unix socket mode"},
			{name: "negative UID", address: "unix://http.sock", options: &tcplisten.UnixSocketOptions{UID: new(-1)}, wantErr: "invalid unix socket uid"},
			{name: "negative GID", address: "unix://http.sock", options: &tcplisten.UnixSocketOptions{GID: new(-1)}, wantErr: "invalid unix socket gid"},
			{name: "abstract address", address: "unix://@http", options: &tcplisten.UnixSocketOptions{}},
		} {
			t.Run(field+"/"+tt.name, func(t *testing.T) {
				cfg := &Config{Address: "127.0.0.1:0", FCGIConfig: &fcgi.FCGI{Address: "127.0.0.1:0"}}
				if field == "http.unix_socket" {
					cfg.Address, cfg.UnixSocket = tt.address, tt.options
				} else {
					cfg.FCGIConfig.Address, cfg.FCGIConfig.UnixSocket = tt.address, tt.options
				}
				wantErr := tt.wantErr
				if runtime.GOOS == "linux" && tt.address == "unix://@http" {
					wantErr = "filesystem unix:// address"
				}
				if runtime.GOOS == "windows" && tt.options != nil {
					wantErr = "unix socket attributes are not supported on Windows"
				}
				err := cfg.InitDefaults()
				if wantErr != "" {
					require.ErrorContains(t, err, op)
					require.ErrorContains(t, err, wantErr)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestUnixSocketDefaults(t *testing.T) {
	cfg := &Config{
		Address:    "unix://http.sock",
		FCGIConfig: &fcgi.FCGI{Address: "unix://fcgi.sock"},
		UID:        123, GID: 456,
	}
	require.NoError(t, cfg.InitDefaults())
	require.Nil(t, cfg.UnixSocket)
	require.Nil(t, cfg.FCGIConfig.UnixSocket)
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
