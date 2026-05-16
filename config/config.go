package config

import (
	"strings"
	"time"

	"github.com/roadrunner-server/http/v6/servers/fcgi"
	"github.com/roadrunner-server/http/v6/servers/http3"
	"github.com/roadrunner-server/http/v6/servers/https"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool/v2/pool"
)

// Config configures RoadRunner HTTP server.
type Config struct {
	// Host and port to handle as http server.
	Address string `mapstructure:"address"`
	// AccessLogs turn on/off, logged at Info log level, default: false
	AccessLogs bool `mapstructure:"access_logs"`
	// List of the middleware names (order will be preserved)
	Middleware []string `mapstructure:"middleware"`
	// Pool configures worker pool (lifecycle only; requests are delivered via Proxy).
	Pool *pool.Config `mapstructure:"pool"`
	// Proxy configures the worker-facing ConnectRPC server.
	Proxy *Proxy `mapstructure:"proxy"`
	// InternalErrorCode used to override default 500 (InternalServerError) http code
	InternalErrorCode uint64 `mapstructure:"internal_error_code"`
	// MaxRequestSize specified max size for payload body in megabytes. 0 = 1GB.
	MaxRequestSize uint64 `mapstructure:"max_request_size"`
	// SSLConfig defines https server options.
	SSLConfig *https.SSL `mapstructure:"ssl"`
	// FCGIConfig configuration. You can use FastCGI without HTTP server.
	FCGIConfig *fcgi.FCGI `mapstructure:"fcgi"`
	// HTTP2Config configuration
	HTTP2Config *https.HTTP2  `mapstructure:"http2"`
	HTTP3Config *http3.Config `mapstructure:"http3"`
	// Uploads configures uploads configuration.
	Uploads *Uploads `mapstructure:"uploads"`

	// private
	UID int
	GID int
}

// Proxy configures the ConnectRPC server that PHP workers connect into.
type Proxy struct {
	// Address is the TCP address the proxy server listens on, e.g. ":7070".
	Address string `mapstructure:"address"`
	// RequestTimeout caps how long a single request can sit waiting for a
	// worker to produce a response. Defaults to 60s.
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	// InboxSize bounds the in-process request queue. Submits beyond this
	// return 503 to the client. Defaults to 1024.
	InboxSize int `mapstructure:"inbox_size"`
	// DebugMode flips the handler into debug mode (verbose error bodies on 5xx).
	DebugMode bool `mapstructure:"debug"`
}

func (p *Proxy) InitDefaults() {
	if p.Address == "" {
		// Bind to loopback by default
		p.Address = "127.0.0.1:7070"
	}
	if p.RequestTimeout == 0 {
		p.RequestTimeout = time.Minute
	}
	if p.InboxSize == 0 {
		p.InboxSize = 1024
	}
}

// EnableHTTP is true when http server must run.
func (c *Config) EnableHTTP() bool {
	return c.Address != ""
}

// EnableHTTP3 is true when http server must run.
func (c *Config) EnableHTTP3() bool {
	return c.HTTP3Config != nil
}

// EnableTLS returns true if pool must listen TLS connections.
func (c *Config) EnableTLS() bool {
	if c.SSLConfig == nil {
		return false
	}
	if c.SSLConfig.Acme != nil {
		return true
	}
	return c.SSLConfig.Key != "" && c.SSLConfig.Cert != ""
}

// EnableFCGI is true when FastCGI server must be enabled.
func (c *Config) EnableFCGI() bool {
	if c.FCGIConfig == nil {
		return false
	}
	return c.FCGIConfig.Address != ""
}

// InitDefaults must populate HTTP values using given HTTP source. Must return error if HTTP is not valid.
func (c *Config) InitDefaults() error {
	if c.Pool == nil {
		c.Pool = &pool.Config{}
	}
	c.Pool.InitDefaults()

	if c.Proxy == nil {
		c.Proxy = &Proxy{}
	}
	c.Proxy.InitDefaults()

	if c.InternalErrorCode == 0 {
		c.InternalErrorCode = 500
	}

	if c.MaxRequestSize == 0 {
		// 1Gb
		c.MaxRequestSize = 1000
	}

	if c.HTTP2Config != nil {
		err := c.HTTP2Config.InitDefaults()
		if err != nil {
			return err
		}
	}

	if c.SSLConfig != nil {
		err := c.SSLConfig.InitDefaults()
		if err != nil {
			return err
		}
	}

	if c.Uploads == nil {
		c.Uploads = &Uploads{}
	}

	err := c.Uploads.InitDefaults()
	if err != nil {
		return err
	}

	return c.Valid()
}

// Valid validates the configuration.
func (c *Config) Valid() error {
	const op = errors.Op("validation")
	if c.Uploads == nil {
		return errors.E(op, errors.Str("malformed uploads config"))
	}

	if c.Pool == nil {
		return errors.E(op, "malformed pool config")
	}

	if !c.EnableHTTP() && !c.EnableTLS() && !c.EnableFCGI() {
		return errors.E(op, errors.Str("unable to run http service, no method has been specified (http, https, http/2 or FastCGI)"))
	}

	if c.Address != "" && !strings.Contains(c.Address, ":") {
		return errors.E(op, errors.Str("malformed http server address"))
	}

	if c.EnableTLS() {
		err := c.SSLConfig.Valid()
		if err != nil {
			return errors.E(op, err)
		}
	}

	return nil
}
