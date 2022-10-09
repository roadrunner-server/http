package fcgi

import (
	"context"
	stderr "errors"
	"log"
	"net/http"
	"net/http/fcgi"
	"time"

	"github.com/roadrunner-server/http/v2/common"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/sdk/v3/utils"
	"go.uber.org/zap"
)

type Server struct {
	cfg  *FCGI
	log  *zap.Logger
	fcgi *http.Server
}

func NewFCGIServer(handler http.Handler, cfg *FCGI, log *zap.Logger, errLog *log.Logger) *Server {
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

func (s *Server) Start(mdwr map[string]common.Middleware, order []string) error {
	const op = errors.Op("serve_fcgi")

	if len(mdwr) > 0 {
		applyMiddleware(s.fcgi, mdwr, order, s.log)
	}

	l, err := utils.CreateListener(s.cfg.Address)
	if err != nil {
		return errors.E(op, err)
	}

	err = fcgi.Serve(l, s.fcgi.Handler)
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) GetServer() *http.Server {
	return s.fcgi
}

func (s *Server) Stop() {
	err := s.fcgi.Shutdown(context.Background())
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		s.log.Error("fcgi shutdown", zap.Error(err))
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
