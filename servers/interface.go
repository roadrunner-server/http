package servers

import (
	"github.com/roadrunner-server/http/v4/common"
)

// internal interface to start-stop http servers
type InternalServer[T any] interface {
	Serve(map[string]common.Middleware, []string) error
	Server() T
	Stop()
}
