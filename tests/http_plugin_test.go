package tests

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"tests/helpers"

	"github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	"github.com/roadrunner-server/gzip/v6"
	"github.com/roadrunner-server/headers/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/logger/v6"
	"github.com/roadrunner-server/send/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/roadrunner-server/static/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPXSendFile(t *testing.T) {
	cont := endure.New(slog.LevelDebug, endure.GracefulShutdownTimeout(time.Minute))

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-sendfile.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&send.Plugin{},
		&httpPlugin.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-stopCh:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	})

	time.Sleep(time.Second * 2)
	t.Run("X-Sendfile", xsendfile)
	stopCh <- struct{}{}
	wg.Wait()
}

func xsendfile(t *testing.T) {
	parsedURL, _ := url.Parse("http://127.0.0.1:41134")
	client := http.Client{}
	pwd, _ := os.Getwd()
	req := &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	b, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	require.True(t, len(b) > 0)
	require.Empty(t, resp.Header.Get("X-Sendfile"))

	file, err := os.ReadFile(fmt.Sprintf("%s/php_test_files/well", pwd))
	require.NoError(t, err)
	assert.True(t, len(b) == len(file))
	require.NoError(t, resp.Body.Close())
	_, _ = io.Discard.Write(file)
}

func TestStaticEtagPlugin(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-static.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-stopCh:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	})

	time.Sleep(time.Second)
	t.Run("ServeSampleEtag", serveStaticSampleEtag)
	t.Run("NoStaticHeaders", noStaticHeaders)

	stopCh <- struct{}{}
	wg.Wait()
}

func serveStaticSampleEtag(t *testing.T) {
	// OK 200 response
	b, r, err := helpers.Get("http://127.0.0.1:21603/sample.txt")
	assert.NoError(t, err)
	assert.Contains(t, b, "sample")
	assert.Equal(t, r.StatusCode, http.StatusOK)
	etag := r.Header.Get("Etag")

	_ = r.Body.Close()

	// Should be 304 response with same etag
	c := http.Client{
		Timeout: time.Second * 5,
	}

	parsedURL, _ := url.Parse("http://127.0.0.1:21603/sample.txt")

	req := &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
		Header: map[string][]string{"If-None-Match": {etag}},
	}

	resp, err := c.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusNotModified, resp.StatusCode)
	_ = resp.Body.Close()
}

// regular request should not contain static headers
func noStaticHeaders(t *testing.T) {
	// OK 200 response
	_, r, err := helpers.Get("http://127.0.0.1:21603")
	assert.NoError(t, err)
	assert.NotContains(t, r.Header["Input"], "custom-header")
	assert.NotContains(t, r.Header["Output"], "output-header")
	assert.Equal(t, r.StatusCode, http.StatusOK)

	_ = r.Body.Close()
}

func TestStaticPluginSecurity(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-static-security.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-stopCh:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	})

	time.Sleep(time.Second)
	t.Run("ServeSampleNotAllowedPath", serveStaticSampleNotAllowedPath)

	stopCh <- struct{}{}
	wg.Wait()
}

func serveStaticSampleNotAllowedPath(t *testing.T) {
	// Should be 304 response with same etag
	c := http.Client{
		Timeout: time.Second * 5,
	}

	parsedURL := &url.URL{
		Scheme: "http",
		User:   nil,
		Host:   "127.0.0.1:21603",
		Path:   "%2e%2e%/tests/",
	}

	req := &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

	resp, err := c.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	parsedURL = &url.URL{
		Scheme: "http",
		User:   nil,
		Host:   "127.0.0.1:21603",
		Path:   "%2e%2e%5ctests/",
	}

	req = &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

	resp, err = c.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	parsedURL = &url.URL{
		Scheme: "http",
		User:   nil,
		Host:   "127.0.0.1:21603",
		Path:   "..%2ftests/",
	}

	req = &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

	resp, err = c.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	parsedURL = &url.URL{
		Scheme: "http",
		User:   nil,
		Host:   "127.0.0.1:21603",
		Path:   "%2e%2e%2ftests/",
	}

	req = &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

	resp, err = c.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	_, r, err := helpers.Get("http://127.0.0.1:21603/../../sample.txt")
	assert.NoError(t, err)
	assert.Equal(t, 403, r.StatusCode)
	_ = r.Body.Close()
}

