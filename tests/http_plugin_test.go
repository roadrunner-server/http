package tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"tests/helpers"
	mocklogger "tests/mock"

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
	"github.com/yookoala/gofast"
	"golang.org/x/net/http2"
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

func TestSSL(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-ssl.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&send.Plugin{},
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

	time.Sleep(time.Second * 1)
	t.Run("SSLEcho", sslEcho)
	t.Run("SSLNoRedirect", sslNoRedirect)
	t.Run("FCGEcho", fcgiEcho)

	stopCh <- struct{}{}
	wg.Wait()
}

func sslNoRedirect(t *testing.T) {
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

	req, err := http.NewRequest("GET", "http://127.0.0.1:8085?hello=world", nil)
	assert.NoError(t, err)

	r, err := client.Do(req)
	assert.NoError(t, err)

	assert.Nil(t, r.TLS)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	err2 := r.Body.Close()
	if err2 != nil {
		t.Errorf("fail to close the Body: error %v", err2)
	}
}

func sslEcho(t *testing.T) {
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

	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1:8893?hello=world", nil) //nolint:noctx
	assert.NoError(t, err)

	r, err := client.Do(req)
	assert.NoError(t, err)
	require.NotNil(t, r)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	err2 := r.Body.Close()
	if err2 != nil {
		t.Errorf("fail to close the Body: error %v", err2)
	}
}

func fcgiEcho(t *testing.T) {
	fcgiConnFactory := gofast.SimpleConnFactory("tcp", "127.0.0.1:16920")

	fcgiHandler := gofast.NewHandler(
		gofast.BasicParamsMap(gofast.BasicSession),
		gofast.SimpleClientFactory(fcgiConnFactory),
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "http://site.local/?hello=world", nil)
	fcgiHandler.ServeHTTP(w, req)

	body, err := io.ReadAll(w.Result().Body) //nolint:bodyclose

	defer func() {
		_ = w.Result().Body.Close()
		w.Body.Reset()
	}()

	assert.NoError(t, err)
	assert.Equal(t, 201, w.Result().StatusCode) //nolint:bodyclose
	assert.Equal(t, "WORLD", string(body))
}

func TestSSLRedirect(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-ssl-redirect.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&send.Plugin{},
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

	time.Sleep(time.Second * 1)
	t.Run("SSLRedirect", sslRedirect)

	stopCh <- struct{}{}
	wg.Wait()
}

func sslRedirect(t *testing.T) {
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

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8087?hello=world", nil)
	assert.NoError(t, err)

	r, err := client.Do(req)
	assert.NoError(t, err)
	require.NotNil(t, r)
	require.NotNil(t, r.TLS)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	err2 := r.Body.Close()
	if err2 != nil {
		t.Errorf("fail to close the Body: error %v", err2)
	}
}

func TestSSLPushPipes(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-ssl-push.yaml",
	}

	err := cont.RegisterAll(
		cfg,
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

	time.Sleep(time.Second * 1)
	t.Run("SSLPush", sslPush)

	stopCh <- struct{}{}
	wg.Wait()
}

func sslPush(t *testing.T) {
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

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1:8894?hello=world", nil)
	assert.NoError(t, err)

	r, err := client.Do(req)
	assert.NoError(t, err)
	require.NotNil(t, r)

	assert.NotNil(t, r.TLS)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.Equal(t, "", r.Header.Get("Http2-Release"))

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	err2 := r.Body.Close()
	if err2 != nil {
		t.Errorf("fail to close the Body: error %v", err2)
	}
}

func TestFastCGI_Echo(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-fcgi.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
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

	time.Sleep(time.Second * 1)
	t.Run("FastCGIEcho", fcgiEcho1)

	stopCh <- struct{}{}
	wg.Wait()
}

func fcgiEcho1(t *testing.T) {
	time.Sleep(time.Second * 2)
	fcgiConnFactory := gofast.SimpleConnFactory("tcp", "127.0.0.1:6920")

	fcgiHandler := gofast.NewHandler(
		gofast.BasicParamsMap(gofast.BasicSession),
		gofast.SimpleClientFactory(fcgiConnFactory),
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "http://site.local/hello-world", nil)
	fcgiHandler.ServeHTTP(w, req)

	_, err := io.ReadAll(w.Result().Body) //nolint:bodyclose
	assert.NoError(t, err)
	assert.Equal(t, 200, w.Result().StatusCode) //nolint:bodyclose
}

func TestFastCGI_EchoUnix(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-fcgi-unix.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
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

	time.Sleep(time.Second * 1)
	t.Run("FastCGIEcho", fcgiEchoUnix)

	stopCh <- struct{}{}
	wg.Wait()

	t.Cleanup(func() {
		_ = os.RemoveAll("rr.sock")
	})
}

