package http3

import (
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"

	"github.com/roadrunner-server/http/v4/common"
)

type Server struct {
	server *http3.Server
	log    *zap.Logger
}

func NewHTTP3server(address string, log *zap.Logger) *Server {
	return &Server{
		log: log,
		server: &http3.Server{
			Addr:       address,
			QuicConfig: &quic.Config{},
		},
	}
}

func (s *Server) Serve(mdwr map[string]common.Middleware, order []string) error {
	const op = errors.Op("serve_HTTP3")

	if len(mdwr) > 0 {
		applyMiddleware(s.server, mdwr, order, s.log)
	}

	s.log.Debug("http3 server was started", zap.String("address", s.server.Addr))
	err := s.server.ListenAndServe()
	if err != nil {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) Server() *http3.Server {
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
