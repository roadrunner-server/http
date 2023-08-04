package common

import (
	"context"
	"net/http"

	"github.com/roadrunner-server/sdk/v4/payload"
	"github.com/roadrunner-server/sdk/v4/pool"
	staticPool "github.com/roadrunner-server/sdk/v4/pool/static_pool"
	"github.com/roadrunner-server/sdk/v4/worker"
	"go.uber.org/zap"
)

type Pool interface {
	// Workers returns worker list associated with the pool.
	Workers() (workers []*worker.Process)
	// Exec payload
	Exec(ctx context.Context, p *payload.Payload, stopCh chan struct{}) (chan *staticPool.PExec, error)
	// Reset kill all workers inside the watcher and replaces with new
	Reset(ctx context.Context) error
	// Destroy all underlying stack (but let them complete the task).
	Destroy(ctx context.Context)
}

// Server creates workers for the application.
type Server interface {
	UID() int
	GID() int
	NewPool(ctx context.Context, cfg *pool.Config, env map[string]string, _ *zap.Logger) (*staticPool.Pool, error)
}

// Middleware represents http stdlib middleware interface
type Middleware interface {
	Middleware(f http.Handler) http.Handler
	Name() string
}

type Configurer interface {
	// UnmarshalKey takes a single key and unmarshal it into a Struct.
	UnmarshalKey(name string, out any) error
	// Has checks if config section exists.
	Has(name string) bool
}

type Logger interface {
	NamedLogger(name string) *zap.Logger
}
