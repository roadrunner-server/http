package tests

import (
	"bytes"
	"context"
	"crypto/tls"
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
	mocklogger "tests/mock"

	"github.com/goccy/go-json"
	"github.com/roadrunner-server/config/v4"
	"github.com/roadrunner-server/endure/v2"
	"github.com/roadrunner-server/fileserver/v4"
	"github.com/roadrunner-server/gzip/v4"
	httpPlugin "github.com/roadrunner-server/http/v5"
	"github.com/roadrunner-server/logger/v4"
	"github.com/roadrunner-server/memory/v4"
	rpcPlugin "github.com/roadrunner-server/rpc/v4"
	"github.com/roadrunner-server/server/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestHTTPPost(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-post-test.yaml",
		Prefix:  "rr",
	}

	err := cont.RegisterAll(
		cfg,
		&rpcPlugin.Plugin{},
		&logger.Plugin{},
		&server.Plugin{},
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
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(time.Second * 1)
	t.Run("BombardWithPosts", echoHTTPPost)

	stopCh <- struct{}{}

	wg.Wait()
}

func echoHTTPPost(t *testing.T) {
	body := struct {
		Name  string `json:"name"`
		Index int    `json:"index"`
	}{
		Name:  "foo",
		Index: 111,
	}

	bd, err := json.Marshal(body)
	require.NoError(t, err)

	rdr := bytes.NewReader(bd)
	client := &http.Client{}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:10084/", rdr)
	assert.NoError(t, err)

	resp, err := client.Do(req)
	assert.NoError(t, err)

	b, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	require.True(t, bytes.Equal(bd, b))

	_ = resp.Body.Close()

	for i := 0; i < 20; i++ {
		rdr = bytes.NewReader(bd)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:10084/", rdr)
		assert.NoError(t, err)
		req.Header.Add("Content-Type", "application/json")

		resp, err := client.Do(req)
		assert.NoError(t, err)

		b, err = io.ReadAll(resp.Body)
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		require.True(t, bytes.Equal(bd, b))

		_ = resp.Body.Close()
	}
}

func TestSSLNoHTTP(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-ssl-no-http.yaml",
		Prefix:  "rr",
	}

	err := cont.RegisterAll(
		cfg,
		&rpcPlugin.Plugin{},
		&logger.Plugin{},
		&server.Plugin{},
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
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(time.Second * 1)
	t.Run("SSLEcho", sslEcho2)

	stopCh <- struct{}{}
	wg.Wait()
	time.Sleep(time.Second)
}

func sslEcho2(t *testing.T) {
	cert, err := tls.LoadX509KeyPair("test-certs/localhost+2-client.pem", "test-certs/localhost+2-client-key.pem")
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS12,
				Certificates: []tls.Certificate{cert},
			},
		},
	}

	req, err := http.NewRequest("GET", "https://127.0.0.1:4455?hello=world", nil)
	assert.NoError(t, err)

	r, err := client.Do(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	require.NoError(t, err)
	require.Equal(t, 201, r.StatusCode)
	require.Equal(t, "WORLD", string(b))

	err2 := r.Body.Close()
	if err2 != nil {
		t.Errorf("fail to close the Body: error %v", err2)
	}
}

func TestFileServer(t *testing.T) {
	cont := endure.New(slog.LevelDebug, endure.GracefulShutdownTimeout(time.Second*30))

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-static-new.yaml",
		Prefix:  "rr",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&fileserver.Plugin{},
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
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(time.Second)
	t.Run("ServeSampleEtag", serveStaticSampleEtag2)

	stopCh <- struct{}{}
	wg.Wait()
}

func serveStaticSampleEtag2(t *testing.T) {
	// OK 200 response
	b, r, err := helpers.Get("http://127.0.0.1:10101/foo/sample.txt")
	assert.NoError(t, err)
	assert.Contains(t, b, "sample")
	assert.Equal(t, r.StatusCode, http.StatusOK)
	etag := r.Header.Get("Etag")
	_ = r.Body.Close()

	// Should be 304 response with same etag
	c := http.Client{
		Timeout: time.Second * 5,
	}

	parsedURL, _ := url.Parse("http://127.0.0.1:10101/foo/sample.txt")

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

func TestHTTPBigResp(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-init-big-resp.yaml",
		Prefix:  "rr",
	}

	err := cont.RegisterAll(
		cfg,
		&gzip.Plugin{},
		&logger.Plugin{},
		&server.Plugin{},
		&memory.Plugin{},
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
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(time.Second * 5)

	wg2 := &sync.WaitGroup{}
	wg2.Add(2)
	go func() {
		defer wg2.Done()
		req, err1 := http.NewRequest(http.MethodGet, "http://127.0.0.1:15399", nil)
		require.NoError(t, err1)

		r, err1 := http.DefaultClient.Do(req)
		require.NoError(t, err1)
		require.Equal(t, 200, r.StatusCode)
		_ = r.Body.Close()
	}()

	go func() {
		defer wg2.Done()
		req, err2 := http.NewRequest(http.MethodGet, "http://127.0.0.1:15399", nil)
		require.NoError(t, err2)

		r, err2 := http.DefaultClient.Do(req)
		require.NoError(t, err2)
		require.Equal(t, 200, r.StatusCode)
		_ = r.Body.Close()
	}()

	wg2.Wait()
	stopCh <- struct{}{}
	wg.Wait()

	t.Cleanup(func() {
		_ = os.RemoveAll("php_test_files/big-resp")
	})
}

// https://github.com/laravel/octane/issues/504
func TestHTTPExecTTL(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-exec_ttl.yaml",
		Prefix:  "rr",
	}

	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)
	err := cont.RegisterAll(
		cfg,
		l,
		&server.Plugin{},
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
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(time.Second)

	req, err2 := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:18988", nil)
	require.NoError(t, err2)

	r, err2 := http.DefaultClient.Do(req)
	require.NoError(t, err2)
	require.Equal(t, 500, r.StatusCode)
	_ = r.Body.Close()

	stopCh <- struct{}{}
	wg.Wait()

	shouldBe1 := oLogger.FilterField(zapcore.Field{
		Key:       "internal_event_name",
		Type:      zapcore.StringType,
		Integer:   0,
		String:    "EventExecTTL",
		Interface: nil,
	}).Len()
	require.Equal(t, 1, shouldBe1)
}

func TestHTTPBigRespMaxReqSize(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-init-big-resp-max-req-size.yaml",
		Prefix:  "rr",
	}

	err := cont.RegisterAll(
		cfg,
		&gzip.Plugin{},
		&logger.Plugin{},
		&server.Plugin{},
		&memory.Plugin{},
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
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(time.Second)

	b2 := &bytes.Buffer{}
	for i := 0; i < 1024*1024; i++ {
		b2.Write([]byte("  "))
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1:16766", b2)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	assert.NoError(t, err)
	assert.Equal(t, 500, r.StatusCode)

	stopCh <- struct{}{}
	wg.Wait()
}
