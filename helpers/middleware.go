package helpers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/roadrunner-server/api/v2/plugins/middleware"
	"go.uber.org/zap"
)

// TLSAddr replaces listen or host port with port configured by SSLConfig config.
func TLSAddr(host string, forcePort bool, sslPort int) string {
	// remove current forcePort first
	host = strings.Split(host, ":")[0]

	if forcePort || sslPort != 443 {
		host = fmt.Sprintf("%s:%v", host, sslPort)
	}

	return host
}

func ApplyMiddleware(server *http.Server, middleware map[string]middleware.Middleware, order []string, log *zap.Logger) {
	for i := 0; i < len(order); i++ {
		if mdwr, ok := middleware[order[i]]; ok {
			server.Handler = mdwr.Middleware(server.Handler)
		} else {
			log.Warn("requested middleware does not exist", zap.String("requested", order[i]))
		}
	}
}
