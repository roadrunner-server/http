package http

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/roadrunner-server/sdk/v4/metrics"
	"github.com/roadrunner-server/sdk/v4/state/process"
)

type Informer interface {
	Workers() []*process.State
}

func (p *Plugin) MetricsCollector() []prometheus.Collector {
	return []prometheus.Collector{p.statsExporter}
}

func newWorkersExporter(stats Informer) *metrics.StatsExporter {
	return &metrics.StatsExporter{
		TotalWorkersDesc: prometheus.NewDesc("rr_http_total_workers", "Total number of workers used by the HTTP plugin", nil, nil),
		TotalMemoryDesc:  prometheus.NewDesc("rr_http_workers_memory_bytes", "Memory usage by HTTP workers.", nil, nil),
		StateDesc:        prometheus.NewDesc("rr_http_worker_state", "Worker current state", []string{"state", "pid"}, nil),
		WorkerMemoryDesc: prometheus.NewDesc("rr_http_worker_memory_bytes", "Worker current memory usage", []string{"pid"}, nil),

		WorkersReady:   prometheus.NewDesc("rr_http_workers_ready", "HTTP workers currently in ready state", nil, nil),
		WorkersWorking: prometheus.NewDesc("rr_http_workers_working", "HTTP workers currently in working state", nil, nil),
		WorkersInvalid: prometheus.NewDesc("rr_http_workers_invalid", "HTTP workers currently in invalid,killing,destroyed,errored,inactive states", nil, nil),

		Workers: stats,
	}
}
