package proxyprotocol_test

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
	"github.com/roadrunner-server/http/v6/servers/proxyprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	proxyLine = "PROXY TCP4 192.0.2.1 198.51.100.2 12345 443\r\n"
	payload   = "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n"
)

func TestConfigInitDefaults(t *testing.T) {
	for _, tt := range []struct {
		name    string
		proxies []string
		address string
		timeout time.Duration
		invalid bool
	}{
		{"ipv4", []string{"127.0.0.1"}, "127.0.0.1:8080", 0, false},
		{"ipv6", []string{"::1"}, "[::1]:8080", 0, false},
		{"cidrs and tcp prefix", []string{"192.0.2.0/24", "2001:db8::/32"}, "tcp://127.0.0.1:8080", 0, false},
		{"wildcard and explicit timeout", []string{"127.0.0.1"}, ":0", time.Second, false},
		{"empty list", nil, ":8080", 0, true},
		{"empty entry", []string{""}, ":8080", 0, true},
		{"blank entry", []string{" "}, ":8080", 0, true},
		{"hostname", []string{"localhost"}, ":8080", 0, true},
		{"invalid ip", []string{"999.0.0.1"}, ":8080", 0, true},
		{"invalid cidr", []string{"127.0.0.1/33"}, ":8080", 0, true},
		{"mixed valid and invalid", []string{"127.0.0.1", "invalid"}, ":8080", 0, true},
		{"negative timeout", []string{"127.0.0.1"}, ":8080", -time.Second, true},
		{"empty address", []string{"127.0.0.1"}, "", 0, true},
		{"missing port", []string{"127.0.0.1"}, "127.0.0.1", 0, true},
		{"invalid port", []string{"127.0.0.1"}, "127.0.0.1:invalid", 0, true},
		{"port out of range", []string{"127.0.0.1"}, "127.0.0.1:65536", 0, true},
		{"unix address", []string{"127.0.0.1"}, "unix:///tmp/proxy.sock", 0, true},
		{"udp address", []string{"127.0.0.1"}, "udp://127.0.0.1:8080", 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &proxyprotocol.Config{TrustedProxies: tt.proxies, ReadHeaderTimeout: tt.timeout}
			err := cfg.InitDefaults(tt.address)
			if tt.invalid {
				require.Error(t, err)
				_, err = cfg.Wrap(&scriptedListener{addr: &net.TCPAddr{}})
				assert.Error(t, err, "failed initialization must not enable the listener")
				return
			}
			require.NoError(t, err)
			want := tt.timeout
			if want == 0 {
				want = 5 * time.Second
			}
			assert.Equal(t, want, cfg.ReadHeaderTimeout)
		})
	}
}

func TestConfigWrap(t *testing.T) {
	raw := tcpListener(t)
	unix := &scriptedListener{addr: &net.UnixAddr{Net: "unix", Name: "unused"}}
	for _, ln := range []net.Listener{raw, unix} {
		wrapped, err := (*proxyprotocol.Config)(nil).Wrap(ln)
		require.NoError(t, err)
		assert.Same(t, ln, wrapped)
	}

	cfg := &proxyprotocol.Config{TrustedProxies: []string{"127.0.0.1"}, ReadHeaderTimeout: time.Second}
	_, err := cfg.Wrap(raw)
	require.Error(t, err, "populated but uninitialized configuration must fail closed")

	for _, timeout := range []time.Duration{0, 123 * time.Millisecond} {
		cfg.ReadHeaderTimeout = timeout
		require.NoError(t, cfg.InitDefaults(raw.Addr().String()))
		wrapped, err := cfg.Wrap(raw)
		require.NoError(t, err)
		require.IsType(t, &proxyproto.Listener{}, wrapped)
		assert.Equal(t, cfg.ReadHeaderTimeout, wrapped.(*proxyproto.Listener).ReadHeaderTimeout)
		assert.Equal(t, raw.Addr(), wrapped.Addr())
	}
	_, err = cfg.Wrap(unix)
	assert.Error(t, err)
}

