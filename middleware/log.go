package middleware

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var _ io.ReadCloser = &wrapper{}

type wrapper struct {
	io.ReadCloser
	read int

	code      int
	data      []byte
	HeaderMap http.Header
}

func (w *wrapper) Read(b []byte) (int, error) {
	n, err := w.ReadCloser.Read(b)
	w.read = n
	return n, err
}

func (w *wrapper) Close() error {
	return w.ReadCloser.Close()
}

func (w *wrapper) Write(data []byte) (int, error) {
	w.data = make([]byte, len(data))
	copy(w.data, data)
	return len(data), nil
}

func (w *wrapper) WriteHeader(code int) {
	w.code = code
}

func (w *wrapper) Header() http.Header {
	return w.HeaderMap
}

func (w *wrapper) reset() {
	w.code = 0
	w.read = 0
	w.data = nil
	w.ReadCloser = nil
	w.HeaderMap = make(map[string][]string)
}

type lm struct {
	pool sync.Pool
	log  *zap.Logger
}

func NewLogMiddleware(next http.Handler, accessLogs bool, log *zap.Logger) http.Handler {
	l := &lm{
		log: log,
		pool: sync.Pool{
			New: func() any {
				return &wrapper{}
			},
		},
	}

	return l.Log(next, accessLogs)
}

func (l *lm) Log(next http.Handler, accessLogs bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PRI" {
			next.ServeHTTP(w, r)
			l.log.Info("upgrade", zap.String("method", "PRI"), zap.Any("headers", r.Header))
			return
		}

		start := time.Now()

		bw := l.getW()
		defer l.putW(bw)

		bw.HeaderMap = make(map[string][]string)
		r2 := *r
		if r2.Body != nil {
			bw.ReadCloser = r2.Body
			r2.Body = bw
		}

		next.ServeHTTP(bw, &r2)

		// write headers back
		for k, v := range bw.HeaderMap {
			for i := 0; i < len(v); i++ {
				w.Header().Add(k, v[i])
			}
		}

		// write status code
		w.WriteHeader(bw.code)
		// write data
		_, err := w.Write(bw.data)
		if err != nil {
			l.log.Error("write response", zap.Error(err))
			return
		}

		l.writeLog(accessLogs, r, bw, start)
	})
}

func (l *lm) writeLog(accessLog bool, r *http.Request, bw *wrapper, start time.Time) {
	switch accessLog {
	case false:
		l.log.Info("http log",
			zap.Int("status", bw.code),
			zap.String("method", r.Method),
			zap.String("URI", r.RequestURI),
			zap.String("remote_address", r.RemoteAddr),
			zap.Int("read_bytes", bw.read),
			zap.Time("start", start),
			zap.Duration("elapsed", time.Since(start)))
	case true:
		// external/cwe/cwe-117
		usrA := r.UserAgent()
		usrA = strings.ReplaceAll(usrA, "\n", "")
		usrA = strings.ReplaceAll(usrA, "\r", "")

		rfr := r.Referer()
		rfr = strings.ReplaceAll(rfr, "\n", "")
		rfr = strings.ReplaceAll(rfr, "\r", "")

		l.log.Info("http access log",
			zap.Int("read_bytes", bw.read),
			zap.Int("status", bw.code),
			zap.String("method", r.Method),
			zap.String("URI", r.RequestURI),
			zap.String("remote_address", r.RemoteAddr),
			zap.String("query", r.URL.RawQuery),
			zap.Int64("content_len", r.ContentLength),
			zap.String("host", r.Host),
			zap.String("user_agent", usrA),
			zap.String("referer", rfr),
			zap.String("time_local", time.Now().Format("02/Jan/06:15:04:05 -0700")),
			zap.Time("request_time", time.Now()),
			zap.Time("start", start),
			zap.Duration("elapsed", time.Since(start)))
	}
}

func (l *lm) getW() *wrapper {
	return l.pool.Get().(*wrapper)
}

func (l *lm) putW(w *wrapper) {
	w.reset()
	l.pool.Put(w)
}
