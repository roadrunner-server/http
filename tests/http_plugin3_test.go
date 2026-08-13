package tests

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	"github.com/roadrunner-server/gzip/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/logger/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
