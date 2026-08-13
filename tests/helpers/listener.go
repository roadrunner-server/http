package helpers

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// ListenerTimeout caps how long WaitListener waits for an address to accept.
	ListenerTimeout = time.Second * 15
	// ListenerTick is the interval between two connection attempts.
	ListenerTick = time.Millisecond * 20
)

// WaitListener waits until addr accepts a connection. The plain, tls and fastcgi
// frontends of one config bind in parallel, so the readiness of the one Start
// probed says nothing about the others.
func WaitListener(t *testing.T, network, addr string) {
	t.Helper()

	require.Eventually(t, func() bool {
		var d net.Dialer

		conn, err := d.DialContext(t.Context(), network, addr)
		if err != nil {
			return false
		}

		return conn.Close() == nil
	}, ListenerTimeout, ListenerTick, "listener %s did not start", addr)
}
