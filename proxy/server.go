package proxy

import (
	"context"
	stderr "errors"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/api-go/v6/http/v2/httpV2connect"
	"github.com/roadrunner-server/tcplisten"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Config configures the ConnectRPC proxy server that PHP workers connect into.
type Config struct {
	// Address is the TCP address to listen on, e.g. ":7070".
	Address string
	// ReadHeaderTimeout caps how long a worker can keep a connection open without
	// sending request headers. Defaults to 1 minute.
	ReadHeaderTimeout time.Duration
}

// Server hosts the HttpProxyService over h2c. Workers connect IN, pull work
// from the Queue via FetchRequest, and return results via SendResponse.
type Server struct {
	queue *Queue
	log   *slog.Logger
	cfg   Config

	http *http.Server
}

// Compile-time check that Server satisfies the generated handler interface.
var _ httpV2connect.HttpProxyServiceHandler = (*Server)(nil)

func NewServer(cfg Config, queue *Queue, log *slog.Logger) *Server {
	if cfg.ReadHeaderTimeout == 0 {
		cfg.ReadHeaderTimeout = time.Minute
	}

	s := &Server{queue: queue, log: log, cfg: cfg}

	mux := http.NewServeMux()
	path, handler := httpV2connect.NewHttpProxyServiceHandler(s)
	mux.Handle(path, handler)

	protocols := new(http.Protocols)
	protocols.SetHTTP2(true)
	protocols.SetHTTP1(false)
	protocols.SetUnencryptedHTTP2(true)

	// Build *http.Server eagerly so a concurrent Stop() can always Shutdown it
	// even if Serve hasn't bound the listener yet.
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		Protocols:         protocols,
	}
	return s
}

// Serve blocks until the listener errors or Stop is called.
func (s *Server) Serve() error {
	listener, err := tcplisten.CreateListener(s.cfg.Address)
	if err != nil {
		return err
	}

	s.log.Info("proxy server listening", "address", listener.Addr().String())
	err = s.http.Serve(listener)
	if stderr.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Stop performs a graceful shutdown: new RPCs are rejected, in-flight RPCs
// have until ctx deadline to complete.
func (s *Server) Stop(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// FetchRequest pulls the next pending request from the queue. Blocks until
// a request arrives, the worker disconnects (ctx canceled), or the queue is
// closed.
func (s *Server) FetchRequest(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[httpV2.HttpHandlerRequest], error) {
	req, err := s.queue.Next(ctx)
	if err != nil {
		switch {
		case stderr.Is(err, context.Canceled), stderr.Is(err, context.DeadlineExceeded):
			return nil, connect.NewError(connect.CodeCanceled, err)
		case stderr.Is(err, ErrQueueClosed):
			return nil, connect.NewError(connect.CodeUnavailable, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	return connect.NewResponse(req), nil
}

// FetchRequests pulls up to BatchSize requests from the queue in one round
// trip. It blocks for the first request (matching FetchRequest semantics) and
// then non-blockingly drains up to BatchSize-1 more. The non-blocking second
// phase prevents a concurrent FetchRequest/FetchRequests consumer from
// stranding us on Next() after we've already collected some — we return what
// we have rather than waiting for a request that may never arrive.
func (s *Server) FetchRequests(ctx context.Context, req *connect.Request[httpV2.HttpHandlerFetchRequest]) (*connect.Response[httpV2.HttpHandlerRequests], error) {
	batch := int(req.Msg.GetBatchSize())
	if batch <= 0 {
		batch = 1
	}

	first, err := s.queue.Next(ctx)
	if err != nil {
		switch {
		case stderr.Is(err, context.Canceled), stderr.Is(err, context.DeadlineExceeded):
			return nil, connect.NewError(connect.CodeCanceled, err)
		case stderr.Is(err, ErrQueueClosed):
			return nil, connect.NewError(connect.CodeUnavailable, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	out := &httpV2.HttpHandlerRequests{
		Requests: make([]*httpV2.HttpHandlerRequest, 0, batch),
	}
	out.Requests = append(out.Requests, first)

	for range batch - 1 {
		r := s.queue.TryNext()
		if r == nil {
			break
		}
		out.Requests = append(out.Requests, r)
	}
	return connect.NewResponse(out), nil
}

// StreamRequest is reserved for future server-streaming dispatch.
func (s *Server) StreamRequest(_ context.Context, _ *connect.Request[emptypb.Empty], _ *connect.ServerStream[httpV2.HttpHandlerRequest]) error {
	return connect.NewError(connect.CodeUnimplemented, stderr.New("StreamRequest is not implemented"))
}

// SendResponse delivers a worker's response to whichever producer is waiting
// for this request Id. Always returns Empty; the producer being gone is not a
// worker-visible error.
func (s *Server) SendResponse(_ context.Context, req *connect.Request[httpV2.HttpHandlerResponse]) (*connect.Response[emptypb.Empty], error) {
	id := req.Msg.GetId()
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, stderr.New("response missing Id"))
	}
	if !s.queue.Deliver(id, req.Msg) {
		s.log.Debug("response dropped: producer canceled", "id", id)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// StreamResponse is reserved for future client-streaming responses.
func (s *Server) StreamResponse(_ context.Context, _ *connect.ClientStream[httpV2.HttpHandlerResponse]) (*connect.Response[emptypb.Empty], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, stderr.New("StreamResponse is not implemented"))
}
