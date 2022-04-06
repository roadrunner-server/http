package http

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/roadrunner-server/api/v2/plugins/informer"
)

func (p *Plugin) MetricsCollector() []prometheus.Collector {
	return []prometheus.Collector{p.statsExporter}
}

const (
	readySt   string = "ready"
	workingSt string = "working"
)

type statsExporter struct {
	totalMemoryDesc  *prometheus.Desc
	stateDesc        *prometheus.Desc
	workerMemoryDesc *prometheus.Desc
	totalWorkersDesc *prometheus.Desc

	workersReady   *prometheus.Desc
	workersWorking *prometheus.Desc
	workersInvalid *prometheus.Desc

	workers informer.Informer
}

func newWorkersExporter(stats informer.Informer) *statsExporter {
	return &statsExporter{
		totalWorkersDesc: prometheus.NewDesc("rr_http_total_workers", "Total number of workers used by the HTTP plugin", nil, nil),
		totalMemoryDesc:  prometheus.NewDesc("rr_http_workers_memory_bytes", "Memory usage by HTTP workers.", nil, nil),
		stateDesc:        prometheus.NewDesc("rr_http_worker_state", "Worker current state", []string{"state", "pid"}, nil),
		workerMemoryDesc: prometheus.NewDesc("rr_http_worker_memory_bytes", "Worker current memory usage", []string{"pid"}, nil),

		workersReady:   prometheus.NewDesc("rr_http_workers_ready", "HTTP workers currently in ready state", nil, nil),
		workersWorking: prometheus.NewDesc("rr_http_workers_working", "HTTP workers currently in working state", nil, nil),
		workersInvalid: prometheus.NewDesc("rr_http_workers_invalid", "HTTP workers currently in invalid,killing,destroyed,errored,inactive states", nil, nil),

		workers: stats,
	}
}

func (s *statsExporter) Describe(d chan<- *prometheus.Desc) {
	// send description
	d <- s.totalWorkersDesc
	d <- s.totalMemoryDesc
	d <- s.stateDesc
	d <- s.workerMemoryDesc

	d <- s.workersReady
	d <- s.workersWorking
	d <- s.workersInvalid
}

func (s *statsExporter) Collect(ch chan<- prometheus.Metric) {
	// get the copy of the processes
	workers := s.workers.Workers()

	// cumulative RSS memory in bytes
	var cum float64

	var ready float64
	var working float64
	var invalid float64

	// collect the memory
	for i := 0; i < len(workers); i++ {
		cum += float64(workers[i].MemoryUsage)

		ch <- prometheus.MustNewConstMetric(s.stateDesc, prometheus.GaugeValue, 0, workers[i].Status, strconv.Itoa(workers[i].Pid))
		ch <- prometheus.MustNewConstMetric(s.workerMemoryDesc, prometheus.GaugeValue, float64(workers[i].MemoryUsage), strconv.Itoa(workers[i].Pid))

		// sync with sdk/worker/state.go
		switch workers[i].Status {
		case readySt:
			ready++
		case workingSt:
			working++
		default:
			invalid++
		}
	}

	ch <- prometheus.MustNewConstMetric(s.workersReady, prometheus.GaugeValue, ready)
	ch <- prometheus.MustNewConstMetric(s.workersWorking, prometheus.GaugeValue, working)
	ch <- prometheus.MustNewConstMetric(s.workersInvalid, prometheus.GaugeValue, invalid)

	// send the values to the prometheus
	ch <- prometheus.MustNewConstMetric(s.totalWorkersDesc, prometheus.GaugeValue, float64(len(workers)))
	ch <- prometheus.MustNewConstMetric(s.totalMemoryDesc, prometheus.GaugeValue, cum)
}
