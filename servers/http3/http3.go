package http3

import (
	"log/slog"
	"net/http"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v6/acme"
	"github.com/roadrunner-server/http/v6/tlsconf"

	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/servers"
)

const ACMETLS1Protocol string = "acme-tls/1"

type Server struct {
	server *http3.Server
	log    *slog.Logger
	cfg    *Config
}

func NewHTTP3server(handler http.Handler, acmeCfg *acme.Config, cfg *Config, log *slog.Logger) (servers.InternalServer[any], error) {
	http3Srv := &Server{
		log: log,
		cfg: cfg,
		server: &http3.Server{
			Addr:       cfg.Address,
			Handler:    handler,
			QUICConfig: &quic.Config{},
			TLSConfig:  tlsconf.DefaultTLSConfig(),
		},
	}

	if acmeCfg != nil {
		tlsCfg, err := acme.IssueCertificates(
			acmeCfg.CacheDir,
			acmeCfg.Email,
			acmeCfg.ChallengeType,
			acmeCfg.Domains,
			acmeCfg.UseProductionEndpoint,
			acmeCfg.AltHTTPPort,
			acmeCfg.AltTLSALPNPort,
			log,
		)

		if err != nil {
			return nil, err
		}

		http3Srv.server.TLSConfig.GetCertificate = tlsCfg.GetCertificate
		http3Srv.server.TLSConfig.NextProtos = append(http3Srv.server.TLSConfig.NextProtos, ACMETLS1Protocol)
	}

	return http3Srv, nil
}

func (s *Server) Serve(mdwr map[string]api.Middleware, order []string) error {
	const op = errors.Op("serve_HTTP3")

	if len(mdwr) > 0 {
		applyMiddleware(s.server, mdwr, order, s.log)
	}

	s.log.Debug("http3 server was started", "address", s.server.Addr)
	err := s.server.ListenAndServeTLS(s.cfg.Cert, s.cfg.Key)
	if err != nil {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) Server() any {
	return s.server
}

func (s *Server) Stop() {
	err := s.server.Close()
	if err != nil {
		s.log.Error("http3 server shutdown", "error", err)
	}
}

func applyMiddleware(server *http3.Server, middleware map[string]api.Middleware, order []string, log *slog.Logger) {
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		if mdwr, ok := middleware[name]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", "requested", name)
		}
	}
}
