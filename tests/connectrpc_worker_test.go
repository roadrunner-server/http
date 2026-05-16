package tests

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect"
	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/api-go/v6/http/v2/httpV2connect"
	"github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/logger/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestConnectRPCWorker emulates a PHP worker connecting via ConnectRPC.
//
// Flow:
//  1. Endure brings up logger + server + http plugin.
//  2. A goroutine acting as the worker opens an h2c ConnectRPC client to
//     http.proxy.address and calls FetchRequest (blocks).
//  3. After 2 seconds, the test fires a public HTTP GET. The plugin builds
//     an HttpHandlerRequest, submits it to the queue.
//  4. FetchRequest unblocks with the request — assertions on Id, Method,
//     Uri, and headers.
//
// We never call SendResponse — the HTTP client times out client-side and
// Handler.ServeHTTP hits ctx.Done, which is the documented drop-on-floor path.
func TestConnectRPCWorker(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-connectrpc-worker.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	)
	require.NoError(t, err)

	err = cont.Init()
	require.NoError(t, err)

	ch, err := cont.Serve()
	require.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "plugin error", e.Error.Error())
				_ = cont.Stop()
				return
			case <-sig:
				_ = cont.Stop()
				return
			case <-stopCh:
				if stopErr := cont.Stop(); stopErr != nil {
					assert.FailNow(t, "stop error", stopErr.Error())
				}
				return
			}
		}
	})

	// h2c transport so the ConnectRPC client can speak to the cleartext HTTP/2
	// listener.
	tr := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	proxyClient := httpV2connect.NewHttpProxyServiceClient(
		&http.Client{Transport: tr},
		"http://127.0.0.1:7077",
	)

	// give Endure a moment to bind both listeners
	time.Sleep(500 * time.Millisecond)

	type fetchResult struct {
		req *httpV2.HttpHandlerRequest
		err error
	}
	fetchCh := make(chan fetchResult, 1)

	// worker: park on FetchRequest until the producer submits to the queue
	go func() {
		fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, fetchErr := proxyClient.FetchRequest(fetchCtx, connect.NewRequest(&emptypb.Empty{}))
		if fetchErr != nil {
			fetchCh <- fetchResult{err: fetchErr}
			return
		}
		fetchCh <- fetchResult{req: resp.Msg}
	}()

	// wait two seconds, per the test brief, then emit a real HTTP request
	time.Sleep(2 * time.Second)

	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:19099/foo?bar=baz", nil)
		req.Header.Set("X-Custom", "hello")
		// Fire-and-forget: no SendResponse will arrive, so the client will time
		// out and Handler.ServeHTTP will Cancel(id). That's the expected path.
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case got := <-fetchCh:
		require.NoError(t, got.err)
		require.NotNil(t, got.req)

		assert.NotEmpty(t, got.req.GetId(), "request Id should be set (uuid v7)")
		assert.Equal(t, http.MethodGet, got.req.GetMethod())
		assert.Contains(t, got.req.GetUri(), "/foo?bar=baz")

		if hv, ok := got.req.GetHeader()["X-Custom"]; assert.True(t, ok, "X-Custom header missing on request") {
			assert.Equal(t, []string{"hello"}, hv.GetValues())
		}

	case <-time.After(10 * time.Second):
		t.Fatal("FetchRequest did not unblock after public HTTP request was fired")
	}

	stopCh <- struct{}{}
	wg.Wait()
}
