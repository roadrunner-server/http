package http

import (
	"net/http"

	"github.com/roadrunner-server/api/v4/plugins/v1/status"
	"github.com/roadrunner-server/pool/fsm"
)

// Status return status of the particular plugin
func (p *Plugin) Status() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := p.pool.Workers()

	for i := range workers {
		if workers[i].State().IsActive() {
			return &status.Status{
				Code: http.StatusOK,
			}, nil
		}
	}
	// if there are no workers, threat this as error
	return &status.Status{
		Code: http.StatusServiceUnavailable,
	}, nil
}

// Ready return readiness status of the particular plugin
func (p *Plugin) Ready() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := p.pool.Workers()

	for i := range workers {
		// If state of the worker is ready (at least 1)
		// we assume, that plugin's worker pool is ready
		if workers[i].State().Compare(fsm.StateReady) {
			return &status.Status{
				Code: http.StatusOK,
			}, nil
		}
	}
	// if there are no workers, threat this as no content error
	return &status.Status{
		Code: http.StatusServiceUnavailable,
	}, nil
}