func TestStaticBigFilePlugin(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.1.5",
		Path:    "configs/.rr-http-static-big-file.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&headers.Plugin{},
		&send.Plugin{},
		&httpPlugin.Plugin{},
		&static.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-stopCh:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	})

	time.Sleep(time.Second)
	t.Run("ServeSample", serveStaticSample(21604, "sample-big.txt"))
	t.Run("StaticNotForbid", staticNotForbid(21604))
	t.Run("StaticHeaders", staticHeaders(21604))

	stopCh <- struct{}{}
	wg.Wait()
}

func TestStaticPlugin(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-static.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-stopCh:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	})

	time.Sleep(time.Second)
	t.Run("ServeSample", serveStaticSample(21603, "sample.txt"))
	t.Run("StaticNotForbid", staticNotForbid(21603))
	t.Run("StaticHeaders", staticHeaders(21603))

	stopCh <- struct{}{}
	wg.Wait()
}

func staticHeaders(port int) func(t *testing.T) {
	return func(t *testing.T) {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/php_test_files/client.php", port), nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		if resp.Header.Get("Output") != "output-header" {
			t.Fatal("can't find output header in response")
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		want, err := os.ReadFile("php_test_files/client.php")
		require.NoError(t, err)
		require.Equal(t, string(want), string(b))
	}
}

func staticNotForbid(port int) func(t *testing.T) {
	return func(t *testing.T) {
		b, r, err := helpers.Get(fmt.Sprintf("http://127.0.0.1:%d/php_test_files/client.php", port))
		require.NoError(t, err)
		want, err := os.ReadFile("php_test_files/client.php")
		require.NoError(t, err)
		require.Equal(t, string(want), b)
		_ = r.Body.Close()
	}
}

func serveStaticSample(port int, filename string) func(t *testing.T) {
	return func(t *testing.T) {
		b, r, err := helpers.Get(fmt.Sprintf("http://127.0.0.1:%d/%s", port, filename))
		require.NoError(t, err)
		require.Contains(t, b, "sample")
		_ = r.Body.Close()
	}
}

func TestStaticDisabled_Error(t *testing.T) {
	_ = helpers.StartExpectInitError(t, "configs/.rr-http-static-disabled.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	})
}

func TestStaticFilesDisabled(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-static-files-disable.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	stopCh := make(chan struct{}, 1)

	wg.Go(func() {
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-stopCh:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	})

	time.Sleep(time.Second)
	t.Run("StaticFilesDisabled", staticFilesDisabled)

	stopCh <- struct{}{}
	wg.Wait()
}

func staticFilesDisabled(t *testing.T) {
	b, r, err := helpers.Get("http://127.0.0.1:45877/php_test_files/client.php?hello=world")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "WORLD", b)
	_ = r.Body.Close()
}

func TestStaticFilesForbid(t *testing.T) {
	// A TCP probe instead of a request one: a probe request would add an "http log" record.
	rr, stop := helpers.Start(t, "configs/.rr-http-static-files.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&gzip.Plugin{},
		&static.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithTCPProbe("127.0.0.1:34653"))

	t.Run("StaticTestFilesDir", staticTestFilesDir)
	t.Run("StaticNotFound", staticNotFound)
	t.Run("StaticFilesForbid", staticFilesForbid)

	stop()

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("http server was started").Len())
	require.Equal(t, 3, rr.Logs.FilterMessageSnippet("http log").Len())
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("file extension is forbidden").Len())
}

func staticTestFilesDir(t *testing.T) {
	b, r, err := helpers.Get("http://127.0.0.1:34653/http?hello=world")
	assert.NoError(t, err)
	assert.Equal(t, "WORLD", b)
	_ = r.Body.Close()
}

func staticNotFound(t *testing.T) {
	b, _, _ := helpers.Get("http://127.0.0.1:34653/client.XXX?hello=world") //nolint:bodyclose
	assert.Equal(t, "WORLD", b)
}

func staticFilesForbid(t *testing.T) {
	b, r, err := helpers.Get("http://127.0.0.1:34653/client.php?hello=world")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "WORLD", b)
	_ = r.Body.Close()
}
