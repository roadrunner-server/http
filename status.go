package http

import (
	"net/http"

	"github.com/roadrunner-server/api-plugins/v6/status"
)

// Status return status of the particular plugin
func (p *Plugin) Status() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return &status.Status{
			Code: http.StatusServiceUnavailable,
		}, nil
	}

	workers := p.pool.Workers()

	for _, w := range workers {
		if w.State().IsActive() {
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

// Ready returns readiness status of the particular plugin.
func (p *Plugin) Ready() (*status.Status, error) {
	return p.Status()
}
