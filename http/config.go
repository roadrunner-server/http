package http

type Config struct {
	// Host and port to handle as http server.
	Address string `mapstructure:"address"`

	// AccessLogs turn on/off, logged at Info log level, default: false
	AccessLogs bool `mapstructure:"access_logs"`

	// InternalErrorCode used to override default 500 (InternalServerError) http code
	InternalErrorCode uint64 `mapstructure:"internal_error_code"`

	// MaxRequestSize specified max size for payload body in megabytes, set 0 to unlimited.
	MaxRequestSize uint64 `mapstructure:"max_request_size"`
}
