package https

import (
	"crypto/tls"
	"crypto/x509"
	stderr "errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/roadrunner-server/tcplisten"

	"github.com/roadrunner-server/http/v5/acme"
	"github.com/roadrunner-server/http/v5/api"
	"github.com/roadrunner-server/http/v5/servers"
	"github.com/roadrunner-server/http/v5/tlsconf"

	"github.com/mholt/acmez"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

type Server struct {
	cfg   *SSL
	log   *zap.Logger
	https *http.Server
}

func NewHTTPSServer(handler http.Handler, cfg *SSL, cfgHTTP2 *HTTP2, errLog *log.Logger, logger *zap.Logger) (servers.InternalServer[any], error) {
	httpsServer := initTLS(handler, errLog, cfg.Address, cfg.Port)

	if cfg.RootCA != "" {
		pool, err := createCertPool(cfg.RootCA)
		if err != nil {
			return nil, err
		}

		if pool != nil {
			httpsServer.TLSConfig.ClientCAs = pool
			// auth type used only for the CA
			switch cfg.AuthType {
			case NoClientCert:
				httpsServer.TLSConfig.ClientAuth = tls.NoClientCert
			case RequestClientCert:
				httpsServer.TLSConfig.ClientAuth = tls.RequestClientCert
			case RequireAnyClientCert:
				httpsServer.TLSConfig.ClientAuth = tls.RequireAnyClientCert
			case VerifyClientCertIfGiven:
				httpsServer.TLSConfig.ClientAuth = tls.VerifyClientCertIfGiven
			case RequireAndVerifyClientCert:
				httpsServer.TLSConfig.ClientAuth = tls.RequireAndVerifyClientCert
			default:
				httpsServer.TLSConfig.ClientAuth = tls.NoClientCert
			}
		}
	}

	if cfg.EnableACME() {
		tlsCfg, err := acme.IssueCertificates(
			cfg.Acme.CacheDir,
			cfg.Acme.Email,
			cfg.Acme.ChallengeType,
			cfg.Acme.Domains,
			cfg.Acme.UseProductionEndpoint,
			cfg.Acme.AltHTTPPort,
			cfg.Acme.AltTLSALPNPort,
			logger,
		)

		if err != nil {
			return nil, err
		}

		httpsServer.TLSConfig.GetCertificate = tlsCfg.GetCertificate
		httpsServer.TLSConfig.NextProtos = append(httpsServer.TLSConfig.NextProtos, acmez.ACMETLS1Protocol)
	}

	if cfgHTTP2 != nil && cfgHTTP2.EnableHTTP2() {
		err := initHTTP2(httpsServer, cfgHTTP2.MaxConcurrentStreams)
		if err != nil {
			return nil, err
		}
	}

	return &Server{
		cfg:   cfg,
		log:   logger,
		https: httpsServer,
	}, nil
}

func (s *Server) Serve(mdwr map[string]api.Middleware, order []string) error {
	const op = errors.Op("serveHTTPS")
	if len(mdwr) > 0 {
		applyMiddleware(s.https, mdwr, order, s.log)
	}

	l, err := tcplisten.CreateListener(s.cfg.Address)
	if err != nil {
		return errors.E(op, err)
	}

	/*
		ACME powered server
	*/
	if s.cfg.EnableACME() {
		s.log.Debug("https(acme) server was started", zap.String("address", s.cfg.Address))
		err = s.https.ServeTLS(
			l,
			"",
			"",
		)
		if err != nil && !stderr.Is(err, http.ErrServerClosed) {
			return errors.E(op, err)
		}

		return nil
	}

	s.log.Debug("https server was started", zap.String("address", s.cfg.Address))
	err = s.https.ServeTLS(
		l,
		s.cfg.Cert,
		s.cfg.Key,
	)

	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.E(op, err)
	}

	return nil
}

func (s *Server) Server() any {
	return s.https
}

func (s *Server) Stop() {
	err := s.https.Close()
	if err != nil && !stderr.Is(err, http.ErrServerClosed) {
		s.log.Error("https shutdown", zap.Error(err))
	}
}

// append RootCA to the https server TLS config
func createCertPool(rootCa string) (*x509.CertPool, error) {
	const op = errors.Op("http_plugin_append_root_ca")
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, nil
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	CA, err := os.ReadFile(rootCa)
	if err != nil {
		return nil, err
	}

	// should append our CA cert
	ok := rootCAs.AppendCertsFromPEM(CA)
	if !ok {
		return nil, errors.E(op, errors.Str("could not append Certs from PEM"))
	}

	return rootCAs, nil
}

// Init https server
func initTLS(handler http.Handler, errLog *log.Logger, addr string, port int) *http.Server {
	sslServer := &http.Server{
		Addr:              tlsAddr(addr, true, port),
		Handler:           handler,
		ErrorLog:          errLog,
		ReadHeaderTimeout: time.Minute * 5,
		TLSConfig:         tlsconf.DefaultTLSConfig(),
	}

	return sslServer
}

// tlsAddr replaces listen or host port with port configured by SSLConfig config.
func tlsAddr(host string, forcePort bool, sslPort int) string {
	// remove current forcePort first
	host = strings.Split(host, ":")[0]

	if forcePort || sslPort != 443 {
		host = fmt.Sprintf("%s:%v", host, sslPort)
	}

	return host
}

func applyMiddleware(server *http.Server, middleware map[string]api.Middleware, order []string, log *zap.Logger) {
	for i := range order {
		if mdwr, ok := middleware[order[i]]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", zap.String("requested", order[i]))
		}
	}
}