func TestWrapHeaders(t *testing.T) {
	ipv4 := string(net.ParseIP("192.0.2.1").To4()) + string(net.ParseIP("198.51.100.2").To4()) + "\x30\x39\x01\xbb"
	ipv6 := string(net.ParseIP("2001:db8::1").To16()) + string(net.ParseIP("2001:db8::2").To16()) + "\x30\x39\x01\xbb"
	unix := strings.Repeat("\x00", 216)
	for _, tt := range []struct {
		name, header, remote, local string
		reject                      bool
	}{
		{"v1 ipv4", proxyLine, "192.0.2.1:12345", "198.51.100.2:443", false},
		{"v1 ipv6", "PROXY TCP6 2001:db8::1 2001:db8::2 12345 443\r\n", "[2001:db8::1]:12345", "[2001:db8::2]:443", false},
		{"v2 ipv4", v2Frame(0x21, 0x11, ipv4), "192.0.2.1:12345", "198.51.100.2:443", false},
		{"v2 ipv6", v2Frame(0x21, 0x21, ipv6), "[2001:db8::1]:12345", "[2001:db8::2]:443", false},
		{"v2 opaque tlv", v2Frame(0x21, 0x11, ipv4+"\xe0\x00\x03abc"), "192.0.2.1:12345", "198.51.100.2:443", false},
		{"v1 unknown", "PROXY UNKNOWN\r\n", "", "", false},
		{"v2 local", v2Frame(0x20, 0x00, ""), "", "", false},
		{"v2 local opaque payload", v2Frame(0x20, 0x00, "\xff\x00opaque"), "", "", false},
		{"v2 local ignores advertised transport", v2Frame(0x20, 0x12, ipv4), "", "", false},
		{"headerless", "", "", "", true},
		{"malformed", "PROXY TCP4 invalid 198.51.100.2 12345 443\r\n", "", "", true},
		{"udp4", v2Frame(0x21, 0x12, ipv4), "", "", true},
		{"udp6", v2Frame(0x21, 0x22, ipv6), "", "", true},
		{"unix stream", v2Frame(0x21, 0x31, unix), "", "", true},
		{"unix datagram", v2Frame(0x21, 0x32, unix), "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, client := tcpPair(t, time.Second)
			_, err := io.WriteString(client, tt.header+payload)
			require.NoError(t, err)
			// Close input so parser rejection cannot depend on a read timeout.
			require.NoError(t, client.CloseWrite())
			body, err := io.ReadAll(server)
			if tt.reject {
				require.Error(t, err)
				assert.Empty(t, body, "rejected connections must not expose application bytes")
				if timeout, ok := errors.AsType[net.Error](err); ok {
					assert.False(t, timeout.Timeout(), "a safety deadline is not a rejection")
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, payload, string(body), "only the coalesced application payload should remain")
			remote, local := tt.remote, tt.local
			if remote == "" {
				remote, local = client.LocalAddr().String(), client.RemoteAddr().String()
			}
			assert.Equal(t, remote, server.RemoteAddr().String())
			assert.Equal(t, local, server.LocalAddr().String())
		})
	}
}

func TestWrapHeaderTimeout(t *testing.T) {
	server, _ := tcpPair(t, 50*time.Millisecond)
	start := time.Now()
	n, err := server.Read(make([]byte, 1))
	require.Error(t, err)
	assert.Zero(t, n)
	assert.Less(t, time.Since(start), 2*time.Second, "header timeout must fire before the 5s safety deadline")
}

func TestWrapHeaderDeadlineRestored(t *testing.T) {
	server, client := tcpPair(t, 100*time.Millisecond)
	_, err := io.WriteString(client, proxyLine)
	require.NoError(t, err)
	require.Equal(t, "192.0.2.1:12345", server.RemoteAddr().String())

	// Wait past the header deadline without HTTP resetting it.
	time.Sleep(200 * time.Millisecond)
	_, err = io.WriteString(client, payload)
	require.NoError(t, err)
	body := make([]byte, len(payload))
	_, err = io.ReadFull(server, body)
	require.NoError(t, err)
	assert.Equal(t, payload, string(body))
}