func fcgiEchoUnix(t *testing.T) {
	time.Sleep(time.Second * 2)
	fcgiConnFactory := gofast.SimpleConnFactory("unix", "rr.sock")

	fcgiHandler := gofast.NewHandler(
		gofast.BasicParamsMap(gofast.BasicSession),
		gofast.SimpleClientFactory(fcgiConnFactory),
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "http://site.local/hello-world", nil)
	fcgiHandler.ServeHTTP(w, req)

	_, err := io.ReadAll(w.Result().Body) //nolint:bodyclose
	assert.NoError(t, err)
	assert.Equal(t, 200, w.Result().StatusCode) //nolint:bodyclose
}

func TestFastCGI_RequestUri(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-fcgi-request-uri.yaml",
	}

	err := cont.RegisterAll(
		cfg,
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

	time.Sleep(time.Second * 1)
	t.Run("FastCGIServiceRequestUri", fcgiReqURI)

	stopCh <- struct{}{}
	wg.Wait()
}

func fcgiReqURI(t *testing.T) {
	time.Sleep(time.Second * 2)
	fcgiConnFactory := gofast.SimpleConnFactory("tcp", "127.0.0.1:6921")

	fcgiHandler := gofast.NewHandler(
		gofast.BasicParamsMap(gofast.BasicSession),
		gofast.SimpleClientFactory(fcgiConnFactory),
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "http://site.local/hello-world", nil)
	fcgiHandler.ServeHTTP(w, req)

	body, err := io.ReadAll(w.Result().Body) //nolint:bodyclose
	assert.NoError(t, err)
	assert.Equal(t, 200, w.Result().StatusCode) //nolint:bodyclose
	assert.Contains(t, string(body), "ddddd")
}

func TestHTTP2Req(t *testing.T) {
	cont := endure.New(slog.LevelDebug, endure.GracefulShutdownTimeout(time.Second*5))

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-h2-ssl.yaml",
	}

	l, oLogger := mocklogger.SlogTestLogger(slog.LevelDebug)
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

	time.Sleep(time.Second * 1)

	cert, err := tls.LoadX509KeyPair("test-certs/localhost+2-client.pem", "test-certs/localhost+2-client-key.pem")
	require.NoError(t, err)

	tr := &http2.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	client := &http.Client{
		Transport:     tr,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}

	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1:23452?hello=world", nil) //nolint:noctx
	require.NoError(t, err)

	r, err := client.Do(req)
	require.NoError(t, err)

	assert.Equal(t, r.StatusCode, http.StatusCreated)
	data, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	require.Equal(t, data, []byte("WORLD"))
	require.NoError(t, r.Body.Close())

	stopCh <- struct{}{}
	wg.Wait()

	require.Equal(t, 1, oLogger.FilterMessageSnippet("http server was started").Len())
	require.Equal(t, 1, oLogger.FilterMessageSnippet("http log").Len())
}

func TestH2CUpgrade(t *testing.T) {
	cont := endure.New(slog.LevelDebug, endure.GracefulShutdownTimeout(time.Second*5))

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-h2c.yaml",
	}

	l, oLogger := mocklogger.SlogTestLogger(slog.LevelDebug)
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

	time.Sleep(time.Second * 1)
	client := &http.Client{}

	req, err := http.NewRequestWithContext(t.Context(), "PRI", "http://127.0.0.1:8083", nil)
	assert.NoError(t, err)

	req.Header.Add("Upgrade", "h2c")
	req.Header.Add("Connection", "HTTP2-Settings")
	req.Header.Add("Connection", "Upgrade")
	req.Header.Add("HTTP2-Settings", "AAMAAABkAARAAAAAAAIAAAAA")

	r, err := client.Do(req)
	require.NoError(t, err)

	// h2c is prior-knowledge only; an upgrade request is served as plain HTTP/1.1
	assert.Equal(t, "201 Created", r.Status)
	require.NoError(t, r.Body.Close())

	assert.Equal(t, http.StatusCreated, r.StatusCode)

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8083?hello=world", nil)
	assert.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 1, resp.ProtoMajor)
	_ = resp.Body.Close()

	time.Sleep(time.Second * 2)
	stopCh <- struct{}{}
	wg.Wait()

	require.Equal(t, 1, oLogger.FilterMessageSnippet("http server was started").Len())
}

func TestH2C(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-h2c.yaml",
	}

	l, oLogger := mocklogger.SlogTestLogger(slog.LevelDebug)
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

	tr := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			// use the http dial (w/o tls)
			return net.Dial(network, addr)
		},
	}
	client := &http.Client{
		Transport: tr,
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8083?hello=world", nil) //nolint:noctx
	require.NoError(t, err)

	r, err := client.Do(req)
	require.NoError(t, err)

	assert.Equal(t, "201 Created", r.Status)
	data, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	require.Equal(t, []byte("WORLD"), data)

	require.Equal(t, 2, r.ProtoMajor)
	require.NoError(t, r.Body.Close())

	stopCh <- struct{}{}
	wg.Wait()

	require.Equal(t, 1, oLogger.FilterMessageSnippet("http server was started").Len())
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
