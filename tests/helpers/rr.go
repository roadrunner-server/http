package helpers

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	mocklogger "tests/mock"

	"github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	"github.com/roadrunner-server/logger/v6"
	"github.com/stretchr/testify/require"
)

const (
	// defaultConfigVersion is the config schema version used by the test configs.
	defaultConfigVersion = "2023.3.5"
	// probeTimeout caps how long Start waits for the server to answer the probe.
	probeTimeout = time.Second * 15
	probeTick    = time.Millisecond * 20
	probeDial    = time.Second
)

// bootCfg holds the options applied to a container before it is started.
type bootCfg struct {
	version      string
	inline       string
	logLevel     slog.Level
	graceful     time.Duration
	logger       loggerKind
	probe        func(ctx context.Context) bool
	experimental bool
}

// loggerKind selects which logger plugin Start registers.
type loggerKind int

const (
	realLogger loggerKind = iota
	observedLogger
	noLogger
)

// Option customizes the container built by Start and its error-path variants.
type Option func(*bootCfg)

// WithConfigVersion overrides the config schema version.
func WithConfigVersion(v string) Option {
	return func(b *bootCfg) { b.version = v }
}

// WithInlineConfig feeds the container YAML from memory; the cfgPath argument is ignored.
func WithInlineConfig(yaml string) Option {
	return func(b *bootCfg) { b.inline = yaml }
}

// WithExperimentalFeatures sets ExperimentalFeatures on the config plugin, which
// gates the features still behind a flag, such as the http3 server.
func WithExperimentalFeatures() Option {
	return func(b *bootCfg) { b.experimental = true }
}

// WithLogLevel sets the endure container log level (debug by default).
func WithLogLevel(l slog.Level) Option {
	return func(b *bootCfg) { b.logLevel = l }
}

// WithGracefulTimeout sets the endure graceful shutdown timeout.
func WithGracefulTimeout(d time.Duration) Option {
	return func(b *bootCfg) { b.graceful = d }
}

// WithObservedLogger registers an in-memory logger instead of the real logger
// plugin and exposes the captured records as RR.Logs.
func WithObservedLogger() Option {
	return func(b *bootCfg) { b.logger = observedLogger }
}

// WithoutLogger registers no logger plugin at all.
func WithoutLogger() Option {
	return func(b *bootCfg) { b.logger = noLogger }
}

// WithProbe makes Start return only once a GET to url gets a response. The probe
// reaches the worker pool and produces one access-log record, so tests asserting
// exact log counts want WithTCPProbe instead.
func WithProbe(url string) Option {
	return func(b *bootCfg) {
		b.probe = func(ctx context.Context) bool {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return false
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return false
			}

			_ = resp.Body.Close()
			return true
		}
	}
}

// WithTCPProbe makes Start return only once addr accepts a connection. The
// listener binds after the worker pool is allocated, so this proves readiness
// without sending a request.
func WithTCPProbe(addr string) Option {
	return func(b *bootCfg) {
		b.probe = func(ctx context.Context) bool {
			d := net.Dialer{Timeout: probeDial}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return false
			}

			_ = conn.Close()
			return true
		}
	}
}

// RR is a running container.
type RR struct {
	// Logs holds the captured log records, non-nil only with WithObservedLogger.
	Logs *mocklogger.ObservedLogs

	mu   sync.Mutex
	errs []error
}

// Errs returns the errors the container reported on its error channel so far.
func (rr *RR) Errs() []error {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return append([]error(nil), rr.errs...)
}

func (rr *RR) addErr(err error) {
	rr.mu.Lock()
	rr.errs = append(rr.errs, err)
	rr.mu.Unlock()
}

// Start registers the plugins, boots the container and waits for the probe, if
// any, to answer. Errors arriving on the container channel are reported through
// t.Errorf and stop the container, but they do not abort the test.
//
// The returned stop is idempotent and also registered with t.Cleanup, so tests
// asserting on logs written during shutdown can stop the container mid-test.
func Start(t *testing.T, cfgPath string, plugins []any, opts ...Option) (*RR, func()) {
	t.Helper()

	cont, rr, bc := newContainer(t, cfgPath, plugins, opts)
	require.NoError(t, cont.Init())

	ch, err := cont.Serve()
	require.NoError(t, err)

	stopCont := sync.OnceValue(cont.Stop)
	done := make(chan struct{})
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		for {
			select {
			case res := <-ch:
				if res == nil {
					return
				}
				rr.addErr(res.Error)
				t.Errorf("plugin %s reported an error: %v", res.VertexID, res.Error)
				if errS := stopCont(); errS != nil {
					t.Errorf("container stop: %v", errS)
				}
			case <-done:
				if errS := stopCont(); errS != nil {
					t.Errorf("container stop: %v", errS)
				}
				return
			}
		}
	})

	// The drain goroutine calls t.Errorf, so it has to be joined while the test
	// is still running.
	stop := sync.OnceFunc(func() {
		close(done)
		wg.Wait()
	})
	t.Cleanup(stop)

	if bc.probe != nil {
		require.Eventually(t, func() bool { return bc.probe(t.Context()) }, probeTimeout, probeTick, "server did not become ready")
	}

	return rr, stop
}

// StartExpectInitError registers the plugins and requires Init to fail, returning its error.
func StartExpectInitError(t *testing.T, cfgPath string, plugins []any, opts ...Option) error {
	t.Helper()

	cont, _, _ := newContainer(t, cfgPath, plugins, opts)

	err := cont.Init()
	require.Error(t, err)

	return err
}

// StartExpectServeError registers the plugins, requires Init to pass and Serve to
// fail, and returns the Serve error.
func StartExpectServeError(t *testing.T, cfgPath string, plugins []any, opts ...Option) error {
	t.Helper()

	cont, _, _ := newContainer(t, cfgPath, plugins, opts)
	require.NoError(t, cont.Init())

	_, err := cont.Serve()
	require.Error(t, err)
	t.Cleanup(func() { _ = cont.Stop() })

	return err
}

// newContainer builds the container and registers the config, a logger and the
// caller's plugins. The container is not initialized yet.
func newContainer(t *testing.T, cfgPath string, plugins []any, opts []Option) (*endure.Endure, *RR, *bootCfg) {
	t.Helper()

	bc := &bootCfg{version: defaultConfigVersion, logLevel: slog.LevelDebug}
	for _, o := range opts {
		o(bc)
	}

	cfg := &config.Plugin{Version: bc.version, ExperimentalFeatures: bc.experimental}
	if bc.inline != "" {
		cfg.Type = "yaml"
		cfg.ReadInCfg = []byte(bc.inline)
	} else {
		cfg.Path = cfgPath
	}

	var endureOpts []endure.Options
	if bc.graceful != 0 {
		endureOpts = append(endureOpts, endure.GracefulShutdownTimeout(bc.graceful))
	}

	rr := &RR{}
	all := []any{cfg}

	switch bc.logger {
	case realLogger:
		all = append(all, &logger.Plugin{})
	case observedLogger:
		l, obs := mocklogger.SlogTestLogger(slog.LevelDebug)
		rr.Logs = obs
		all = append(all, l)
	case noLogger:
	}

	cont := endure.New(bc.logLevel, endureOpts...)
	require.NoError(t, cont.RegisterAll(append(all, plugins...)...))

	return cont, rr, bc
}
