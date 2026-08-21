package tests

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"tests/helpers"

	"github.com/roadrunner-server/gzip/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
)

// A worker streaming a PSR response sends its chunks one by one, in order.
func TestStreamResponse(t *testing.T) {
	helpers.Start(t, "configs/.rr-stream-worker.yaml", []any{
		&server.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithTCPProbe("127.0.0.1:19993"))

	requireStream(t, "http://127.0.0.1:19993", 10)
}

// A worker failing mid-stream keeps the chunks it already sent, the response
// stays a 200 and simply ends early.
func TestStreamFail(t *testing.T) {
	helpers.Start(t, "configs/.rr-stream-fail.yaml", []any{
		&server.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithTCPProbe("127.0.0.1:19993"))

	requireStream(t, "http://127.0.0.1:19993", 2)
}

// A worker killed mid-stream leaves the client with the chunk it already sent;
// the receive error ends the stream and is logged.
func TestStreamDie(t *testing.T) {
	rr, _ := helpers.Start(t, "configs/.rr-stream-die.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithObservedLogger(), helpers.WithTCPProbe("127.0.0.1:19973"))

	requireStream(t, "http://127.0.0.1:19973", 1)

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("read stream").Len())
}

type hint struct {
	code   int
	header http.Header
}

func TestStream103(t *testing.T) {
	helpers.Start(t, "configs/.rr-stream-103.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.0"), helpers.WithTCPProbe("127.0.0.1:19983"))

	var hints []hint
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			hints = append(hints, hint{code, http.Header(header).Clone()})
			return nil
		},
	})

	header := requireStreamCtx(t, ctx, "http://127.0.0.1:19983", 10)

	require.Len(t, hints, 3)

	require.Equal(t, http.StatusContinue, hints[0].code)
	require.Equal(t, "100", hints[0].header.Get("X-100"))

	require.Equal(t, http.StatusProcessing, hints[1].code)
	require.Equal(t, "102", hints[1].header.Get("X-102"))
	require.Empty(t, hints[1].header.Get("X-100"), "an interim response must carry only its own headers")

	require.Equal(t, http.StatusEarlyHints, hints[2].code)
	require.Equal(t, "103", hints[2].header.Get("X-103"))
	require.Equal(t, "</style111.css>; rel=preload; as=style", hints[2].header.Get("Link"))

	require.Equal(t, "200", header.Get("X-200"))
	for _, k := range []string{"Link", "X-100", "X-101", "X-102", "X-103"} {
		require.Empty(t, header.Get(k), "%s leaked into the final response", k)
	}
}

// The worker writes a 10MB file on startup; two concurrent requests still get
// their response.
func TestHTTPBigResp(t *testing.T) {
	t.Cleanup(removeBigResp)

	helpers.Start(t, "configs/.rr-http-big-resp.yaml", []any{
		&server.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:15399"))

	codes := make([]int, 2)
	errs := make([]error, 2)

	wg := &sync.WaitGroup{}
	for i := range codes {
		wg.Go(func() {
			codes[i], errs[i] = getStatus(t.Context(), "http://127.0.0.1:15399")
		})
	}

	wg.Wait()

	for i := range codes {
		require.NoError(t, errs[i])
		require.Equal(t, http.StatusOK, codes[i])
	}
}

// A 2MB body against a max_request_size of 1MB is rejected.
func TestHTTPBigRespMaxReqSize(t *testing.T) {
	t.Cleanup(removeBigResp)

	helpers.Start(t, "configs/.rr-http-big-resp-max-req-size.yaml", []any{
		&server.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:16766"))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:16766",
		strings.NewReader(strings.Repeat("  ", 1024*1024)))
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

// requireStream issues a GET, requires a 200 and reads the streamed body line by
// line: the workers write the 1-based index of every chunk they send, so wantLines
// lines in that order say the chunks arrived one by one and intact.
func requireStream(t *testing.T, url string, wantLines int) {
	t.Helper()
	requireStreamCtx(t, t.Context(), url, wantLines)
}

// requireStreamCtx is requireStream with a caller-supplied context and returns
// the final response headers with the body already closed.
func requireStreamCtx(t *testing.T, ctx context.Context, url string, wantLines int) http.Header {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	idx := 0

	for {
		line, isPrefix, errR := reader.ReadLine()
		if errR != nil {
			require.ErrorIs(t, errR, io.EOF)

			break
		}

		idx++
		require.False(t, isPrefix)
		require.Equal(t, strconv.Itoa(idx), string(line))
	}

	require.Equal(t, wantLines, idx)

	return resp.Header
}

// getStatus issues a GET, drains the body and returns the status code. It takes
// no testing.T because the big-response test calls it from several goroutines.
func getStatus(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if _, err = io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}

	return resp.StatusCode, nil
}

// removeBigResp deletes the file big-resp-worker.php appends to on every start.
func removeBigResp() {
	_ = os.RemoveAll("php_test_files/big-resp")
}
