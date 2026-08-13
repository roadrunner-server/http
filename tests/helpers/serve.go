package helpers

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"tests/testLog"

	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/handler"
	"github.com/roadrunner-server/pool/v2/ipc/pipe"
	"github.com/roadrunner-server/pool/v2/pool"
	staticPool "github.com/roadrunner-server/pool/v2/pool/static_pool"
	"github.com/stretchr/testify/require"
)

// Served is a handler served on an ephemeral port by a worker pool.
type Served struct {
	// URL is the server root, e.g. http://127.0.0.1:53124.
	URL string
	// Addr is the host:port the server listens on.
	Addr string
	Pool *staticPool.Pool
}

// ServeHandler runs `php argv...` as a worker pool, wraps it in the http handler
// and serves it on an ephemeral port. The server and the pool are torn down by
// t.Cleanup. A nil hcfg or pcfg means the default configuration.
func ServeHandler(t testing.TB, argv []string, hcfg *config.Config, pcfg *pool.Config) *Served {
	t.Helper()

	if pcfg == nil {
		pcfg = defaultPoolConfig()
	}

	// Workers outlive t.Context(), which is canceled before the cleanups run. This
	// context is canceled last instead, so a graceful Destroy comes first and only
	// a worker that survived it gets killed.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	p, err := staticPool.NewPool(ctx,
		func(_ []string) *exec.Cmd {
			return exec.CommandContext(ctx, "php", argv...)
		},
		pipe.NewPipeFactory(testLog.SlogLogger()),
		pcfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { p.Destroy(context.Background()) })

	if hcfg == nil {
		hcfg = DefaultHandlerConfig()
	}

	h, err := handler.NewHandler(hcfg, p, testLog.SlogLogger())
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return &Served{
		URL:  srv.URL,
		Addr: strings.TrimPrefix(srv.URL, "http://"),
		Pool: p,
	}
}

// DefaultHandlerConfig is the handler configuration shared by the handler and
// uploads tests: 1 KB request limit, uploads in the system temp dir.
func DefaultHandlerConfig() *config.Config {
	return &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}
}

func defaultPoolConfig() *pool.Config {
	return &pool.Config{
		NumWorkers:      1,
		AllocateTimeout: time.Second * 1000,
		DestroyTimeout:  time.Second * 1000,
	}
}
