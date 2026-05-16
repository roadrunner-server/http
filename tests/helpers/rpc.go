package helpers

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"

	"github.com/roadrunner-server/api-go/v6/informer/v1/informerV1connect"
	"github.com/roadrunner-server/api-go/v6/resetter/v1/resetterV1connect"
	"golang.org/x/net/http2"
)

// rpcH2CClient builds an h2c-capable HTTP client for ConnectRPC calls to
// RoadRunner's RPC server (which serves HTTP/1 + cleartext HTTP/2).
func rpcH2CClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}
}

// rpcURL turns "tcp://host:port" (RPC DSN format used in configs) or a bare
// "host:port" into an "http://host:port" URL suitable for ConnectRPC clients.
func rpcURL(addr string) string {
	if rest, ok := strings.CutPrefix(addr, "tcp://"); ok {
		return "http://" + rest
	}
	return "http://" + addr
}

// RPCInformerClient builds a ConnectRPC InformerService client for the given
// RR rpc address (e.g. "127.0.0.1:6001" or "tcp://127.0.0.1:6001").
func RPCInformerClient(address string) informerV1connect.InformerServiceClient {
	return informerV1connect.NewInformerServiceClient(rpcH2CClient(), rpcURL(address))
}

// RPCResetterClient builds a ConnectRPC ResetterService client.
func RPCResetterClient(address string) resetterV1connect.ResetterServiceClient {
	return resetterV1connect.NewResetterServiceClient(rpcH2CClient(), rpcURL(address))
}
