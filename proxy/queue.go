package proxy

import (
	"context"
	"errors"
	"sync"

	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
)

// ErrInboxFull is returned by Submit when the inbox has reached its capacity.
// The producer should typically translate this into a 503 to the client.
var ErrInboxFull = errors.New("proxy: request queue is full")

// ErrQueueClosed is returned when Submit or Next is called on a closed queue.
var ErrQueueClosed = errors.New("proxy: queue is closed")

// ErrDuplicateID is returned by Submit when a request with the same Id is
// already pending. Treat as a programming error.
var ErrDuplicateID = errors.New("proxy: duplicate request Id")

// ErrEmptyID is returned by Submit when the request has no Id set.
var ErrEmptyID = errors.New("proxy: request has empty Id")

// Queue is the broker between HTTP producers (Handler.ServeHTTP) and PHP
// consumers (proxy.Server.FetchRequest / SendResponse).
//
// Producers call Submit and select on the returned channel. Consumers call
// Next to pull the next pending request, then Deliver to hand back a response
// keyed by req.Id. If a producer abandons its wait (client disconnect, timeout),
// it MUST call Cancel(id); a late response from a slow worker is then dropped
// on the floor rather than leaking.
type Queue struct {
	inbox chan *httpV2.HttpHandlerRequest

	mu      sync.Mutex
	pending map[string]chan *httpV2.HttpHandlerResponse
	closed  bool
}

// NewQueue returns a queue with the given inbox capacity. Capacity 0 makes
// Submit fully synchronous with Next.
func NewQueue(inboxSize int) *Queue {
	if inboxSize < 0 {
		inboxSize = 0
	}
	return &Queue{
		inbox:   make(chan *httpV2.HttpHandlerRequest, inboxSize),
		pending: make(map[string]chan *httpV2.HttpHandlerResponse),
	}
}

// Submit registers a response slot keyed by req.Id and enqueues the request.
// Returns the channel the producer should select on for the response.
//
// The returned channel is buffered (cap 1), so Deliver never blocks even if
// the producer has already moved on.
func (q *Queue) Submit(req *httpV2.HttpHandlerRequest) (<-chan *httpV2.HttpHandlerResponse, error) {
	id := req.GetId()
	if id == "" {
		return nil, ErrEmptyID
	}

	respCh := make(chan *httpV2.HttpHandlerResponse, 1)

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, ErrQueueClosed
	}
	if _, dup := q.pending[id]; dup {
		return nil, ErrDuplicateID
	}

	// Non-blocking send under the lock: holding the mutex prevents Close
	// from racing in and closing the inbox between our capacity check and
	// the send (which would panic).
	select {
	case q.inbox <- req:
		q.pending[id] = respCh
		return respCh, nil
	default:
		return nil, ErrInboxFull
	}
}

// Next blocks until a request is available, ctx is canceled, or the queue is
// closed. Returns ctx.Err() on cancellation, ErrQueueClosed once the inbox is
// drained after Close.
func (q *Queue) Next(ctx context.Context) (*httpV2.HttpHandlerRequest, error) {
	select {
	case req, ok := <-q.inbox:
		if !ok {
			return nil, ErrQueueClosed
		}
		return req, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TryNext returns the next pending request without blocking. Returns nil if
// the inbox is empty or the queue is closed. Use in batch-fetch paths to
// avoid blocking on the second-and-subsequent requests when a concurrent
// consumer could otherwise strand us.
func (q *Queue) TryNext() *httpV2.HttpHandlerRequest {
	select {
	case req, ok := <-q.inbox:
		if !ok {
			return nil
		}
		return req
	default:
		return nil
	}
}

func (q *Queue) Len() int {
	return len(q.inbox)
}

// Deliver hands a response to whoever is waiting on this Id. Returns false if
// the id is unknown — meaning the producer already canceled (client disconnect
// or timeout), or the same response is being delivered twice. Both cases drop
// the response on the floor.
func (q *Queue) Deliver(id string, resp *httpV2.HttpHandlerResponse) bool {
	q.mu.Lock()
	ch, ok := q.pending[id]
	if ok {
		delete(q.pending, id)
	}
	q.mu.Unlock()

	if !ok {
		return false
	}
	// ch has capacity 1 and only Deliver writes to it under exclusive lookup,
	// so this never blocks.
	ch <- resp
	return true
}

// Cancel removes the pending entry. Idempotent. If Deliver wins the race,
// Cancel is a no-op; if Cancel wins, a later Deliver returns false.
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	delete(q.pending, id)
	q.mu.Unlock()
}

// Close stops accepting new submissions and unblocks consumers parked in Next.
// In-flight requests already pulled from the inbox will land in Deliver if the
// worker still sends a response; producers that have given up will have called
// Cancel, so those responses are dropped.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.inbox)
}

// Pending reports the number of producers currently waiting for responses.
// Intended for metrics / debug.
func (q *Queue) Pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}
