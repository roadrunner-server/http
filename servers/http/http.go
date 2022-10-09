package http

import (
	"context"
	stderr "errors"
	"log"
	"net/http"
	"time"

	"github.com/roadrunner-server/http/v3/common"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v3/config"
	"github.com/roadrunner-server/http/v3/middleware"
	"github.com/roadrunner-server/sdk/v3/utils"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Server struct {
	log          *zap.Logger
	http         *http.Server
	address      string
	redirect     bool
	redirectPort int
}

func NewHTTPServer(handler http.Handler, cfg *config.Config, errLog *log.Logger, log *zap.Logger) *Server {
	var redirect bool
	var redirectPort int

	if cfg.SSLConfig != nil {
		redirect = cfg.SSLConfig.Redirect
		redirectPort = cfg.SSLConfig.Port
	}

	if cfg.HTTP2Config != nil && cfg.HTTP2Config.H2C {
		return &Server{
			log:          log,
			redirect:     redirect,
			redirectPort: redirectPort,
			address:      cfg.Address,
			http: &http.Server{
				Handler: h2c.NewHandler(handler, &http2.Server{
					MaxConcurrentStreams:         cfg.HTTP2Config.MaxConcurrentStreams,
					PermitProhibitedCipherSuites: false,
				}),
				ReadTimeout:       time.Minute,
				ReadHeaderTimeout: time.Minute,
				WriteTimeout:      time.Minute,
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
			ReadHeaderTimeout: time.Minute * 5,
			Handler:           handler,
			ErrorLog:          errLog,
		},
	}
}

// Start is a blocking function
func (s *Server) Start(mdwr map[string]common.Middleware, order []string) error {
	const op = errors.Op("serveHTTP")

	if len(mdwr) > 0 {
		applyMiddleware(s.http, mdwr, order, s.log)
	}

	// apply redirect middleware first (if redirect specified)
	if s.redirect {
		s.http.Handler = middleware.Redirect(s.http.Handler, s.redirectPort)
	}

	l, err := utils.CreateListener(s.address)
	if err != nil {
		return errors.E(op, err)
	}

	s.log.Debug("http server was started", zap.String("address", s.address))
	err = s.http.Serve(l)
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) GetServer() *http.Server {
	return s.http
}

func (s *Server) Stop() {
	err := s.http.Shutdown(context.Background())
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		s.log.Error("http shutdown", zap.Error(err))
	}
}

func applyMiddleware(server *http.Server, middleware map[string]common.Middleware, order []string, log *zap.Logger) {
	for i := 0; i < len(order); i++ {
		if mdwr, ok := middleware[order[i]]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", zap.String("requested", order[i]))
		}
	}
}
