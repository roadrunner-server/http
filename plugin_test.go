package http

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/servers/fcgi"
	"github.com/roadrunner-server/pool/v2/payload"
	"github.com/roadrunner-server/pool/v2/pool"
	staticPool "github.com/roadrunner-server/pool/v2/pool/static_pool"
	"github.com/roadrunner-server/pool/v2/worker"
)

// stubConfigurer serves a single prepared http config and can be told to fail
// on exactly one configuration section.
type stubConfigurer struct {
	has          bool
	experimental bool
	errSection   string
	httpCfg      *config.Config
}

func (c *stubConfigurer) Has(string) bool    { return c.has }
func (c *stubConfigurer) Experimental() bool { return c.experimental }

func (c *stubConfigurer) UnmarshalKey(name string, out any) error {
	if name == c.errSection {
		return errors.Errorf("cannot unmarshal %s", name)
	}

	if name == PluginName {
		cfg, ok := out.(**config.Config)
		if !ok {
			return errors.Errorf("unexpected target type %T", out)
		}
		*cfg = c.httpCfg
	}

	return nil
}

// stubLoggerProvider satisfies api.Logger without emitting anything.
type stubLoggerProvider struct{}

func (stubLoggerProvider) NamedLogger(string) *slog.Logger { return slog.New(slog.DiscardHandler) }

// stubWorkerServer satisfies api.Server; only UID/GID are read during Init.
type stubWorkerServer struct {
	uid int
	gid int
}

func (s *stubWorkerServer) UID() int { return s.uid }
func (s *stubWorkerServer) GID() int { return s.gid }
func (s *stubWorkerServer) NewPool(context.Context, *pool.Config, map[string]string, *slog.Logger) (*staticPool.Pool, error) {
	return nil, nil
}

// stubResetPool satisfies api.Pool; only Reset is exercised.
type stubResetPool struct{ resetErr error }

func (p *stubResetPool) Workers() []*worker.Process           { return nil }
func (p *stubResetPool) RemoveWorker(_ context.Context) error { return nil }
func (p *stubResetPool) AddWorker() error                     { return nil }
func (p *stubResetPool) Exec(_ context.Context, _ *payload.Payload, _ chan struct{}) (chan *staticPool.PExec, error) {
	return nil, nil
}
func (p *stubResetPool) Reset(_ context.Context) error { return p.resetErr }
func (p *stubResetPool) Destroy(_ context.Context)     {}

func servableConfig() *config.Config {
	return &config.Config{Address: "127.0.0.1:8080"}
}

func TestInit_SectionDisabled(t *testing.T) {
	p := &Plugin{}

	err := p.Init(&stubConfigurer{has: false}, stubLoggerProvider{}, &stubWorkerServer{})
	if !errors.Is(errors.Disabled, err) {
		t.Fatalf("error = %v, want errors.Disabled", err)
	}
}

func TestInit_UnmarshalErrorPerSection(t *testing.T) {
	sections := []string{PluginName, sectionHTTPS, sectionHTTP2, sectionUploads, sectionFCGI}

	for _, section := range sections {
		t.Run(section, func(t *testing.T) {
			p := &Plugin{}
			cfg := &stubConfigurer{has: true, errSection: section, httpCfg: servableConfig()}

			err := p.Init(cfg, stubLoggerProvider{}, &stubWorkerServer{})
			if err == nil {
				t.Fatal("expected an unmarshal error")
			}
			if !strings.Contains(err.Error(), "cannot unmarshal "+section) {
				t.Errorf("error = %v, want the %s section to be named", err, section)
			}
		})
	}
}

func TestInit_NoServerConfigured_FailsValidation(t *testing.T) {
	p := &Plugin{}
	cfg := &stubConfigurer{has: true, httpCfg: &config.Config{}}

	err := p.Init(cfg, stubLoggerProvider{}, &stubWorkerServer{})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "no method has been specified") {
		t.Errorf("error = %v, want a no-method-specified error", err)
	}
}

func TestInit_PopulatesPluginState(t *testing.T) {
	p := &Plugin{}
	cfg := &stubConfigurer{has: true, experimental: true, httpCfg: servableConfig()}

	if err := p.Init(cfg, stubLoggerProvider{}, &stubWorkerServer{uid: 501, gid: 20}); err != nil {
		t.Fatal(err)
	}

	if !p.experimentalFeatures {
		t.Error("experimentalFeatures is false")
	}
	if p.cfg.UID != 501 || p.cfg.GID != 20 {
		t.Errorf("uid/gid = %d/%d, want 501/20", p.cfg.UID, p.cfg.GID)
	}
	if p.statsExporter == nil {
		t.Error("statsExporter is nil")
	}
	if p.prop == nil {
		t.Error("propagator is nil")
	}
	if p.mdwr == nil {
		t.Error("middleware map is nil")
	}
	if p.stdLog == nil {
		t.Error("stdLog is nil")
	}
	if len(p.servers) != 0 {
		t.Errorf("servers = %v, want an empty slice before Serve", p.servers)
	}
}

func TestInit_FCGIOnlyConfigIsAccepted(t *testing.T) {
	p := &Plugin{}
	cfg := &stubConfigurer{has: true, httpCfg: &config.Config{FCGIConfig: &fcgi.FCGI{Address: "tcp://127.0.0.1:6920"}}}

	if err := p.Init(cfg, stubLoggerProvider{}, &stubWorkerServer{}); err != nil {
		t.Fatal(err)
	}
}

func TestReset_NilPool_IsNoOp(t *testing.T) {
	p := &Plugin{log: slog.New(slog.DiscardHandler)}

	if err := p.Reset(); err != nil {
		t.Fatalf("Reset() = %v, want nil", err)
	}
}

func TestReset_PoolError_Wrapped(t *testing.T) {
	p := &Plugin{
		log:  slog.New(slog.DiscardHandler),
		pool: &stubResetPool{resetErr: errors.Str("pool is dead")},
	}

	err := p.Reset()
	if err == nil {
		t.Fatal("expected the pool error to be returned")
	}
	if !strings.Contains(err.Error(), "pool is dead") {
		t.Errorf("error = %v, want the pool error", err)
	}
}

func TestReset_HealthyPool(t *testing.T) {
	p := &Plugin{log: slog.New(slog.DiscardHandler), pool: &stubResetPool{}}

	if err := p.Reset(); err != nil {
		t.Fatalf("Reset() = %v, want nil", err)
	}
}

func TestWorkers_NilPool_ReturnsNil(t *testing.T) {
	p := &Plugin{}

	if got := p.Workers(); got != nil {
		t.Errorf("Workers() = %v, want nil", got)
	}
}

func TestWorkers_EmptyPool_ReturnsEmptySlice(t *testing.T) {
	p := &Plugin{pool: &stubResetPool{}}

	if got := p.Workers(); len(got) != 0 {
		t.Errorf("Workers() = %v, want no states", got)
	}
}

func TestName_IsPluginName(t *testing.T) {
	if got := (&Plugin{}).Name(); got != PluginName {
		t.Errorf("Name() = %q, want %q", got, PluginName)
	}
}

func TestStop_WithoutServersOrPool(t *testing.T) {
	p := &Plugin{}

	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() = %v, want nil", err)
	}
}

func TestCollects_RegistersMiddlewareCollector(t *testing.T) {
	p := &Plugin{mdwr: make(map[string]api.Middleware)}

	if got := p.Collects(); len(got) != 1 {
		t.Errorf("Collects() returned %d deps, want 1", len(got))
	}
}
