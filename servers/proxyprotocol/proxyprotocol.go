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

// InitDefaults validates the TCP address and builds the proxy trust policy.
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
		// The trust policy cannot parse IPv6 interface zones.
		if addr, ok := opts.Upstream.(*net.TCPAddr); ok && addr.Zone != "" {
			peer := *addr
			peer.Zone = ""
			opts.Upstream = &peer
		}
		return policy(opts)
	}
	return nil
}

// Wrap returns the original listener for a nil config. Other configs require InitDefaults.
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
			// LOCAL and v1 UNKNOWN retain the socket addresses.
			if h.Command.IsLocal() || h.TransportProtocol == proxyproto.TCPv4 || h.TransportProtocol == proxyproto.TCPv6 {
				return nil
			}
			return errors.New("proxy_protocol requires TCP4 or TCP6 client addresses")
		},
	}, nil
}
