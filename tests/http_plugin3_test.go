package tests

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/rpc"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/roadrunner-server/config/v5"
	"github.com/roadrunner-server/endure/v2"
	goridgeRpc "github.com/roadrunner-server/goridge/v3/pkg/rpc"
	"github.com/roadrunner-server/gzip/v5"
	httpPlugin "github.com/roadrunner-server/http/v5"
	"github.com/roadrunner-server/informer/v5"
	"github.com/roadrunner-server/logger/v5"
	"github.com/roadrunner-server/pool/state/process"
	rpcPlugin "github.com/roadrunner-server/rpc/v5"
	"github.com/roadrunner-server/server/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebugModeResponse(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.0",
		Path:    "configs/.rr-debugmode-fail.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	req, err := http.NewRequest("GET", "http://127.0.0.1:19995", nil)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	data, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	require.Contains(t, string(data), "Exception: test")

	assert.Equal(t, 500, r.StatusCode)

	if r.Body != nil {
		_ = r.Body.Close()
	}

	stopCh <- struct{}{}
	wg.Wait()
}

func TestStreamFail(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.0",
		Path:    "configs/.rr-stream-fail.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	req, err := http.NewRequest("GET", "http://127.0.0.1:19993", nil)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	reader := bufio.NewReader(r.Body)
	idx := 0
	for {
		line, ip, errR := reader.ReadLine()
		if errR != nil && errR == io.EOF {
			break
		}

		idx++
		assert.False(t, ip)
		assert.Equal(t, fmt.Sprintf("%d", idx), string(line))
	}

	assert.Equal(t, 2, idx)
	assert.Equal(t, 200, r.StatusCode)

	if r.Body != nil {
		_ = r.Body.Close()
	}

	stopCh <- struct{}{}
	wg.Wait()
}

func TestStreamResponse(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.0",
		Path:    "configs/.rr-stream-worker.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	req, err := http.NewRequest("GET", "http://127.0.0.1:19993", nil)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	reader := bufio.NewReader(r.Body)
	idx := 0
	for {
		line, ip, errR := reader.ReadLine()
		if errR != nil && errR == io.EOF {
			break
		}

		idx++
		assert.False(t, ip)
		assert.Equal(t, fmt.Sprintf("%d", idx), string(line))
	}

	assert.Equal(t, 10, idx)
	assert.Equal(t, 200, r.StatusCode)

	if r.Body != nil {
		_ = r.Body.Close()
	}

	stopCh <- struct{}{}
	wg.Wait()
}

func TestStream103(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.0",
		Path:    "configs/.rr-stream-103.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	req, err := http.NewRequest("GET", "http://127.0.0.1:19983", nil)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NotNil(t, r)

	idx := 0
	reader := bufio.NewReader(r.Body)
	for {
		line, ip, errR := reader.ReadLine()
		if errR != nil && errR == io.EOF {
			break
		}

		idx++
		assert.False(t, ip)
		assert.Equal(t, fmt.Sprintf("%d", idx), string(line))
	}

	assert.Equal(t, 10, idx)
	assert.Equal(t, 200, r.StatusCode)
	hdrs := map[string]string{
		"Link":  "</style111.css>; rel=preload; as=style",
		"X-100": "100",
		"X-101": "101",
		"X-102": "102",
		"X-103": "103",
		"X-200": "200",
	}

	if r.Body != nil {
		_ = r.Body.Close()
	}

	// check that all headers arrived
	for k, v := range hdrs {
		assert.Equal(t, r.Header[k][0], v)
	}

	stopCh <- struct{}{}
	wg.Wait()
}

func TestHTTPNonExistingHTTPCode(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-code.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	req, err := http.NewRequest("GET", "http://127.0.0.1:44555", nil)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NotNil(t, r)
	_, err = io.ReadAll(r.Body)
	assert.NoError(t, err)
	assert.Equal(t, 500, r.StatusCode)

	err = r.Body.Close()
	assert.NoError(t, err)

	stopCh <- struct{}{}
	wg.Wait()
}

// delete tmp files
func TestHTTPMultipartFormTmpFiles(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.1",
		Path:    "configs/.rr-http-multipart.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	tmpdir := os.TempDir()
	f, err := os.Create(path.Join(tmpdir, "test.png"))
	require.NoError(t, err)
	_ = f.Close()

	// New multipart writer.
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	fw, err := writer.CreateFormField("name")
	require.NoError(t, err)

	_, err = io.Copy(fw, strings.NewReader("John"))
	require.NoError(t, err)

	fw, err = writer.CreateFormField("age")
	require.NoError(t, err)

	_, err = io.Copy(fw, strings.NewReader("23"))
	require.NoError(t, err)

	fw, err = writer.CreateFormFile("photo", path.Join(tmpdir, "test.png"))
	require.NoError(t, err)

	file, err := os.Open(path.Join(tmpdir, "test.png"))
	require.NoError(t, err)

	_, err = io.Copy(fw, file)
	require.NoError(t, err)

	// Close multipart writer.
	_ = writer.Close()
	req, err := http.NewRequest("POST", "http://localhost:55667/employee", bytes.NewReader(body.Bytes()))
	require.NoError(t, err)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	rsp, err := client.Do(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rsp.StatusCode)

	stopCh <- struct{}{}
	wg.Wait()

	time.Sleep(time.Second)

	files, err := os.ReadDir(tmpdir)
	require.NoError(t, err)

	for _, fl := range files {
		assert.NotContains(t, fl.Name(), "uploads")
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(path.Join(tmpdir, "test.png"))
		_ = rsp.Body.Close()
	})
}

