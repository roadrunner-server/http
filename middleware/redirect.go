package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const scheme string = "https"

func Redirect(_ http.Handler, port int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		target := &url.URL{
			Scheme: scheme,
			// host or host:port
			Host:     TLSAddr(r.Host, false, port),
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		}

		http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
	})
}

// TLSAddr replaces listen or host port with port configured by SSLConfig config.
func TLSAddr(host string, forcePort bool, sslPort int) string {
	if u, err := url.Parse("//" + host); err == nil {
		host = u.Hostname()
	}

	if forcePort || sslPort != 443 {
		return net.JoinHostPort(host, strconv.Itoa(sslPort))
	}

	// url.URL.Host requires bracketed IPv6 literals even without a port.
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}

	return host
}
