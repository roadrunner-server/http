package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"google.golang.org/protobuf/types/known/emptypb"
)

// newTestServer builds a *Server suitable for unit tests — no listener, no
// TCP. The *http.Server inside is irrelevant for direct method calls.
func newTestServer(q *Queue) *Server {
	return &Server{
		queue: q,
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestServer_FetchRequests_OK(t *testing.T) {
	q := NewQueue(64)
	s := newTestServer(q)

	const N = 5
	want := make([]string, N)
	for i := range N {
		id := newID(t)
		want[i] = id
		if _, err := q.Submit(&httpV2.HttpHandlerRequest{Id: id, Method: "GET"}); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	resp, err := s.FetchRequests(t.Context(), connect.NewRequest(&httpV2.HttpHandlerFetchRequest{BatchSize: int64(N + 5)}))
	if err != nil {
		t.Fatalf("FetchRequests: %v", err)
	}

	got := resp.Msg.GetRequests()
	if len(got) != N {
		t.Fatalf("got %d requests, want %d", len(got), N)
	}
	for i, r := range got {
		if r.GetId() != want[i] {
			t.Fatalf("request[%d].Id = %q, want %q", i, r.GetId(), want[i])
		}
	}

	if pending := q.Len(); pending != 0 {
		t.Fatalf("inbox should be drained, has %d", pending)
	}
}

// TestServer_FetchRequests_DoesNotStrandOnConcurrentSteal exercises the bug
// the user flagged: while FetchRequests is iterating, a concurrent
// FetchRequest can take an inbox slot the batch loop was about to consume.
// With the prior implementation (loop of blocking Next() up to a pre-checked
// Len()), that loss would strand the batch loop on the empty channel forever.
// The current implementation uses TryNext() for the tail, so it returns what
// it has within a bounded time.
//
// We use time.Sleep to coax the steal into firing AFTER FetchRequests's
// first (blocking) Next() but before its TryNext drain runs.
func TestServer_FetchRequests_DoesNotStrandOnConcurrentSteal(t *testing.T) {
	q := NewQueue(64)
	s := newTestServer(q)

	const N = 5
	for i := range N {
		if _, err := q.Submit(&httpV2.HttpHandlerRequest{Id: newID(t)}); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	// Steal one mid-flight. The 30ms sleep gives FetchRequests time to enter
	// the TryNext drain loop (after its initial Next succeeds).
	stealErr := make(chan error, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := s.FetchRequest(ctx, connect.NewRequest(&emptypb.Empty{}))
		stealErr <- err
	}()

	// Run FetchRequests in a goroutine so we can fail fast if it blocks.
	done := make(chan int, 1)
	errCh := make(chan error, 1)
	go func() {
		// Slight delay so FetchRequests' first Next() likely lands before the
		// steal goroutine wakes up. (Order doesn't affect correctness, only
		// which side of the race fires.)
		time.Sleep(10 * time.Millisecond)
		resp, err := s.FetchRequests(t.Context(), connect.NewRequest(&httpV2.HttpHandlerFetchRequest{BatchSize: int64(N + 5)}))
		if err != nil {
			errCh <- err
			return
		}
		done <- len(resp.Msg.GetRequests())
	}()

	select {
	case n := <-done:
		// Steal took at most 1; FetchRequests gets the rest. Combined count
		// must equal N, and FetchRequests itself must be between N-1 and N.
		if n < N-1 || n > N {
			t.Fatalf("FetchRequests returned %d requests, want %d or %d", n, N-1, N)
		}
		if got := <-stealErr; got != nil {
			// Steal can legitimately fail if FetchRequests drained everything
			// before the steal woke up — its 1s ctx then times out. That's a
			// legitimate timing outcome, not a bug.
			if !errors.Is(got, context.DeadlineExceeded) {
				t.Fatalf("steal FetchRequest: %v", got)
			}
		}
	case err := <-errCh:
		t.Fatalf("FetchRequests error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("FetchRequests blocked — concurrent steal stranded the batch loop")
	}

	if pending := q.Len(); pending != 0 {
		t.Fatalf("inbox should be drained, has %d", pending)
	}
}
