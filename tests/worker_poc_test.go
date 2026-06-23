package tests

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/logger/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkerPoC boots RoadRunner from .rr-worker-poc.yaml and exercises the
// real PoC PHP worker end-to-end:
// HTTP client -> http plugin -> queue -> HttpProxyService (h2c) -> PHP worker.
//
// worker-poc.php is not a pipes worker: the pool only owns its process
// lifecycle. The worker dials the http plugin's worker-facing proxy listener
// (http.proxy.address default 127.0.0.1:7070, hardcoded in the worker), then
// long-polls FetchRequest and answers via SendResponse.
func TestWorkerPoC(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-worker-poc.yaml",
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

	// give the pool time to spawn the PHP worker and let it park on FetchRequest
	time.Sleep(time.Second * 2)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8080", nil)
	require.NoError(t, err)

	// short client timeout: a worker that never answers must fail the test in
	// seconds, not after the proxy's 60s request_timeout
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.NoError(t, resp.Body.Close())

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "Hello from the gRPC PoC worker!")
	assert.Contains(t, string(body), "Request ID: ")
	assert.Equal(t, "rr-grpc-poc", resp.Header.Get("X-Powered-By"))

	stopCh <- struct{}{}
	wg.Wait()
}
