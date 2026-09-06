package http

import (
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"

	"github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/http/v6/acme"
	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/http/v6/config"
	bundledMw "github.com/roadrunner-server/http/v6/middleware"
	"github.com/roadrunner-server/http/v6/servers/fcgi"
	httpServer "github.com/roadrunner-server/http/v6/servers/http11"
	http3Server "github.com/roadrunner-server/http/v6/servers/http3"
	httpsServer "github.com/roadrunner-server/http/v6/servers/https"
	"github.com/roadrunner-server/tcplisten"
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
	for _, s := range p.servers {
		switch srv := s.Server().(type) {
		case *http.Server:
			srv.Handler = bundledMw.MaxRequestSize(srv.Handler, p.cfg.MaxRequestSize*MB)
			srv.Handler = bundledMw.NewLogMiddleware(srv.Handler, p.cfg.AccessLogs, p.log)
		case *http3.Server:
			srv.Handler = bundledMw.MaxRequestSize(srv.Handler, p.cfg.MaxRequestSize*MB)
			srv.Handler = bundledMw.NewLogMiddleware(srv.Handler, p.cfg.AccessLogs, p.log)
		default:
			p.log.Error("unknown server type", "server", s.Server())
		}
	}
}

func (p *Plugin) unmarshal(cfg api.Configurer) error {
	for _, key := range []string{"http.unix_socket", "http.fcgi.unix_socket"} {
		if err := validateUnixSocketIDs(cfg, key); err != nil {
			return err
		}
	}

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

	// Viper can omit empty maps when it decodes the parent section.
	if cfg.Has("http.unix_socket") {
		p.cfg.UnixSocket = &tcplisten.UnixSocketOptions{}
		if err = cfg.UnmarshalKey("http.unix_socket", p.cfg.UnixSocket); err != nil {
			return err
		}
	}
	if cfg.Has("http.fcgi.unix_socket") {
		if p.cfg.FCGIConfig == nil {
			p.cfg.FCGIConfig = &fcgi.FCGI{}
		}
		p.cfg.FCGIConfig.UnixSocket = &tcplisten.UnixSocketOptions{}
		if err = cfg.UnmarshalKey("http.fcgi.unix_socket", p.cfg.FCGIConfig.UnixSocket); err != nil {
			return err
		}
	}

	return nil
}

// Check raw IDs before weak decoding can convert booleans or truncate fractions.
func validateUnixSocketIDs(cfg api.Configurer, key string) error {
	if !cfg.Has(key) {
		return nil
	}
	var options map[string]any
	if err := cfg.UnmarshalKey(key, &options); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	for _, field := range []string{"uid", "gid"} {
		if options[field] == nil {
			continue
		}
		value := reflect.ValueOf(options[field])
		var valid bool
		switch value.Kind() { //nolint:exhaustive // Other kinds fail validation.
		case reflect.String:
			id, err := strconv.ParseInt(value.String(), 0, strconv.IntSize)
			valid = err == nil && id >= 0 && id < 4294967295
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			id := value.Int()
			valid = id >= 0 && id < 4294967295
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			valid = value.Uint() < 4294967295
		case reflect.Float32, reflect.Float64:
			id := value.Float()
			valid = id >= 0 && id < 4294967295 && math.Trunc(id) == id
		}
		if !valid {
			return fmt.Errorf("%s.%s: must be an integer between 0 and 4294967294", key, field)
		}
	}
	return nil
}
