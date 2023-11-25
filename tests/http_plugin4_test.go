package tests

import (
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

	"github.com/quic-go/quic-go/http3"
	"github.com/roadrunner-server/config/v4"
	"github.com/roadrunner-server/endure/v2"
	httpPlugin "github.com/roadrunner-server/http/v4"
	"github.com/roadrunner-server/logger/v4"
	rpcPlugin "github.com/roadrunner-server/rpc/v4"
	"github.com/roadrunner-server/server/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttp3(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2023.3.0",
		Path:    "configs/.rr-http3.yaml",
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
	t.Run("response", http3ResponseMatcher)

	stopCh <- struct{}{}
	wg.Wait()
}

func http3ResponseMatcher(t *testing.T) {
	cert, err := tls.LoadX509KeyPair("test-certs/localhost+2-client.pem", "test-certs/localhost+2-client-key.pem")
	require.NoError(t, err)

	roundTripper := &http3.RoundTripper{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			InsecureSkipVerify: true,
		},
	}
	defer roundTripper.Close()
	client := &http.Client{
		Transport: roundTripper,
	}

	parsedURL, _ := url.Parse("https://127.0.0.1:8081?hello=world")
	req := &http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

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
}
