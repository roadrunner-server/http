package https

import (
	"os"
	"strconv"
	"strings"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v4/acme"
)

type ClientAuthType string

const (
	NoClientCert               ClientAuthType = "no_client_cert"
	RequestClientCert          ClientAuthType = "request_client_cert"
	RequireAnyClientCert       ClientAuthType = "require_any_client_cert"
	VerifyClientCertIfGiven    ClientAuthType = "verify_client_cert_if_given"
	RequireAndVerifyClientCert ClientAuthType = "require_and_verify_client_cert"
)

// HTTP2 HTTP/2 server customizations.
type HTTP2 struct {
	// h2cHandler is a Handler which implements h2c by hijacking the HTTP/1 traffic
	// that should be h2c traffic. There are two ways to begin a h2c connection
	// (RFC 7540 Section 3.2 and 3.4): (1) Starting with Prior Knowledge - this
	// works by starting an h2c connection with a string of bytes that is valid
	// HTTP/1, but unlikely to occur in practice and (2) Upgrading from HTTP/1 to
	// h2c - this works by using the HTTP/1 Upgrade header to request an upgrade to
	// h2c. When either of those situations occur we hijack the HTTP/1 connection,
	// convert it to a HTTP/2 connection and pass the net.Conn to http2.ServeConn.

	// H2C enables HTTP/2 over TCP
	H2C bool

	// MaxConcurrentStreams defaults to 128.
	MaxConcurrentStreams uint32 `mapstructure:"max_concurrent_streams"`
}

func (h2 *HTTP2) EnableHTTP2() bool {
	return h2 != nil && h2.H2C
}

// SSL defines https server configuration.
type SSL struct {
	// Address to listen as HTTPS server, defaults to 0.0.0.0:443.
	Address string
	// ACME configuration
	Acme *acme.Config `mapstructure:"acme"`
	// Redirect when enabled forces all http connections to switch to https.
	Redirect bool
	// Key defined private server key.
	Key string
	// Cert is https certificate.
	Cert string
	// Root CA file
	RootCA string `mapstructure:"root_ca"`
	// mTLS auth
	AuthType ClientAuthType `mapstructure:"client_auth_type"`
	// internal
	host string
	// internal
	Port int
}

// InitDefaults sets default values for HTTP/2 configuration.
func (h2 *HTTP2) InitDefaults() error {
	if h2.MaxConcurrentStreams == 0 {
		h2.MaxConcurrentStreams = 128
	}

	return nil
}

func (s *SSL) InitDefaults() error {
	if s.Acme != nil {
		err := s.Acme.InitDefaults()
		if err != nil {
			return err
		}
	}

	if s.Address == "" {
		s.Address = "127.0.0.1:443"
	}

	return nil
}

func (s *SSL) EnableACME() bool {
	if s == nil {
		return false
	}
	return s.Acme != nil
}

func (s *SSL) Valid() error {
	const op = errors.Op("ssl_valid")

	parts := strings.Split(s.Address, ":")
	switch len(parts) {
	// :443 form
	// 127.0.0.1:443 form
	// use 0.0.0.0 as host and 443 as port
	case 2:
		if parts[0] == "" {
			s.host = "127.0.0.1"
		} else {
			s.host = parts[0]
		}

		port, err := strconv.Atoi(parts[1])
		if err != nil {
			return errors.E(op, err)
		}
		s.Port = port
	default:
		return errors.E(op, errors.Errorf("unknown format, accepted format is [:<port> or <host>:<port>], provided: %s", s.Address))
	}

	// the user use they own certificates
	if s.Acme == nil {
		if _, err := os.Stat(s.Key); err != nil {
			if os.IsNotExist(err) {
				return errors.E(op, errors.Errorf("key file '%s' does not exists", s.Key))
			}

			return err
		}

		if _, err := os.Stat(s.Cert); err != nil {
			if os.IsNotExist(err) {
				return errors.E(op, errors.Errorf("cert file '%s' does not exists", s.Cert))
			}

			return err
		}
	}

	// RootCA is optional, but if provided - check it
	if s.RootCA != "" {
		if _, err := os.Stat(s.RootCA); err != nil {
			if os.IsNotExist(err) {
				return errors.E(op, errors.Errorf("root ca path provided, but path '%s' does not exists", s.RootCA))
			}
			return err
		}
	}

	return nil
}
