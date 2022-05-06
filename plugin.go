package http

import (
	"context"
	stderr "errors"
	"log"
	"net/http"
	"sync"
	"time"

	cfgPlugin "github.com/roadrunner-server/api/v2/plugins/config"
	"github.com/roadrunner-server/api/v2/plugins/middleware"
	"github.com/roadrunner-server/api/v2/plugins/server"
	"github.com/roadrunner-server/api/v2/plugins/status"
	"github.com/roadrunner-server/api/v2/pool"
	"github.com/roadrunner-server/api/v2/state/process"
	"github.com/roadrunner-server/api/v2/worker"
	endure "github.com/roadrunner-server/endure/pkg/container"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v2/config"
	"github.com/roadrunner-server/http/v2/fcgi"
	"github.com/roadrunner-server/http/v2/handler"
	"github.com/roadrunner-server/http/v2/helpers"
	httpServer "github.com/roadrunner-server/http/v2/http"
	tlsServer "github.com/roadrunner-server/http/v2/https"
	bundledMw "github.com/roadrunner-server/http/v2/middleware"
	"github.com/roadrunner-server/sdk/v2/metrics"
	pstate "github.com/roadrunner-server/sdk/v2/state/process"
	"go.uber.org/zap"
)

const (
	// PluginName declares plugin name.
	PluginName = "http"

	// configuration sections
	sectionHTTPS   = "http.ssl"
	sectionHTTP2   = "http.http2"
	sectionFCGI    = "http.fcgi"
	sectionUploads = "http.uploads"

	// RrMode RR_HTTP env variable key (internal) if the HTTP presents
	RrMode = "RR_MODE"

	Scheme = "https"
)

// internal interface to start-stop http servers
type internalServer interface {
	Start(map[string]middleware.Middleware, []string) error
	GetServer() *http.Server
	Stop()
}

// Plugin manages pool, http servers. The main http plugin structure
type Plugin struct {
	mu sync.RWMutex

	// plugins
	server server.Server
	log    *zap.Logger
	// stdlog passed to the http/https/fcgi servers to log their internal messages
	stdLog *log.Logger

	// http configuration
	cfg *config.Config

	// middlewares to chain
	mdwr map[string]middleware.Middleware

	// Pool which attached to all servers
	pool pool.Pool
	// servers RR handler
	handler *handler.Handler
	// metrics
	statsExporter *metrics.StatsExporter
	// servers
	servers []internalServer
}

// Init must return configure svc and return true if svc hasStatus enabled. Must return error in case of
// misconfiguration. Services must not be used without proper configuration pushed first.
func (p *Plugin) Init(cfg cfgPlugin.Configurer, rrLogger *zap.Logger, srv server.Server) error {
	const op = errors.Op("http_plugin_init")
	if !cfg.Has(PluginName) {
		return errors.E(op, errors.Disabled)
	}

	// unmarshal general section
	err := cfg.UnmarshalKey(PluginName, &p.cfg)
	if err != nil {
		return errors.E(op, err)
	}

	// unmarshal HTTP section
	err = cfg.UnmarshalKey(PluginName, &p.cfg.HTTPConfig)
	if err != nil {
		return errors.E(op, err)
	}

	// unmarshal HTTPS section
	err = cfg.UnmarshalKey(sectionHTTPS, &p.cfg.SSLConfig)
	if err != nil {
		return errors.E(op, err)
	}

	// unmarshal H2C section
	err = cfg.UnmarshalKey(sectionHTTP2, &p.cfg.HTTP2Config)
	if err != nil {
		return errors.E(op, err)
	}

	// unmarshal uploads section
	err = cfg.UnmarshalKey(sectionUploads, &p.cfg.Uploads)
	if err != nil {
		return errors.E(op, err)
	}

	// unmarshal fcgi section
	err = cfg.UnmarshalKey(sectionFCGI, &p.cfg.FCGIConfig)
	if err != nil {
		return errors.E(op, err)
	}

	err = p.cfg.InitDefaults()
	if err != nil {
		return errors.E(op, err)
	}

	// rr logger (via plugin)
	p.log = new(zap.Logger)
	*p.log = *rrLogger

	// use time and date in UTC format
	p.stdLog = log.New(helpers.NewStdAdapter(p.log), "http_plugin: ", log.Ldate|log.Ltime|log.LUTC)
	p.mdwr = make(map[string]middleware.Middleware)

	if !p.cfg.EnableHTTP() && !p.cfg.EnableTLS() && !p.cfg.EnableFCGI() {
		return errors.E(op, errors.Disabled)
	}

	// initialize statsExporter
	p.statsExporter = newWorkersExporter(p)
	p.server = srv
	p.servers = make([]internalServer, 0, 4)

	return nil
}

// Serve serves the svc.
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 2)
	// run whole process in the goroutine, needed for the PHP
	go func() {
		// protect http initialization
		p.mu.Lock()
		p.serve(errCh)
		p.mu.Unlock()
	}()

	return errCh
}

// Stop stops the http.
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < len(p.servers); i++ {
		if p.servers[i] != nil {
			p.servers[i].Stop()
		}
	}

	return nil
}

