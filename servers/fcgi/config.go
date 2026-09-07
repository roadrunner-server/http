package fcgi

import (
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/tcplisten"
)

// FCGI for FastCGI server.
type FCGI struct {
	// Address and port to handle as http server.
	Address string `mapstructure:"address"`
	// UnixSocket sets attributes on the FastCGI UNIX socket only.
	UnixSocket *tcplisten.UnixSocketOptions `mapstructure:"unix_socket"`
}

// Valid validates the FastCGI socket options.
func (c *FCGI) Valid() error {
	if err := c.UnixSocket.Validate(c.Address); err != nil {
		return errors.E(errors.Op("http.fcgi.unix_socket"), err)
	}
	return nil
}