func TestMTLS1(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-mtls1.yaml",
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

	req, err := http.NewRequest("GET", "https://127.0.0.1:8895?hello=world", nil)
	assert.NoError(t, err)

	r, err := client.Do(req)
	assert.NoError(t, err)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	err2 := r.Body.Close()
	if err2 != nil {
		t.Errorf("fail to close the Body: error %v", err2)
	}

	stopCh <- struct{}{}
	wg.Wait()
}

func TestMTLS2(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-mtls2.yaml",
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

	req, err := http.NewRequest("GET", "https://127.0.0.1:8896?hello=world", nil)
	assert.NoError(t, err)

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

	r, err := client.Do(req)
	assert.NoError(t, err)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	_ = r.Body.Close()
	stopCh <- struct{}{}
	wg.Wait()
}

func TestMTLS3(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-mtls3.yaml",
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

	req, err := http.NewRequest("GET", "https://127.0.0.1:8897?hello=world", nil)
	assert.NoError(t, err)

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

	r, err := client.Do(req)
	assert.NoError(t, err)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	_ = r.Body.Close()
	stopCh <- struct{}{}
	wg.Wait()
}

func TestMTLS4(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-mtls4.yaml",
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

	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1:8898?hello=world", nil)
	assert.NoError(t, err)

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

	r, err := client.Do(req)
	assert.NoError(t, err)

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", string(b))

	_ = r.Body.Close()
	stopCh <- struct{}{}
	wg.Wait()
}

func TestMTLS5(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-mtls1.yaml",
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

	req, err := http.NewRequest("GET", "https://127.0.0.1:8895?hello=world", nil)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	_, err = client.Do(req) //nolint:bodyclose
	assert.Error(t, err)

	stopCh <- struct{}{}
	wg.Wait()
}

func TestHTTPBigURLEncoded(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-urlencoded1.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	form := url.Values{}

	// 11mb
	buf := make([]byte, 11*1024*1024)
	_, err = rand.Read(buf)
	require.NoError(t, err)

	form.Add("foo", string(buf))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:55777", strings.NewReader(form.Encode()))
	require.NoError(t, err)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(t, err)
	_, _ = io.ReadAll(resp.Body)

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	stopCh <- struct{}{}
	wg.Wait()
}

func TestHTTPBigURLEncoded2(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-urlencoded2.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	form := url.Values{}

	// 11mb
	buf := make([]byte, 11*1024*1024)
	_, err = rand.Read(buf)
	require.NoError(t, err)

	// after encode will be ~28mb
	form.Add("foo", string(buf))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:55778", strings.NewReader(form.Encode()))
	require.NoError(t, err)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(t, err)
	_, _ = io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	stopCh <- struct{}{}
	wg.Wait()
}

func TestHTTPBigURLEncoded3(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.5",
		Path:    "configs/.rr-http-urlencoded3.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&server.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	form := url.Values{}

	// 11mb
	buf := make([]byte, 10*1024*1024)
	_, err = rand.Read(buf)
	require.NoError(t, err)

	form.Add("foo", string(buf))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:55779", strings.NewReader(form.Encode()))
	require.NoError(t, err)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(t, err)

	_, _ = io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	stopCh <- struct{}{}
	wg.Wait()
}

func TestHTTPAddWorkers(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.0",
		Path:    "configs/.rr-http-workers1.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&rpcPlugin.Plugin{},
		&logger.Plugin{},
		&server.Plugin{},
		&informer.Plugin{},
		&gzip.Plugin{},
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

	time.Sleep(time.Second * 2)

	wl, err := workers("127.0.0.1:30301")
	require.NoError(t, err)
	require.Equal(t, 2, len(wl.Workers))

	err = addWorker("127.0.0.1:30301")
	require.NoError(t, err)

	wl, err = workers("127.0.0.1:30301")
	require.NoError(t, err)
	require.Equal(t, 3, len(wl.Workers))

	for i := 0; i < 3; i++ {
		err = removeWorker("127.0.0.1:30301")
		require.NoError(t, err)
	}

	wl, err = workers("127.0.0.1:30301")
	require.NoError(t, err)
	require.Equal(t, 1, len(wl.Workers))

	req, err := http.NewRequest("GET", "http://127.0.0.1:44556", nil)
	assert.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.NotNil(t, r)
	_, err = io.ReadAll(r.Body)
	_ = r.Body.Close()
	assert.NoError(t, err)
	assert.Equal(t, 500, r.StatusCode)

	go func() {
		time.Sleep(time.Second * 2)
		err2 := addWorker("127.0.0.1:30301")
		require.NoError(t, err2)
	}()

	r, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.NotNil(t, r)
	b, err := io.ReadAll(r.Body)
	assert.Equal(t, "hello world", string(b))
	_ = r.Body.Close()
	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	time.Sleep(time.Second)
	stopCh <- struct{}{}
	wg.Wait()
}

type workersList struct {
	// Workers is list of workers.
	Workers []process.State `json:"workers"`
}

func workers(address string) (*workersList, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}

	wl := &workersList{}
	client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
	err = client.Call("informer.Workers", "http", wl)
	if err != nil {
		return nil, err
	}

	return wl, nil
}

func addWorker(address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}

	res := true
	client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
	err = client.Call("informer.AddWorker", "http", &res)
	if err != nil {
		return err
	}

	return nil
}

func removeWorker(address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}

	res := true
	client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
	err = client.Call("informer.RemoveWorker", "http", &res)
	if err != nil {
		return err
	}

	return nil
}

func reset(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial("tcp", address)
		assert.NoError(t, err)
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))

		var ret bool
		err = client.Call("resetter.Reset", "http", &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}
