package http3

import (
	"crypto/tls"
	"net/http"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"

	"github.com/roadrunner-server/http/v4/common"
	"github.com/roadrunner-server/http/v4/servers"
)

type Server struct {
	server *http3.Server
	log    *zap.Logger
	cfg    *Config
}

func NewHTTP3server(handler http.Handler, cfg *Config, log *zap.Logger) servers.InternalServer[any] {
	return &Server{
		log: log,
		cfg: cfg,
		server: &http3.Server{
			Addr:       cfg.Address,
			Handler:    handler,
			QuicConfig: &quic.Config{},
			TLSConfig:  &tls.Config{},
		},
	}
}

func (s *Server) Serve(mdwr map[string]common.Middleware, order []string) error {
	const op = errors.Op("serve_HTTP3")

	if len(mdwr) > 0 {
		applyMiddleware(s.server, mdwr, order, s.log)
	}

	s.log.Debug("http3 server was started", zap.String("address", s.server.Addr))
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
		s.log.Error("http3 server shutdown", zap.Error(err))
	}
}

func applyMiddleware(server *http3.Server, middleware map[string]common.Middleware, order []string, log *zap.Logger) {
	for i := 0; i < len(order); i++ {
		if mdwr, ok := middleware[order[i]]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", zap.String("requested", order[i]))
		}
	}
}
