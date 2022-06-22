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
var _ http.ResponseWriter = &wrapper{}

type wrapper struct {
	io.ReadCloser
	read  int
	write int

	w    http.ResponseWriter
	code int
	data []byte
}

func (w *wrapper) Read(b []byte) (int, error) {
	n, err := w.ReadCloser.Read(b)
	w.read += n
	return n, err
}

func (w *wrapper) WriteHeader(code int) {
	w.code = code
	w.w.WriteHeader(code)
}

func (w *wrapper) Header() http.Header {
	return w.w.Header()
}

func (w *wrapper) Write(b []byte) (int, error) {
	n, err := w.w.Write(b)
	w.write += n
	return n, err
}

func (w *wrapper) Close() error {
	return w.ReadCloser.Close()
}

func (w *wrapper) reset() {
	w.code = 0
	w.read = 0
	w.write = 0
	w.w = nil
	w.data = nil
	w.ReadCloser = nil
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
		start := time.Now()

		bw := l.getW(w)
		defer l.putW(bw)

		r2 := *r
		if r2.Body != nil {
			bw.ReadCloser = r2.Body
			r2.Body = bw
		}

		next.ServeHTTP(bw, &r2)
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
			zap.Int("write_bytes", bw.write),
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

		rq := r.URL.RawQuery
		rq = strings.ReplaceAll(rq, "\n", "")
		rq = strings.ReplaceAll(rq, "\r", "")

		l.log.Info("http access log",
			zap.Int("read_bytes", bw.read),
			zap.Int("write_bytes", bw.write),
			zap.Int("status", bw.code),
			zap.String("method", r.Method),
			zap.String("URI", r.RequestURI),
			zap.String("remote_address", r.RemoteAddr),
			zap.String("query", rq),
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

func (l *lm) getW(w http.ResponseWriter) *wrapper {
	wr := l.pool.Get().(*wrapper)
	wr.w = w
	return wr
}

func (l *lm) putW(w *wrapper) {
	w.reset()
	l.pool.Put(w)
}
