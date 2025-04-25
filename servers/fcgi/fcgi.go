package fcgi

import (
	stderr "errors"
	"log"
	"net/http"
	"net/http/fcgi"
	"time"

	"github.com/roadrunner-server/http/v5/common"
	"github.com/roadrunner-server/http/v5/servers"
	"github.com/roadrunner-server/tcplisten"

	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

type Server struct {
	cfg  *FCGI
	log  *zap.Logger
	fcgi *http.Server
}

func NewFCGIServer(handler http.Handler, cfg *FCGI, log *zap.Logger, errLog *log.Logger) servers.InternalServer[any] {
	return &Server{
		cfg: cfg,
		log: log,
		fcgi: &http.Server{
			ReadHeaderTimeout: time.Minute * 5,
			Handler:           handler,
			ErrorLog:          errLog,
		},
	}
}

func (s *Server) Serve(mdwr map[string]common.Middleware, order []string) error {
	const op = errors.Op("serve_fcgi")

	if len(mdwr) > 0 {
		applyMiddleware(s.fcgi, mdwr, order, s.log)
	}

	l, err := tcplisten.CreateListener(s.cfg.Address)
	if err != nil {
		return errors.E(op, err)
	}

	err = fcgi.Serve(l, s.fcgi.Handler)
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) Server() any {
	return s.fcgi
}

func (s *Server) Stop() {
	err := s.fcgi.Close()
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		s.log.Error("fcgi shutdown", zap.Error(err))
	}
}

func applyMiddleware(server *http.Server, middleware map[string]common.Middleware, order []string, log *zap.Logger) {
	for i := range order {
		if mdwr, ok := middleware[order[i]]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", zap.String("requested", order[i]))
		}
	}
}
