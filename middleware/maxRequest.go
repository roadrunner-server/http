package middleware

import (
	"net/http"
)

func MaxRequestSize(next http.Handler, maxReqSize uint64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// use max_request_size limit in megabytes
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxReqSize)) //nolint:gosec
		next.ServeHTTP(w, r)
	})
}
