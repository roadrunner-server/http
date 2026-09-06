package fcgi

import (
	stderr "errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/fcgi"
	"slices"
	"sync"
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

	mu       sync.Mutex
	listener net.Listener
	stopped  bool
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

	// Hold the lock through bind so Stop cannot miss a new listener.
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	l, err := tcplisten.CreateListenerWithOptions(s.cfg.Address, s.cfg.UnixSocket)
	if err != nil {
		s.mu.Unlock()
		return errors.E(op, err)
	}
	s.listener = l
	s.mu.Unlock()
	defer s.Stop()

	err = fcgi.Serve(l, s.fcgi.Handler)
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if err != nil && (!stopped || !stderr.Is(err, net.ErrClosed)) {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) Server() any {
	return s.fcgi
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	if s.listener != nil {
		if err := s.listener.Close(); err != nil && !stderr.Is(err, net.ErrClosed) {
			s.log.Error("fcgi shutdown", "error", err)
		}
	}
}

func applyMiddleware(server *http.Server, middleware map[string]api.Middleware, order []string, log *slog.Logger) {
	for _, name := range slices.Backward(order) {
		if mdwr, ok := middleware[name]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", "requested", name)
		}
	}
}
