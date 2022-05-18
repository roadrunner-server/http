package https

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	stderr "errors"
	"log"
	"net/http"
	"os"

	"github.com/mholt/acmez"
	"github.com/roadrunner-server/api/v2/plugins/middleware"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/http/v2/helpers"
	"github.com/roadrunner-server/sdk/v2/utils"
	"go.uber.org/zap"
	"golang.org/x/sys/cpu"
)

type Server struct {
	cfg   *SSL
	log   *zap.Logger
	https *http.Server
}

func NewHTTPSServer(handler http.Handler, cfg *SSL, cfgHTTP2 *HTTP2, errLog *log.Logger, logger *zap.Logger) (*Server, error) {
	httpsServer := initTLS(handler, errLog, cfg.Address, cfg.Port)

	if cfg.RootCA != "" {
		pool, err := createCertPool(cfg.RootCA)
		if err != nil {
			return nil, err
		}

		if pool != nil {
			httpsServer.TLSConfig.RootCAs = pool
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
		tlsCfg, err := IssueCertificates(
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

	if cfgHTTP2.EnableHTTP2() {
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

func (s *Server) Start(mdwr map[string]middleware.Middleware, order []string) error {
	const op = errors.Op("serveHTTPS")
	if len(mdwr) > 0 {
		helpers.ApplyMiddleware(s.https, mdwr, order, s.log)
	}

	l, err := utils.CreateListener(s.cfg.Address)
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

func (s *Server) GetServer() *http.Server {
	return s.https
}

func (s *Server) Stop() {
	err := s.https.Shutdown(context.Background())
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
	var topCipherSuites []uint16
	var defaultCipherSuitesTLS13 []uint16

	hasGCMAsmAMD64 := cpu.X86.HasAES && cpu.X86.HasPCLMULQDQ
	hasGCMAsmARM64 := cpu.ARM64.HasAES && cpu.ARM64.HasPMULL
	// Keep in sync with crypto/aes/cipher_s390x.go.
	hasGCMAsmS390X := cpu.S390X.HasAES && cpu.S390X.HasAESCBC && cpu.S390X.HasAESCTR && (cpu.S390X.HasGHASH || cpu.S390X.HasAESGCM)

	hasGCMAsm := hasGCMAsmAMD64 || hasGCMAsmARM64 || hasGCMAsmS390X

	if hasGCMAsm {
		// If AES-GCM hardware is provided then priorities AES-GCM
		// cipher suites.
		topCipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		}
		defaultCipherSuitesTLS13 = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
		}
	} else {
		// Without AES-GCM hardware, we put the ChaCha20-Poly1305
		// cipher suites first.
		topCipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		}
		defaultCipherSuitesTLS13 = []uint16{
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
		}
	}

	DefaultCipherSuites := make([]uint16, 0, 22)
	DefaultCipherSuites = append(DefaultCipherSuites, topCipherSuites...)
	DefaultCipherSuites = append(DefaultCipherSuites, defaultCipherSuitesTLS13...)

	sslServer := &http.Server{
		Addr:     helpers.TLSAddr(addr, true, port),
		Handler:  handler,
		ErrorLog: errLog,
		TLSConfig: &tls.Config{
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP256,
				tls.CurveP384,
				tls.CurveP521,
			},
			CipherSuites: DefaultCipherSuites,
			MinVersion:   tls.VersionTLS12,
		},
	}

	return sslServer
}
