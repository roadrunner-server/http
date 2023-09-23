package http

import (
	"context"
)

func (p *Plugin) AddWorker() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool.AddWorker()
}

func (p *Plugin) RemoveWorker(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool.RemoveWorker(ctx)
}
