package http

import (
	bundledMw "github.com/roadrunner-server/http/v4/middleware"
	"github.com/roadrunner-server/http/v4/servers/fcgi"
	httpServer "github.com/roadrunner-server/http/v4/servers/http"
	httpsServer "github.com/roadrunner-server/http/v4/servers/https"
)

// ------- PRIVATE ---------

func (p *Plugin) initServers() error {
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

func (p *Plugin) applyBundledMiddleware() {
	// apply max_req_size and logger middleware
	for i := 0; i < len(p.servers); i++ {
		serv := p.servers[i].Server()
		serv.Handler = bundledMw.MaxRequestSize(serv.Handler, p.cfg.MaxRequestSize*MB)
		serv.Handler = bundledMw.NewLogMiddleware(serv.Handler, p.cfg.AccessLogs, p.log)
	}
}
