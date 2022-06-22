package middleware

import (
	"net/http"
	"net/url"

	"github.com/roadrunner-server/http/v2/helpers"
)

const scheme string = "https"

func Redirect(_ http.Handler, port int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		target := &url.URL{
			Scheme: scheme,
			// host or host:port
			Host:     helpers.TLSAddr(r.Host, false, port),
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		}

		http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
	})
}
