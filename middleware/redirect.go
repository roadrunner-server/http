package middleware

import (
	"fmt"
	"net/http"
	"net/url"
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
	// remove current forcePort first
	host = strings.Split(host, ":")[0]

	if forcePort || sslPort != 443 {
		host = fmt.Sprintf("%s:%v", host, sslPort)
	}

	return host
}
