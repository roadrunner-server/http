package tests

import (
	"fmt"
	"log/slog"
	"net/rpc"
	"testing"
	"time"

	"tests/helpers"

	"github.com/roadrunner-server/gzip/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/informer/v6"
	"github.com/roadrunner-server/resetter/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/roadrunner-server/send/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
)

func TestHTTPInformerReset(t *testing.T) {
	helpers.Start(t, "configs/.rr-resetter.yaml", []any{
		&rpcPlugin.Plugin{},
		&send.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&informer.Plugin{},
		&resetter.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:10084"))

	client := helpers.RPC(t, "127.0.0.1:6008")

	requireWorkers(t, client, 2)
	helpers.AssertGet(t, "http://127.0.0.1:10084?hello=world", 201, "WORLD")

	var reset bool
	require.NoError(t, client.Call("resetter.Reset", "http", &reset))
	require.True(t, reset)

	var services []string
	require.NoError(t, client.Call("resetter.List", nil, &services))
	require.NotEmpty(t, services)
	require.Equal(t, "http", services[0])

	// the pool is fresh after the reset and still serves
	helpers.AssertGet(t, "http://127.0.0.1:10084?hello=world", 201, "WORLD")
}

func TestHTTPSupervisedPool(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-supervised-pool.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&informer.Plugin{},
	}, helpers.WithProbe("http://127.0.0.1:18888"))

	client := helpers.RPC(t, "127.0.0.1:15432")

	helpers.AssertGet(t, "http://127.0.0.1:18888?hello=world", 201, "WORLD")
	requireWorkerRecycled(t, client)

	// the replacement worker serves and is recycled in turn
	helpers.AssertGet(t, "http://127.0.0.1:18888?hello=world", 201, "WORLD")
	requireWorkerRecycled(t, client)
}

func TestHTTPAddWorkers(t *testing.T) {
	helpers.Start(t, "configs/.rr-http-workers1.yaml", []any{
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&informer.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithLogLevel(slog.LevelError), helpers.WithProbe("http://127.0.0.1:44556"))

	client := helpers.RPC(t, "127.0.0.1:30301")

	requireWorkers(t, client, 2)

	addWorker(t, client)
	requireWorkers(t, client, 3)

	// the pool keeps the last worker, so three removals leave one
	for range 3 {
		removeWorker(t, client)
	}
	requireWorkers(t, client, 1)

	addWorker(t, client)

	code, _ := helpers.GetBody(t, "http://127.0.0.1:44556")
	require.Equal(t, 200, code)

	helpers.AssertGet(t, "http://127.0.0.1:44556", 200, "hello world")

	addWorker(t, client)
	requireWorkers(t, client, 3)
}

// https://github.com/laravel/octane/issues/504
func TestHTTPExecTTL(t *testing.T) {
	// a TCP probe instead of a request one: a probe request would hit exec_ttl as
	// well and add a second restart record
	rr, stop := helpers.Start(t, "configs/.rr-http-exec-ttl.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithTCPProbe("127.0.0.1:18988"))

	// the worker sleeps past exec_ttl (2s), the supervisor kills it mid-request
	code, _ := helpers.GetBody(t, "http://127.0.0.1:18988")
	require.Equal(t, 500, code)

	stop()

	// count the execTTL restart only, not other worker lifecycle events
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("worker stopped, and will be restarted").Len())
}

func TestDebugModeResponse(t *testing.T) {
	helpers.Start(t, "configs/.rr-debugmode-fail.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithProbe("http://127.0.0.1:19995"))

	// pool debug mode: the worker is spawned per request and its error is returned
	code, body := helpers.GetBody(t, "http://127.0.0.1:19995")
	require.Contains(t, body, "Exception: test")
	require.Equal(t, 500, code)
}

// workerPIDs returns the pids of the http workers the informer reports. It takes
// no *testing.T: require.Eventually runs its condition off the test goroutine.
func workerPIDs(client *rpc.Client) ([]int, error) {
	var list helpers.WorkersList
	if err := client.Call("informer.Workers", "http", &list); err != nil {
		return nil, fmt.Errorf("informer.Workers: %w", err)
	}

	pids := make([]int, 0, len(list.Workers))
	for _, w := range list.Workers {
		pids = append(pids, int(w.Pid))
	}

	return pids, nil
}

// requireWorkers requires the pool to hold exactly n workers and returns their pids.
func requireWorkers(t *testing.T, client *rpc.Client, n int) []int {
	t.Helper()

	pids, err := workerPIDs(client)
	require.NoError(t, err)
	require.Len(t, pids, n)

	return pids
}

// requireWorkerRecycled waits for the supervisor to destroy the single idle
// worker (idle_ttl) and for the pool to allocate one with a different pid.
func requireWorkerRecycled(t *testing.T, client *rpc.Client) {
	t.Helper()

	pid := requireWorkers(t, client, 1)[0]

	require.Eventually(t, func() bool {
		pids, err := workerPIDs(client)
		return err == nil && len(pids) == 1 && pids[0] != pid
	}, time.Second*15, time.Millisecond*200, "the supervisor did not replace the idle worker")
}

func addWorker(t *testing.T, client *rpc.Client) {
	t.Helper()

	var ok bool
	require.NoError(t, client.Call("informer.AddWorker", "http", &ok))
}

func removeWorker(t *testing.T, client *rpc.Client) {
	t.Helper()

	var ok bool
	require.NoError(t, client.Call("informer.RemoveWorker", "http", &ok))
}
