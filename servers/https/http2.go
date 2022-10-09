package https

import (
	"net/http"

	"golang.org/x/net/http2"
)

// init http/2 server
func initHTTP2(server *http.Server, streams uint32) error {
	return http2.ConfigureServer(server, &http2.Server{
		MaxConcurrentStreams: streams,
	})
}
