package http

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/roadrunner-server/pool/v2/fsm"
	"github.com/roadrunner-server/pool/v2/state/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeInformer serves a fixed set of worker states to the exporter.
type fakeInformer struct{ states []*process.State }

func (f *fakeInformer) Workers() []*process.State { return f.states }

func TestStatsExporterDescribe(t *testing.T) {
	exporter := newWorkersExporter(&fakeInformer{})

	descCh := make(chan *prometheus.Desc, 16)
	exporter.Describe(descCh)
	close(descCh)

	unique := make(map[*prometheus.Desc]struct{})
	for d := range descCh {
		unique[d] = struct{}{}
	}

	assert.Len(t, unique, 7)
}

// With no workers the exporter still reports the five aggregate gauges.
func TestStatsExporterCollectNoWorkers(t *testing.T) {
	exporter := newWorkersExporter(&fakeInformer{})

	assert.Equal(t, 5, testutil.CollectAndCount(exporter))

	expected := `
# HELP rr_http_total_workers Total number of workers used by the HTTP plugin
# TYPE rr_http_total_workers gauge
rr_http_total_workers 0
# HELP rr_http_workers_memory_bytes Memory usage by HTTP workers.
# TYPE rr_http_workers_memory_bytes gauge
rr_http_workers_memory_bytes 0
# HELP rr_http_workers_ready HTTP workers currently in ready state
# TYPE rr_http_workers_ready gauge
rr_http_workers_ready 0
# HELP rr_http_workers_working HTTP workers currently in working state
# TYPE rr_http_workers_working gauge
rr_http_workers_working 0
# HELP rr_http_workers_invalid HTTP workers currently in invalid,killing,destroyed,errored,inactive states
# TYPE rr_http_workers_invalid gauge
rr_http_workers_invalid 0
`

	require.NoError(t, testutil.CollectAndCompare(exporter, strings.NewReader(expected)))
}

// Every worker contributes two per-worker metrics, and its state falls into one
// of the three aggregate buckets. Errored maps to the default (invalid) arm.
func TestStatsExporterCollectMixedStates(t *testing.T) {
	exporter := newWorkersExporter(&fakeInformer{states: []*process.State{
		{Pid: 1, Status: fsm.StateReady, StatusStr: "ready", MemoryUsage: 100},
		{Pid: 2, Status: fsm.StateWorking, StatusStr: "working", MemoryUsage: 200},
		{Pid: 3, Status: fsm.StateErrored, StatusStr: "errored", MemoryUsage: 300},
	}})

	assert.Equal(t, 11, testutil.CollectAndCount(exporter))

	expected := `
# HELP rr_http_total_workers Total number of workers used by the HTTP plugin
# TYPE rr_http_total_workers gauge
rr_http_total_workers 3
# HELP rr_http_workers_memory_bytes Memory usage by HTTP workers.
# TYPE rr_http_workers_memory_bytes gauge
rr_http_workers_memory_bytes 600
# HELP rr_http_workers_ready HTTP workers currently in ready state
# TYPE rr_http_workers_ready gauge
rr_http_workers_ready 1
# HELP rr_http_workers_working HTTP workers currently in working state
# TYPE rr_http_workers_working gauge
rr_http_workers_working 1
# HELP rr_http_workers_invalid HTTP workers currently in invalid,killing,destroyed,errored,inactive states
# TYPE rr_http_workers_invalid gauge
rr_http_workers_invalid 1
# HELP rr_http_worker_memory_bytes Worker current memory usage
# TYPE rr_http_worker_memory_bytes gauge
rr_http_worker_memory_bytes{pid="1"} 100
rr_http_worker_memory_bytes{pid="2"} 200
rr_http_worker_memory_bytes{pid="3"} 300
# HELP rr_http_worker_state Worker current state
# TYPE rr_http_worker_state gauge
rr_http_worker_state{pid="1",state="ready"} 0
rr_http_worker_state{pid="2",state="working"} 0
rr_http_worker_state{pid="3",state="errored"} 0
`

	require.NoError(t, testutil.CollectAndCompare(exporter, strings.NewReader(expected)))
}

func TestPluginMetricsCollector(t *testing.T) {
	p := &Plugin{}
	p.statsExporter = newWorkersExporter(p)

	collectors := p.MetricsCollector()

	require.Len(t, collectors, 1)
	assert.Same(t, p.statsExporter, collectors[0])
}
