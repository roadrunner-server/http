package http

import (
	"log/slog"
	"net/http"
	"testing"

	quicHTTP3 "github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/http/v6/acme"
	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/servers"
	"github.com/roadrunner-server/http/v6/servers/fcgi"
	"github.com/roadrunner-server/http/v6/servers/http3"
	"github.com/roadrunner-server/http/v6/servers/https"
)

// stubInternalServer hands out whatever Server() should report so the type
// switch in applyBundledMiddleware can be driven for every arm.
type stubInternalServer struct{ inner any }

func (s *stubInternalServer) Serve(map[string]api.Middleware, []string) error { return nil }
func (s *stubInternalServer) Server() any                                     { return s.inner }
func (s *stubInternalServer) Stop()                                           {}

func TestNilOr(t *testing.T) {
	acmeCfg := &acme.Config{Email: "a@b.c"}

	tests := []struct {
		name string
		cfg  *config.Config
		want *acme.Config
	}{
		{"no ssl section", &config.Config{}, nil},
		{"ssl section without acme", &config.Config{SSLConfig: &https.SSL{}}, nil},
		{"ssl section with acme", &config.Config{SSLConfig: &https.SSL{Acme: acmeCfg}}, acmeCfg},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nilOr(tt.cfg); got != tt.want {
				t.Errorf("nilOr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitServers_OneServerPerEnabledSection(t *testing.T) {
	p := &Plugin{
		log:                  slog.New(slog.DiscardHandler),
		experimentalFeatures: true,
		servers:              make([]servers.InternalServer[any], 0, 4),
		cfg: &config.Config{
			Address:     "127.0.0.1:8080",
			SSLConfig:   &https.SSL{Address: "127.0.0.1:8443", Key: "server.key", Cert: "server.crt"},
			FCGIConfig:  &fcgi.FCGI{Address: "tcp://127.0.0.1:6920"},
			HTTP3Config: &http3.Config{Address: "127.0.0.1:8444"},
		},
	}

	if err := p.initServers(); err != nil {
		t.Fatal(err)
	}
	if len(p.servers) != 4 {
		t.Fatalf("servers = %d, want 4 (http3, http, https, fcgi)", len(p.servers))
	}
}

func TestInitServers_HTTP3NeedsExperimentalMode(t *testing.T) {
	p := &Plugin{
		log:     slog.New(slog.DiscardHandler),
		servers: make([]servers.InternalServer[any], 0, 4),
		cfg: &config.Config{
			Address:     "127.0.0.1:8080",
			HTTP3Config: &http3.Config{Address: "127.0.0.1:8444"},
		},
	}

	if err := p.initServers(); err != nil {
		t.Fatal(err)
	}
	if len(p.servers) != 1 {
		t.Fatalf("servers = %d, want only the plain http server", len(p.servers))
	}
}

// markerHandler is comparable, so the wrapping can be detected by identity.
type markerHandler struct{}

func (markerHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestApplyBundledMiddleware_WrapsKnownServerTypes(t *testing.T) {
	base := markerHandler{}
	httpSrv := &http.Server{Handler: base} //nolint:gosec
	http3Srv := &quicHTTP3.Server{Handler: base}

	p := &Plugin{
		log: slog.New(slog.DiscardHandler),
		cfg: &config.Config{MaxRequestSize: 1, AccessLogs: true},
		servers: []servers.InternalServer[any]{
			&stubInternalServer{inner: httpSrv},
			&stubInternalServer{inner: http3Srv},
			&stubInternalServer{inner: "not a server"},
		},
	}

	p.applyBundledMiddleware()

	if httpSrv.Handler == base {
		t.Error("the http server handler was not wrapped")
	}
	if http3Srv.Handler == base {
		t.Error("the http3 server handler was not wrapped")
	}
}
