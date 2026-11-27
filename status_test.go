package http

import (
	"context"
	"log/slog"
	"net/http"
	"os/exec"
	"testing"

	"github.com/roadrunner-server/http/v6/api"
	"github.com/roadrunner-server/pool/v2/fsm"
	"github.com/roadrunner-server/pool/v2/payload"
	staticPool "github.com/roadrunner-server/pool/v2/pool/static_pool"
	"github.com/roadrunner-server/pool/v2/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPool satisfies api.Pool. Only Workers is exercised by the tests below.
type mockPool struct{ workers []*worker.Process }

func (m *mockPool) Workers() []*worker.Process           { return m.workers }
func (m *mockPool) RemoveWorker(_ context.Context) error { return nil }
func (m *mockPool) AddWorker() error                     { return nil }
func (m *mockPool) Exec(_ context.Context, _ *payload.Payload, _ chan struct{}) (chan *staticPool.PExec, error) {
	return nil, nil
}
func (m *mockPool) Reset(_ context.Context) error { return nil }
func (m *mockPool) Destroy(_ context.Context)     {}

// newWorkerInState builds a worker process that is never started, then walks its
// fsm to the requested state. The fsm only accepts working after ready.
func newWorkerInState(t *testing.T, state int64) *worker.Process {
	t.Helper()

	w, err := worker.InitBaseWorker(exec.CommandContext(t.Context(), "php", "-v"), worker.WithLog(slog.New(slog.DiscardHandler)))
	require.NoError(t, err)

	if state == fsm.StateWorking {
		w.State().Transition(fsm.StateReady)
	}
	w.State().Transition(state)
	require.True(t, w.State().Compare(state))

	return w
}

func TestPluginStatusAndReady(t *testing.T) {
	tests := []struct {
		name       string
		pool       api.Pool
		statusCode int
		readyCode  int
	}{
		{
			name:       "nil pool",
			pool:       nil,
			statusCode: http.StatusServiceUnavailable,
			readyCode:  http.StatusServiceUnavailable,
		},
		{
			name:       "no workers",
			pool:       &mockPool{},
			statusCode: http.StatusServiceUnavailable,
			readyCode:  http.StatusServiceUnavailable,
		},
		{
			name:       "ready worker",
			pool:       &mockPool{workers: []*worker.Process{newWorkerInState(t, fsm.StateReady)}},
			statusCode: http.StatusOK,
			readyCode:  http.StatusOK,
		},
		{
			name:       "inactive worker",
			pool:       &mockPool{workers: []*worker.Process{newWorkerInState(t, fsm.StateInactive)}},
			statusCode: http.StatusServiceUnavailable,
			readyCode:  http.StatusServiceUnavailable,
		},
		{
			// An active working worker satisfies both status and readiness.
			name:       "working worker",
			pool:       &mockPool{workers: []*worker.Process{newWorkerInState(t, fsm.StateWorking)}},
			statusCode: http.StatusOK,
			readyCode:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{pool: tt.pool}

			st, err := p.Status()
			require.NoError(t, err)
			assert.Equal(t, tt.statusCode, st.Code)

			rd, err := p.Ready()
			require.NoError(t, err)
			assert.Equal(t, tt.readyCode, rd.Code)
		})
	}
}
