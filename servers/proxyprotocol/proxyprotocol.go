package proxyprotocol

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pires/go-proxyproto"
)

// Config enables PROXY protocol on one TCP application listener.
type Config struct {
	TrustedProxies    []string      `mapstructure:"trusted_proxies"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`

	policy proxyproto.ConnPolicyFunc
}

// InitDefaults validates the listener address and compiles its trusted-peer policy.
func (c *Config) InitDefaults(address string) error {
	c.policy = nil
	_, port, err := net.SplitHostPort(strings.TrimPrefix(address, "tcp://"))
	if err != nil {
		return fmt.Errorf("requires a TCP listen address: %w", err)
	}
	if _, err = strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("invalid TCP listen port: %w", err)
	}
	if len(c.TrustedProxies) == 0 {
		return errors.New("trusted_proxies must contain at least one IP address or CIDR")
	}
	if c.ReadHeaderTimeout < 0 {
		return errors.New("read_header_timeout must not be negative")
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = 5 * time.Second
	}

	policy, err := proxyproto.TrustProxyHeaderFromRanges(c.TrustedProxies)
	if err != nil {
		return fmt.Errorf("trusted_proxies: %w", err)
	}
	c.policy = func(opts proxyproto.ConnPolicyOptions) (proxyproto.Policy, error) {
		// Match the kernel peer's IP, not its interface zone (which the policy cannot parse).
		if addr, ok := opts.Upstream.(*net.TCPAddr); ok && addr.Zone != "" {
			peer := *addr
			peer.Zone = ""
			opts.Upstream = &peer
		}
		return policy(opts)
	}
	return nil
}

// Wrap leaves a nil configuration disabled. Otherwise InitDefaults must have succeeded.
func (c *Config) Wrap(listener net.Listener) (net.Listener, error) {
	if c == nil {
		return listener, nil
	}
	if c.policy == nil {
		return nil, errors.New("proxy_protocol configuration is not initialized")
	}
	if listener.Addr().Network() != "tcp" {
		return nil, errors.New("proxy_protocol requires a TCP listener")
	}

	return &proxyproto.Listener{
		Listener:          listener,
		ConnPolicy:        c.policy,
		ReadHeaderTimeout: c.ReadHeaderTimeout,
		ValidateHeader: func(h *proxyproto.Header) error {
			// LOCAL (including v1 UNKNOWN) retains socket addresses, regardless of transport.
			if h.Command.IsLocal() || h.TransportProtocol == proxyproto.TCPv4 || h.TransportProtocol == proxyproto.TCPv6 {
				return nil
			}
			return errors.New("proxy_protocol requires TCP4 or TCP6 client addresses")
		},
	}, nil
}
