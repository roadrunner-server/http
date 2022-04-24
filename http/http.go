package http

import (
	"context"
	stderr "errors"
	"log"
	"net/http"

	"github.com/roadrunner-server/api/v2/plugins/middleware"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v2/helpers"
	bundledmwr "github.com/roadrunner-server/http/v2/middleware"
	"github.com/roadrunner-server/sdk/v2/utils"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Server struct {
	cfg          *Config
	log          *zap.Logger
	http         *http.Server
	redirect     bool
	redirectPort int
}

func NewHTTPServer(handler http.Handler, cfg *Config, errLog *log.Logger, log *zap.Logger, enableH2C bool, redirect bool, redirectPort int) *Server {
	if enableH2C {
		return &Server{
			log:          log,
			redirect:     redirect,
			redirectPort: redirectPort,
			cfg:          cfg,
			http: &http.Server{
				Handler: h2c.NewHandler(handler, &http2.Server{
					MaxHandlers:                  0,
					MaxConcurrentStreams:         0,
					MaxReadFrameSize:             0,
					PermitProhibitedCipherSuites: false,
					IdleTimeout:                  0,
					MaxUploadBufferPerConnection: 0,
					MaxUploadBufferPerStream:     0,
					NewWriteScheduler:            nil,
					CountError:                   nil,
				}),
				ErrorLog: errLog,
			},
		}
	}
	return &Server{
		log:          log,
		redirect:     redirect,
		redirectPort: redirectPort,
		cfg:          cfg,
		http: &http.Server{
			Handler:  handler,
			ErrorLog: errLog,
		},
	}
}

// Start is a blocking function
func (s *Server) Start(mdwr map[string]middleware.Middleware, order []string) error {
	const op = errors.Op("serveHTTP")

	if len(mdwr) > 0 {
		helpers.ApplyMiddleware(s.http, mdwr, order, s.log)
	}

	// apply redirect middleware first (if redirect specified)
	if s.redirect {
		s.http.Handler = bundledmwr.Redirect(s.http.Handler, s.redirectPort)
	}

	l, err := utils.CreateListener(s.cfg.Address)
	if err != nil {
		return errors.E(op, err)
	}

	s.log.Debug("http server was started", zap.String("address", s.cfg.Address))
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
