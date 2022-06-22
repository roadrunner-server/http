package config

import (
	"runtime"
	"strings"
	"time"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v2/fcgi"
	"github.com/roadrunner-server/http/v2/http"
	"github.com/roadrunner-server/http/v2/https"
	"github.com/roadrunner-server/http/v2/uploads"
	"github.com/roadrunner-server/sdk/v2/pool"
)

// Config configures RoadRunner HTTP server.
type Config struct {
	// List of the middleware names (order will be preserved)
	Middleware []string `mapstructure:"middleware"`

	// Pool configures worker pool.
	Pool *pool.Config `mapstructure:"pool"`

	// HTTP related configuration
	HTTPConfig *http.Config `mapstructure:"http"`

	// SSLConfig defines https server options.
	SSLConfig *https.SSL `mapstructure:"ssl"`

	// FCGIConfig configuration. You can use FastCGI without HTTP server.
	FCGIConfig *fcgi.FCGI `mapstructure:"fcgi"`

	// HTTP2Config configuration
	HTTP2Config *https.HTTP2 `mapstructure:"http2"`

	// Uploads configures uploads configuration.
	Uploads *uploads.Uploads `mapstructure:"uploads"`
}

// EnableHTTP is true when http server must run.
func (c *Config) EnableHTTP() bool {
	return c.HTTPConfig.Address != ""
}

// EnableTLS returns true if pool must listen TLS connections.
func (c *Config) EnableTLS() bool {
	if c.SSLConfig == nil {
		return false
	}
	if c.SSLConfig.Acme != nil {
		return true
	}
	return c.SSLConfig.Key != "" || c.SSLConfig.Cert != ""
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
		// default pool
		c.Pool = &pool.Config{
			Debug:           false,
			NumWorkers:      uint64(runtime.NumCPU()),
			MaxJobs:         0,
			AllocateTimeout: time.Second * 60,
			DestroyTimeout:  time.Second * 60,
			Supervisor:      nil,
		}
	}

	if c.HTTPConfig == nil {
		c.HTTPConfig = &http.Config{}
	}

	if c.HTTPConfig.InternalErrorCode == 0 {
		c.HTTPConfig.InternalErrorCode = 500
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
		c.Uploads = &uploads.Uploads{}
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

	if c.HTTPConfig.Address != "" && !strings.Contains(c.HTTPConfig.Address, ":") {
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
