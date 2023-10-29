package http

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/roadrunner-server/endure/v2/dep"
	"github.com/roadrunner-server/http/v4/common"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v4/config"
	"github.com/roadrunner-server/http/v4/handler"
	"github.com/roadrunner-server/sdk/v4/metrics"
	"github.com/roadrunner-server/sdk/v4/state/process"
	"github.com/roadrunner-server/sdk/v4/utils"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	jprop "go.opentelemetry.io/contrib/propagators/jaeger"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	// PluginName declares plugin name.
	PluginName        = "http"
	MB         uint64 = 1024 * 1024

	// configuration sections
	sectionHTTPS   = "http.ssl"
	sectionHTTP2   = "http.http2"
	sectionFCGI    = "http.fcgi"
	sectionUploads = "http.uploads"

	// RrMode RR_HTTP env variable key (internal) if the HTTP presents
	RrMode     = "RR_MODE"
	RrModeHTTP = "http"
)

// internal interface to start-stop http servers
type internalServer interface {
	Serve(map[string]common.Middleware, []string) error
	Server() *http.Server
	Stop()
}

// Plugin manages pool, http servers. The main http plugin structure
type Plugin struct {
	mu sync.RWMutex

	// otel propagators
	prop propagation.TextMapPropagator

	// plugins
	server common.Server
	log    *zap.Logger
	// stdlog passed to the http/https/fcgi servers to log their internal messages
	stdLog *log.Logger

	// http configuration
	cfg *config.Config

	// middlewares to chain
	mdwr map[string]common.Middleware
	// Pool which attached to all servers
	pool common.Pool
	// servers RR handler
	handler *handler.Handler
	// metrics
	statsExporter *metrics.StatsExporter
	// servers
	servers []internalServer
}

// Init must return configure svc and return true if svc hasStatus enabled. Must return error in case of
// misconfiguration. Services must not be used without proper configuration pushed first.
func (p *Plugin) Init(cfg common.Configurer, rrLogger common.Logger, srv common.Server) error {
	const op = errors.Op("http_plugin_init")
	if !cfg.Has(PluginName) {
		return errors.E(op, errors.Disabled)
	}

	err := p.unmarshal(cfg)
	if err != nil {
		return errors.E(op, err)
	}

	err = p.cfg.InitDefaults()
	if err != nil {
		return errors.E(op, err)
	}

	// get permissions
	p.cfg.UID = srv.UID()
	p.cfg.GID = srv.GID()

	// rr logger (via plugin)
	p.log = rrLogger.NamedLogger(PluginName)

	// use time and date in UTC format
	p.stdLog = log.New(NewStdAdapter(p.log), "http_plugin: ", log.Ldate|log.Ltime|log.LUTC)
	p.mdwr = make(map[string]common.Middleware)

	if !p.cfg.EnableHTTP() && !p.cfg.EnableTLS() && !p.cfg.EnableFCGI() {
		return errors.E(op, errors.Disabled)
	}

	// initialize statsExporter
	p.statsExporter = newWorkersExporter(p)
	p.server = srv
	p.servers = make([]internalServer, 0, 4)
	p.prop = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}, jprop.Jaeger{})

	return nil
}

// Serve serves the svc.
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 2)

	p.mu.Lock()
	defer p.mu.Unlock()

	var err error
	p.pool, err = p.server.NewPool(context.Background(), p.cfg.Pool, map[string]string{RrMode: RrModeHTTP}, p.log)
	if err != nil {
		errCh <- err
		return errCh
	}

	p.handler, err = handler.NewHandler(
		p.cfg,
		p.pool,
		p.log,
	)
	if err != nil {
		errCh <- err
		return errCh
	}

	// initialize servers based on the configuration
	err = p.initServers()
	if err != nil {
		errCh <- err
		return errCh
	}

	// apply access_logs, max_request, redirect middleware if specified by user
	p.applyBundledMiddleware()

	// start all servers
	for i := 0; i < len(p.servers); i++ {
		go func(idx int) {
			errSt := p.servers[idx].Serve(p.mdwr, p.cfg.Middleware)
			if errSt != nil {
				errCh <- errSt
				return
			}
		}(i)
	}

	return errCh
}

// Stop stops the http.
func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	doneCh := make(chan struct{}, 1)

	go func() {
		for i := 0; i < len(p.servers); i++ {
			if p.servers[i] != nil {
				p.servers[i].Stop()
			}
		}
		doneCh <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-doneCh:
		return nil
	}
}

// ServeHTTP handles connection using set of middleware and pool PSR-7 server.
func (p *Plugin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if val, ok := r.Context().Value(utils.OtelTracerNameKey).(string); ok {
		tp := trace.SpanFromContext(r.Context()).TracerProvider()
		ctx, span := tp.Tracer(val, trace.WithSchemaURL(semconv.SchemaURL),
			trace.WithInstrumentationVersion(otelhttp.Version())).
			Start(r.Context(), PluginName, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		// inject
		p.prop.Inject(ctx, propagation.HeaderCarrier(r.Header))
		r = r.WithContext(ctx)
	}

	// protect the case, when user sends Reset, and we are replacing handler with pool
	p.mu.RLock()
	p.handler.ServeHTTP(w, r)
	p.mu.RUnlock()

	_ = r.Body.Close()
}

// Workers returns slice with the process states for the workers
func (p *Plugin) Workers() []*process.State {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return nil
	}

	workers := p.pool.Workers()

	ps := make([]*process.State, 0, len(workers))
	for i := 0; i < len(workers); i++ {
		state, err := process.WorkerProcessState(workers[i])
		if err != nil {
			return nil
		}
		ps = append(ps, state)
	}

	return ps
}

// Name returns endure.Named interface implementation
func (p *Plugin) Name() string {
	return PluginName
}

// Reset destroys the old pool and replaces it with new one, waiting for old pool to die
func (p *Plugin) Reset() error {
	const op = errors.Op("http_plugin_reset")

	p.mu.Lock()
	defer p.mu.Unlock()

	p.log.Info("reset signal was received")

	if p.pool == nil {
		p.log.Info("pool is nil, nothing to reset")
		return nil
	}

	err := p.pool.Reset(context.Background())
	if err != nil {
		return errors.E(op, err)
	}

	p.log.Info("plugin was successfully reset")
	return nil
}

// Collects collecting http middlewares
func (p *Plugin) Collects() []*dep.In {
	return []*dep.In{
		dep.Fits(func(pp any) {
			mdw := pp.(common.Middleware)
			// just to be safe
			p.mu.Lock()
			p.mdwr[mdw.Name()] = mdw
			p.mu.Unlock()
		}, (*common.Middleware)(nil)),
	}
}
