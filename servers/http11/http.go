package http

import (
	stderr "errors"
	"log"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/roadrunner-server/tcplisten"

	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/servers"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/middleware"
)

type Server struct {
	log          *slog.Logger
	http         *http.Server
	address      string
	redirect     bool
	redirectPort int
}

func NewHTTPServer(handler http.Handler, cfg *config.Config, errLog *log.Logger, log *slog.Logger) servers.InternalServer[any] {
	var redirect bool
	var redirectPort int

	if cfg.SSLConfig != nil {
		redirect = cfg.SSLConfig.Redirect
		redirectPort = cfg.SSLConfig.Port
	}

	if cfg.HTTP2Config != nil && cfg.HTTP2Config.H2C {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetUnencryptedHTTP2(true)
		return &Server{
			log:          log,
			redirect:     redirect,
			redirectPort: redirectPort,
			address:      cfg.Address,
			http: &http.Server{
				Handler:           handler,
				Protocols:         protocols,
				HTTP2:             &http.HTTP2Config{MaxConcurrentStreams: int(cfg.HTTP2Config.MaxConcurrentStreams)},
				ReadTimeout:       time.Minute * 5,
				WriteTimeout:      time.Minute * 5,
				IdleTimeout:       time.Hour,
				ReadHeaderTimeout: time.Minute * 5,
				ErrorLog:          errLog,
			},
		}
	}
	return &Server{
		log:          log,
		redirect:     redirect,
		redirectPort: redirectPort,
		address:      cfg.Address,
		http: &http.Server{
			ReadTimeout:       time.Minute * 5,
			WriteTimeout:      time.Minute * 5,
			IdleTimeout:       time.Hour,
			ReadHeaderTimeout: time.Minute * 5,
			Handler:           handler,
			ErrorLog:          errLog,
		},
	}
}

// Serve is a blocking function
func (s *Server) Serve(mdwr map[string]api.Middleware, order []string) error {
	const op = errors.Op("serveHTTP")

	if len(mdwr) > 0 {
		applyMiddleware(s.http, mdwr, order, s.log)
	}

	// apply redirect middleware first (if redirect specified)
	if s.redirect {
		s.http.Handler = middleware.Redirect(s.http.Handler, s.redirectPort)
	}

	l, err := tcplisten.CreateListener(s.address)
	if err != nil {
		return errors.E(op, err)
	}

	s.log.Debug("http server was started", "address", s.address)
	err = s.http.Serve(l)
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) Server() any {
	return s.http
}

func (s *Server) Stop() {
	err := s.http.Close()
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		s.log.Error("http shutdown", "error", err)
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
