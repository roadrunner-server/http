package middleware

import (
	"context"
	"net/http"
)

func MaxRequestSize(next http.Handler, maxReqSize uint64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// validating request size

		r2 := r.Clone(context.Background())
		r2.Body = http.MaxBytesReader(w, r2.Body, int64(maxReqSize))

		// use max_request_size limit in megabytes
		next.ServeHTTP(w, r2)
	})
}