// ServeHTTP handles connection using set of middleware and pool PSR-7 server.
func (p *Plugin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// https://go-review.googlesource.com/c/go/+/30812/3/src/net/http/serve_test.go
	if helpers.HeaderContainsUpgrade(r) {
		// at this point the connection is hijacked, we can't write into the response writer
		_, err := w.Write(nil)
		if stderr.Is(err, http.ErrHijacked) {
			p.log.Error("the connection has been hijacked", zap.Error(err))
			return
		}

		http.Error(w, "server does not support upgrade header", http.StatusInternalServerError)
		return
	}

	// protect the case, when user sendEvent Reset, and we are replacing handler with pool
	p.mu.RLock()
	p.handler.ServeHTTP(w, r)
	p.mu.RUnlock()

	_ = r.Body.Close()
}

// Workers returns slice with the process states for the workers
func (p *Plugin) Workers() []*process.State {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := p.workers()
	if workers == nil {
		return nil
	}

	ps := make([]*process.State, 0, len(workers))
	for i := 0; i < len(workers); i++ {
		state, err := pstate.WorkerProcessState(workers[i])
		if err != nil {
			return nil
		}
		ps = append(ps, state)
	}

	return ps
}

// internal
func (p *Plugin) workers() []worker.BaseProcess {
	if p == nil || p.pool == nil {
		return nil
	}
	return p.pool.Workers()
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

	ctxTout, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()
	if p.pool == nil {
		p.log.Info("pool is nil, nothing to reset")
		return nil
	}

	err := p.pool.Reset(ctxTout)
	if err != nil {
		return errors.E(op, err)
	}

	p.log.Info("plugin was successfully reset")
	return nil
}

// Collects collecting http middlewares
func (p *Plugin) Collects() []interface{} {
	return []interface{}{
		p.AddMiddleware,
	}
}

// AddMiddleware is base requirement for the middleware (name and Middleware)
func (p *Plugin) AddMiddleware(name endure.Named, m middleware.Middleware) {
	// just to be safe
	p.mu.Lock()
	p.mdwr[name.Name()] = m
	p.mu.Unlock()
}

// Status return status of the particular plugin
func (p *Plugin) Status() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := p.workers()
	for i := 0; i < len(workers); i++ {
		if workers[i].State().IsActive() {
			return &status.Status{
				Code: http.StatusOK,
			}, nil
		}
	}
	// if there are no workers, threat this as error
	return &status.Status{
		Code: http.StatusServiceUnavailable,
	}, nil
}

// Ready return readiness status of the particular plugin
func (p *Plugin) Ready() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := p.workers()
	for i := 0; i < len(workers); i++ {
		// If state of the worker is ready (at least 1)
		// we assume, that plugin's worker pool is ready
		if workers[i].State().Value() == worker.StateReady {
			return &status.Status{
				Code: http.StatusOK,
			}, nil
		}
	}
	// if there are no workers, threat this as no content error
	return &status.Status{
		Code: http.StatusServiceUnavailable,
	}, nil
}

func (p *Plugin) serve(errCh chan error) {
	var err error
	p.pool, err = p.server.NewWorkerPool(context.Background(), p.cfg.Pool, map[string]string{RrMode: "http"}, p.log)
	if err != nil {
		errCh <- err
		return
	}

	// just to be safe :)
	if p.pool == nil {
		errCh <- errors.Str("pool should be initialized")
		return
	}

	p.handler, err = handler.NewHandler(
		p.cfg.HTTPConfig,
		p.cfg.Uploads,
		p.pool,
		p.log,
	)

	if err != nil {
		errCh <- err
		return
	}

	if p.cfg.EnableHTTP() {
		// handle redirects
		if p.cfg.SSLConfig != nil {
			p.servers = append(p.servers, httpServer.NewHTTPServer(p, p.cfg.HTTPConfig, p.stdLog, p.log, p.cfg.EnableH2C(), p.cfg.SSLConfig.Redirect, p.cfg.SSLConfig.Port))
		} else {
			p.servers = append(p.servers, httpServer.NewHTTPServer(p, p.cfg.HTTPConfig, p.stdLog, p.log, p.cfg.EnableH2C(), false, 0))
		}
	}

	if p.cfg.EnableTLS() {
		https, errHTTPS := tlsServer.NewHTTPSServer(p, p.cfg.SSLConfig, p.cfg.HTTP2Config, p.stdLog, p.log)
		if errHTTPS != nil {
			errCh <- errHTTPS
			return
		}

		p.servers = append(p.servers, https)
	}

	if p.cfg.EnableFCGI() {
		p.servers = append(p.servers, fcgi.NewFCGIServer(p, p.cfg.FCGIConfig, p.log, p.stdLog))
	}

	// if user uses the max_request_size, apply it to all servers
	if p.cfg.HTTPConfig != nil && p.cfg.HTTPConfig.MaxRequestSize != 0 {
		for i := 0; i < len(p.servers); i++ {
			serv := p.servers[i].GetServer()
			serv.Handler = bundledMw.MaxRequestSize(serv.Handler, p.cfg.HTTPConfig.MaxRequestSize, p.log)
		}
	}

	// start all servers
	for i := 0; i < len(p.servers); i++ {
		go func(idx int) {
			errSt := p.servers[idx].Start(p.mdwr, p.cfg.Middleware)
			if errSt != nil {
				errCh <- errSt
				return
			}
		}(i)
	}
}
