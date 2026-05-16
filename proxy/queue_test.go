package proxy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
)

func TestQueue_SubmitDeliverRoundTrip(t *testing.T) {
	q := NewQueue(4)

	req := &httpV2.HttpHandlerRequest{Id: "req-1", Method: "GET"}
	respCh, err := q.Submit(req)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	got, err := q.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.GetId() != "req-1" {
		t.Fatalf("Next returned wrong request: %q", got.GetId())
	}

	resp := &httpV2.HttpHandlerResponse{Status: 200}
	if !q.Deliver("req-1", resp) {
		t.Fatal("Deliver returned false for a registered Id")
	}

	select {
	case got := <-respCh:
		if got.GetStatus() != 200 {
			t.Fatalf("response status = %d, want 200", got.GetStatus())
		}
	case <-time.After(time.Second):
		t.Fatal("respCh did not receive")
	}

	if q.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0 after Deliver", q.Pending())
	}
}

func TestQueue_CancelBeforeDeliver(t *testing.T) {
	q := NewQueue(4)

	respCh, err := q.Submit(&httpV2.HttpHandlerRequest{Id: "req-1"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	q.Cancel("req-1")

	if q.Deliver("req-1", &httpV2.HttpHandlerResponse{Status: 200}) {
		t.Fatal("Deliver returned true after Cancel; should drop")
	}

	select {
	case <-respCh:
		t.Fatal("respCh received a response after Cancel")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestQueue_DeliverWinsRace(t *testing.T) {
	// Cancel after Deliver should be a no-op (idempotent) — the entry is
	// already gone. The producer must have received the response.
	q := NewQueue(4)
	respCh, _ := q.Submit(&httpV2.HttpHandlerRequest{Id: "x"})

	if !q.Deliver("x", &httpV2.HttpHandlerResponse{Status: 201}) {
		t.Fatal("Deliver returned false on registered Id")
	}
	q.Cancel("x")

	select {
	case got := <-respCh:
		if got.GetStatus() != 201 {
			t.Fatalf("status = %d", got.GetStatus())
		}
	case <-time.After(time.Second):
		t.Fatal("respCh missed the delivered response")
	}
}

func TestQueue_InboxFull(t *testing.T) {
	q := NewQueue(1)

	if _, err := q.Submit(&httpV2.HttpHandlerRequest{Id: "a"}); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	_, err := q.Submit(&httpV2.HttpHandlerRequest{Id: "b"})
	if !errors.Is(err, ErrInboxFull) {
		t.Fatalf("second Submit err = %v, want ErrInboxFull", err)
	}
	if q.Pending() != 1 {
		t.Fatalf("Pending = %d, want 1 (b should have rolled back)", q.Pending())
	}
}

func TestQueue_NextRespectsContext(t *testing.T) {
	q := NewQueue(4)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := q.Next(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestQueue_CloseUnblocksNext(t *testing.T) {
	q := NewQueue(4)

	done := make(chan error, 1)
	go func() {
		_, err := q.Next(context.Background())
		done <- err
	}()

	// give the goroutine a tick to park in Next
	time.Sleep(10 * time.Millisecond)
	q.Close()

	select {
	case err := <-done:
		if !errors.Is(err, ErrQueueClosed) {
			t.Fatalf("err = %v, want ErrQueueClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Next did not unblock after Close")
	}
}

func TestQueue_SubmitOnClosed(t *testing.T) {
	q := NewQueue(4)
	q.Close()

	_, err := q.Submit(&httpV2.HttpHandlerRequest{Id: "x"})
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("err = %v, want ErrQueueClosed", err)
	}
}

func TestQueue_ValidatesIDs(t *testing.T) {
	q := NewQueue(4)

	if _, err := q.Submit(&httpV2.HttpHandlerRequest{}); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("empty id err = %v, want ErrEmptyID", err)
	}

	if _, err := q.Submit(&httpV2.HttpHandlerRequest{Id: "dup"}); err != nil {
		t.Fatalf("first dup submit: %v", err)
	}
	if _, err := q.Submit(&httpV2.HttpHandlerRequest{Id: "dup"}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("dup id err = %v, want ErrDuplicateID", err)
	}
}

func TestQueue_ConcurrentSubmitDeliver(t *testing.T) {
	const N = 200
	q := NewQueue(N)

	// Start a consumer that drains requests and immediately delivers a response.
	consumerDone := make(chan struct{})
	var delivered atomic.Int64
	go func() {
		defer close(consumerDone)
		for range N {
			req, err := q.Next(context.Background())
			if err != nil {
				t.Errorf("Next: %v", err)
				return
			}
			if !q.Deliver(req.GetId(), &httpV2.HttpHandlerResponse{Status: 200}) {
				t.Errorf("Deliver returned false for %q", req.GetId())
				return
			}
			delivered.Add(1)
		}
	}()

	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			id := newID(t)
			respCh, err := q.Submit(&httpV2.HttpHandlerRequest{Id: id})
			if err != nil {
				t.Errorf("Submit %s: %v", id, err)
				return
			}
			select {
			case <-respCh:
			case <-time.After(2 * time.Second):
				t.Errorf("timeout waiting for %s", id)
			}
		})
	}

	wg.Wait()
	<-consumerDone

	if got := delivered.Load(); got != N {
		t.Fatalf("delivered = %d, want %d", got, N)
	}
	if q.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0", q.Pending())
	}
}

// newID returns a fresh UUIDv7 string — same generator the producer uses in
// handler.ServeHTTP.
func newID(t testing.TB) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return id.String()
}
