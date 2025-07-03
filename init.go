package http

import (
	"net/http"

	"github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/http/v5/acme"
	"github.com/roadrunner-server/http/v5/api"
	"github.com/roadrunner-server/http/v5/config"
	bundledMw "github.com/roadrunner-server/http/v5/middleware"
	"github.com/roadrunner-server/http/v5/servers/fcgi"
	httpServer "github.com/roadrunner-server/http/v5/servers/http11"
	http3Server "github.com/roadrunner-server/http/v5/servers/http3"
	httpsServer "github.com/roadrunner-server/http/v5/servers/https"
	"go.uber.org/zap"
)

// ------- PRIVATE ---------

func (p *Plugin) initServers() error {
	if p.cfg.EnableHTTP3() && p.experimentalFeatures {
		http3Srv, err := http3Server.NewHTTP3server(p, nilOr(p.cfg), p.cfg.HTTP3Config, p.log)
		if err != nil {
			return err
		}

		p.servers = append(p.servers, http3Srv)
	}

	if p.cfg.EnableHTTP() {
		p.servers = append(p.servers, httpServer.NewHTTPServer(p, p.cfg, p.stdLog, p.log))
	}

	if p.cfg.EnableTLS() {
		https, err := httpsServer.NewHTTPSServer(p, p.cfg.SSLConfig, p.cfg.HTTP2Config, p.stdLog, p.log)
		if err != nil {
			return err
		}

		p.servers = append(p.servers, https)
	}

	if p.cfg.EnableFCGI() {
		p.servers = append(p.servers, fcgi.NewFCGIServer(p, p.cfg.FCGIConfig, p.log, p.stdLog))
	}

	return nil
}

func nilOr(cfg *config.Config) *acme.Config {
	if cfg.SSLConfig == nil || cfg.SSLConfig.Acme == nil {
		return nil
	}

	return cfg.SSLConfig.Acme
}

func (p *Plugin) applyBundledMiddleware() {
	// apply max_req_size and logger middleware
	for i := range p.servers {
		switch srv := p.servers[i].Server().(type) {
		case *http.Server:
			srv.Handler = bundledMw.MaxRequestSize(srv.Handler, p.cfg.MaxRequestSize*MB)
			srv.Handler = bundledMw.NewLogMiddleware(srv.Handler, p.cfg.AccessLogs, p.log)
		case *http3.Server:
			srv.Handler = bundledMw.MaxRequestSize(srv.Handler, p.cfg.MaxRequestSize*MB)
			srv.Handler = bundledMw.NewLogMiddleware(srv.Handler, p.cfg.AccessLogs, p.log)
		default:
			p.log.DPanic("unknown server type", zap.Any("server", p.servers[i].Server()))
		}
	}
}

func (p *Plugin) unmarshal(cfg api.Configurer) error {
	// unmarshal general section
	err := cfg.UnmarshalKey(PluginName, &p.cfg)
	if err != nil {
		return err
	}

	// unmarshal HTTPS section
	err = cfg.UnmarshalKey(sectionHTTPS, &p.cfg.SSLConfig)
	if err != nil {
		return err
	}

	// unmarshal H2C section
	err = cfg.UnmarshalKey(sectionHTTP2, &p.cfg.HTTP2Config)
	if err != nil {
		return err
	}

	// unmarshal uploads section
	err = cfg.UnmarshalKey(sectionUploads, &p.cfg.Uploads)
	if err != nil {
		return err
	}

	// unmarshal fcgi section
	err = cfg.UnmarshalKey(sectionFCGI, &p.cfg.FCGIConfig)
	if err != nil {
		return err
	}

	return nil
}
