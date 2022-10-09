package fcgi

// FCGI for FastCGI server.
type FCGI struct {
	// Address and port to handle as http server.
	Address string `mapstructure:"address"`
}
