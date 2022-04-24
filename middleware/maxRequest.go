package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

const (
	contentLen string = "Content-Length"
)

func MaxRequestSize(next http.Handler, maxReqSize uint64, log *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// validating request size
		start := time.Now()
		const op = errors.Op("http_handler_max_size")
		if length := r.Header.Get(contentLen); length != "" {
			// try to parse the value from the `content-length` header
			size, err := strconv.ParseInt(length, 10, 64)
			if err != nil {
				// if got an error while parsing -> assign 500 code to the writer and return
				http.Error(w, "", 500)
				log.Error("error while parsing value from the content-length header", zap.Time("start", start), zap.Duration("elapsed", time.Since(start)))
				return
			}

			if size > int64(maxReqSize) {
				log.Error("request max body size is exceeded", zap.Uint64("allowed_size (MB)", maxReqSize), zap.Int64("actual_size (bytes)", size), zap.Time("start", start), zap.Duration("elapsed", time.Since(start)))
				http.Error(w, errors.E(op, errors.Str("request body max size is exceeded")).Error(), http.StatusBadRequest)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
