package servers

import (
	"github.com/roadrunner-server/http/v6/api"
)

// internal interface to start-stop http servers
type InternalServer[T any] interface {
	Serve(map[string]api.Middleware, []string) error
	Server() T
	Stop()
}