func TestWrapDropsUntrustedAndContinues(t *testing.T) {
	for _, tt := range []struct {
		name, trusted, good, bad, zone string
	}{
		{"ipv4 literal", "192.0.2.10", "192.0.2.10", "192.0.2.11", ""},
		{"ipv4 cidr", "192.0.2.0/24", "192.0.2.10", "198.51.100.10", ""},
		{"ipv6 literal", "2001:db8::10", "2001:db8::10", "2001:db8::11", ""},
		{"ipv6 cidr", "2001:db8:1::/48", "2001:db8:1::10", "2001:db8:2::10", ""},
		{"scoped ipv6 literal", "fe80::10", "fe80::10", "fe80::11", "eth0"},
		{"scoped ipv6 cidr", "fe80::/64", "fe80::10", "fe80:0:0:1::10", "eth0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ln := &scriptedListener{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}}
			t.Cleanup(func() { _ = ln.Close() })
			writes := make([]<-chan error, 0, 3)
			for _, peer := range []struct{ ip, wire string }{
				{tt.bad, payload},
				{tt.bad, proxyLine + payload},
				{tt.good, proxyLine + payload},
			} {
				server, client := net.Pipe()
				t.Cleanup(func() { _ = client.Close() })
				ln.conns = append(ln.conns, &addrConn{
					Conn: server, local: ln.addr,
					remote: &net.TCPAddr{IP: net.ParseIP(peer.ip), Port: 1234, Zone: tt.zone},
				})
				require.NoError(t, client.SetDeadline(time.Now().Add(5*time.Second)))
				written := make(chan error, 1)
				writes = append(writes, written)
				go func() {
					_, err := io.WriteString(client, peer.wire)
					written <- err
				}()
			}
			cfg := &proxyprotocol.Config{
				TrustedProxies: []string{"203.0.113.254", tt.trusted}, ReadHeaderTimeout: time.Second,
			}
			require.NoError(t, cfg.InitDefaults(ln.Addr().String()))
			wrapped, err := cfg.Wrap(ln)
			require.NoError(t, err)
			conn, err := wrapped.Accept()
			require.NoError(t, err, "dropping peers must not terminate Accept")
			require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
			body := make([]byte, len(payload))
			_, err = io.ReadFull(conn, body)
			require.NoError(t, err)
			assert.Equal(t, payload, string(body))
			assert.Equal(t, "192.0.2.1:12345", conn.RemoteAddr().String())
			for i, written := range writes {
				if i < 2 {
					assert.ErrorIs(t, <-written, io.ErrClosedPipe, "untrusted peers must be closed, not time out")
				} else {
					assert.NoError(t, <-written)
				}
			}
		})
	}
}

func v2Frame(command, transport byte, body string) string {
	return "\r\n\r\n\x00\r\nQUIT\n" + string([]byte{command, transport, byte((len(body) >> 8) & 0xff), byte(len(body) & 0xff)}) + body
}

func tcpListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	require.NoError(t, ln.(*net.TCPListener).SetDeadline(time.Now().Add(5*time.Second)))
	return ln
}

func tcpPair(t *testing.T, timeout time.Duration) (net.Conn, *net.TCPConn) {
	t.Helper()
	ln := tcpListener(t)
	cfg := &proxyprotocol.Config{TrustedProxies: []string{"127.0.0.1"}, ReadHeaderTimeout: timeout}
	require.NoError(t, cfg.InitDefaults(ln.Addr().String()))
	wrapped, err := cfg.Wrap(ln)
	require.NoError(t, err)
	client, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.SetDeadline(time.Now().Add(5*time.Second)))
	server, err := wrapped.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	require.NoError(t, server.SetDeadline(time.Now().Add(5*time.Second)))
	return server, client.(*net.TCPConn)
}

type addrConn struct {
	net.Conn
	local, remote net.Addr
}

func (c *addrConn) LocalAddr() net.Addr  { return c.local }
func (c *addrConn) RemoteAddr() net.Addr { return c.remote }

type scriptedListener struct {
	addr  net.Addr
	conns []net.Conn
	next  int
}

func (l *scriptedListener) Addr() net.Addr { return l.addr }
func (l *scriptedListener) Close() error {
	for _, conn := range l.conns {
		_ = conn.Close()
	}
	return nil
}
func (l *scriptedListener) Accept() (net.Conn, error) {
	if l.next == len(l.conns) {
		return nil, net.ErrClosed
	}
	conn := l.conns[l.next]
	l.next++
	return conn, nil
}
