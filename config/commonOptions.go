package config

import (
	"github.com/roadrunner-server/sdk/v2/pool"
)

type CommonOptions struct {
	// RawBody if turned on, RR will not parse the incoming HTTP body and will send it as is
	RawBody bool `mapstructure:"raw_body"`

	// Host and port to handle as http server.
	Address string `mapstructure:"address"`

	// AccessLogs turn on/off, logged at Info log level, default: false
	AccessLogs bool `mapstructure:"access_logs"`

	// List of the middleware names (order will be preserved)
	Middleware []string `mapstructure:"middleware"`

	// Pool configures worker pool.
	Pool *pool.Config `mapstructure:"pool"`

	// InternalErrorCode used to override default 500 (InternalServerError) http code
	InternalErrorCode uint64 `mapstructure:"internal_error_code"`

	// MaxRequestSize specified max size for payload body in megabytes, set 0 to unlimited.
	MaxRequestSize uint64 `mapstructure:"max_request_size"`
}
