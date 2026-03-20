package fcgi

import (
	stderr "errors"
	"log"
	"log/slog"
	"net/http"
	"net/http/fcgi"
	"time"

	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/servers"
	"github.com/roadrunner-server/tcplisten"

	"github.com/roadrunner-server/errors"
)

type Server struct {
	cfg  *FCGI
	log  *slog.Logger
	fcgi *http.Server
}

func NewFCGIServer(handler http.Handler, cfg *FCGI, log *slog.Logger, errLog *log.Logger) servers.InternalServer[any] {
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

func (s *Server) Serve(mdwr map[string]api.Middleware, order []string) error {
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
		s.log.Error("fcgi shutdown", "error", err)
	}
}

func applyMiddleware(server *http.Server, middleware map[string]api.Middleware, order []string, log *slog.Logger) {
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		if mdwr, ok := middleware[name]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", "requested", name)
		}
	}
}
