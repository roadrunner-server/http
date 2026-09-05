package tests

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tests/helpers"

	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

const proxyProtocolConfig = `version: "3"
server:
  command: "php php_test_files/http/client.php ip pipes"
  relay: pipes
http:
  pool:
    num_workers: 1
    destroy_timeout: 1s
%s`

func TestProxyProtocolServe(t *testing.T) {
	cert, key, roots := proxyProtocolTLS(t)
	type request struct{ name, protocol, header, remoteAddr string }
	for _, tt := range []struct {
		name                      string
		plainProxy, sslProxy, h2c bool
		requests                  []request
	}{
		{
			name: "plain_only", plainProxy: true,
			requests: []request{
				{"http1_v1_ipv4", "http1", "PROXY TCP4 198.51.100.7 192.0.2.1 12345 80\r\n", "198.51.100.7"},
				{"unknown_socket_address", "http1", "PROXY UNKNOWN\r\n", "127.0.0.1"},
				{"tls_direct", "https1", "", "127.0.0.1"},
			},
		},
		{
			name: "both_h2c", plainProxy: true, sslProxy: true, h2c: true,
			requests: []request{
				{"h2c_v2_ipv6", "h2c", proxyProtocolV2("2001:db8::7"), "2001:db8::7"},
				{"http1_v2_ipv4", "http1", proxyProtocolV2("198.51.100.7"), "198.51.100.7"},
				{"local_socket_address", "https2", "\r\n\r\n\x00\r\nQUIT\n\x20\x00\x00\x00", "127.0.0.1"},
			},
		},
		{
			name: "ssl_only", sslProxy: true, h2c: true,
			requests: []request{
				{"plain_direct", "http1", "", "127.0.0.1"},
				{"https1_v1_ipv6", "https1", "PROXY TCP6 2001:db8::7 2001:db8::1 12345 443\r\n", "2001:db8::7"},
				{"https2_v2_ipv4", "https2", proxyProtocolV2("198.51.100.7"), "198.51.100.7"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plainAddr, sslAddr := proxyProtocolAddresses(t)
			section := fmt.Sprintf("  address: %s\n  ssl:\n    address: %s\n    cert: %q\n    key: %q\n", plainAddr, sslAddr, cert, key)
			if tt.sslProxy {
				section += "    proxy_protocol: {trusted_proxies: [127.0.0.1/32], read_header_timeout: 1s}\n"
			}
			if tt.plainProxy {
				section += "  proxy_protocol: {trusted_proxies: [127.0.0.1]}\n"
			}
			if tt.h2c {
				section += "  http2: {h2c: true}\n"
			}
			helpers.Start(t, "", []any{&server.Plugin{}, &httpPlugin.Plugin{}},
				helpers.WithInlineConfig(fmt.Sprintf(proxyProtocolConfig, section)),
				helpers.WithObservedLogger(), helpers.WithTCPProbe(plainAddr))
			helpers.WaitListener(t, "tcp", sslAddr)

			for _, req := range tt.requests {
				t.Run(req.name, func(t *testing.T) {
					var dials atomic.Int32
					dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
						conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
						if err != nil {
							return nil, err
						}
						dials.Add(1)
						// Write once per TCP connection, before the transport starts TLS or HTTP.
						err = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
						if err == nil {
							_, err = io.WriteString(conn, req.header)
						}
						if err == nil {
							err = conn.SetWriteDeadline(time.Time{})
						}
						if err != nil {
							_ = conn.Close()
							return nil, err
						}
						return conn, nil
					}
					client := &http.Client{
						Timeout: 5 * time.Second,
						Transport: &http.Transport{
							DialContext:       dial,
							TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
							ForceAttemptHTTP2: req.protocol == "https2",
						},
					}
					if req.protocol == "h2c" {
						client.Transport = &http2.Transport{
							AllowHTTP: true,
							DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
								return dial(ctx, network, addr)
							},
						}
					}
					t.Cleanup(client.CloseIdleConnections)
					url, major := "http://"+plainAddr, 1
					secure := strings.HasPrefix(req.protocol, "https")
					if secure {
						url = "https://" + sslAddr
					}
					if req.protocol == "h2c" || req.protocol == "https2" {
						major = 2
					}
					for range 2 {
						res := clientGet(t, client, url)
						require.Equal(t, http.StatusOK, res.StatusCode)
						require.Equal(t, req.remoteAddr, res.Body)
						require.Equal(t, major, res.ProtoMajor)
						if secure {
							require.NotNil(t, res.TLS)
							require.NotEmpty(t, res.TLS.VerifiedChains)
							if major == 2 {
								require.Equal(t, "h2", res.TLS.NegotiatedProtocol)
							}
						} else {
							require.Nil(t, res.TLS)
						}
					}
					require.Equal(t, int32(1), dials.Load(), "keepalive must reuse the connection and its single PROXY header")
				})
			}
		})
	}
}

func TestProxyProtocolInit(t *testing.T) {
	cert, key, _ := proxyProtocolTLS(t)
	plain := "  address: 127.0.0.1:0\n"
	ssl := fmt.Sprintf("  ssl:\n    address: 127.0.0.1:0\n    cert: %q\n    key: %q\n", cert, key)
	for _, tt := range []struct{ name, section string }{
		{"empty_plain", plain + "  proxy_protocol: {}\n"},
		{"empty_ssl", ssl + "    proxy_protocol: {}\n"},
		{"plain_without_http", ssl + "  proxy_protocol: {trusted_proxies: [127.0.0.1]}\n"},
		{"ssl_without_tls", plain + "  ssl:\n    proxy_protocol: {trusted_proxies: [127.0.0.1]}\n"},
		{"negative_timeout", plain + "  proxy_protocol: {trusted_proxies: [127.0.0.1], read_header_timeout: -1s}\n"},
		{"invalid_timeout", ssl + "    proxy_protocol: {trusted_proxies: [127.0.0.1], read_header_timeout: later}\n"},
		{"invalid_ip", plain + "  proxy_protocol: {trusted_proxies: [localhost]}\n"},
		{"invalid_cidr", ssl + "    proxy_protocol: {trusted_proxies: [127.0.0.1/99]}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_ = helpers.StartExpectInitError(t, "", []any{&server.Plugin{}, &httpPlugin.Plugin{}},
				helpers.WithInlineConfig(fmt.Sprintf(proxyProtocolConfig, tt.section)), helpers.WithObservedLogger())
		})
	}
}

func proxyProtocolTLS(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "localhost.pem"), filepath.Join(dir, "localhost.key")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(certPEM))
	return certPath, keyPath, roots
}

func proxyProtocolAddresses(t *testing.T) (string, string) {
	t.Helper()
	// Plugin servers are private; reserve both ports together, then release for Serve.
	plain, err := new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = plain.Close() }()
	ssl, err := new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ssl.Close() }()
	return plain.Addr().String(), ssl.Addr().String()
}

func proxyProtocolV2(ip string) string {
	src, dst, family := net.ParseIP(ip), net.ParseIP("2001:db8::1"), byte(0x21)
	length := byte(36)
	if v4 := src.To4(); v4 != nil {
		src, dst, family = v4, net.IPv4(192, 0, 2, 1).To4(), 0x11
		length = 12
	}
	// v2 PROXY, INET{,6}/STREAM, address length, addresses, ports 12345 -> 443.
	header := []byte("\r\n\r\n\x00\r\nQUIT\n\x21")
	header = append(header, family, 0, length)
	header = append(header, src...)
	header = append(header, dst...)
	return string(append(header, 0x30, 0x39, 0x01, 0xbb))
}
