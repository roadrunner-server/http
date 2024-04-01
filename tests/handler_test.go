package tests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"tests/helpers"
	"tests/testLog"

	"github.com/goccy/go-json"
	"github.com/roadrunner-server/http/v4/config"
	"github.com/roadrunner-server/http/v4/handler"
	"github.com/roadrunner-server/sdk/v4/ipc/pipe"
	"github.com/roadrunner-server/sdk/v4/pool"
	staticPool "github.com/roadrunner-server/sdk/v4/pool/static_pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Echo(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "echo", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	require.NoError(t, err)

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{
		Addr:              ":19177",
		ReadHeaderTimeout: time.Minute * 5,
		Handler:           h,
	}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()
	go func(server *http.Server) {
		err = server.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}(hs)
	time.Sleep(time.Millisecond * 10)

	body, r, err := helpers.Get("http://127.0.0.1:19177/?hello=world")
	assert.NoError(t, err)
	defer func() {
		_ = r.Body.Close()
	}()
	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", body)
	p.Destroy(context.Background())
}

func TestHandler_Headers(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "header", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{
		Addr:              ":8078",
		ReadHeaderTimeout: time.Minute * 5,
		Handler:           h,
	}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 100)

	req, err := http.NewRequest("GET", "http://127.0.0.1:8078?hello=world", nil)
	assert.NoError(t, err)

	req.Header.Add("input", "sample")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "world", r.Header.Get("Header"))
	assert.Equal(t, "SAMPLE", string(b))
}

func TestHandler_Empty_User_Agent(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "user-agent", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{
		Addr:              ":19658",
		Handler:           h,
		ReadHeaderTimeout: time.Minute * 5,
	}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	req, err := http.NewRequest("GET", "http://127.0.0.1:19658?hello=world", nil)
	assert.NoError(t, err)

	req.Header.Add("user-agent", "")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "", string(b))
}

func TestHandler_User_Agent(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "user-agent", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{
		Addr:              ":25688",
		Handler:           h,
		ReadHeaderTimeout: time.Minute * 5,
	}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	req, err := http.NewRequest("GET", "http://127.0.0.1:25688?hello=world", nil)
	assert.NoError(t, err)

	req.Header.Add("User-Agent", "go-agent")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "go-agent", string(b))
}

func TestHandler_Cookies(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "cookie", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8079", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8079", nil)
	assert.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "input", Value: "input-value"})

	r, err := client.Do(req)
	assert.NoError(t, err)

	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "INPUT-VALUE", string(b))

	for _, c := range r.Cookies() {
		assert.Equal(t, "output", c.Name)
		assert.Equal(t, "cookie-output", c.Value)
	}
}

func TestHandler_JsonPayload_POST(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "payload", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8090", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	req, err := http.NewRequest(
		"POST",
		"http://127.0.0.1"+hs.Addr, //nolint:goconst
		bytes.NewBufferString(`{"key":"value"}`),
	)
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/json")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, `{"value":"key"}`, string(b))
}

func TestHandler_JsonPayload_PUT(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "payload", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8081", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	req, err := http.NewRequest("PUT", "http://127.0.0.1"+hs.Addr, bytes.NewBufferString(`{"key":"value"}`))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/json")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, `{"value":"key"}`, string(b))
}

func TestHandler_JsonPayload_PATCH(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "payload", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8082", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	req, err := http.NewRequest("PATCH", "http://127.0.0.1"+hs.Addr, bytes.NewBufferString(`{"key":"value"}`))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/json")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, `{"value":"key"}`, string(b))
}

func TestHandler_UrlEncoded_POST_DELETE(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/psr-worker-echo.php")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		RawBody:           true,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":10084", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1"+hs.Addr, strings.NewReader("arr[x][y][e]=f&arr[c]p=l&arr[c]z=&key=value&name[]=name1&name[]=name2&name[]=name3&arr[x][y][z]=y"))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "arr[x][y][e]=f&arr[c]p=l&arr[c]z=&key=value&name[]=name1&name[]=name2&name[]=name3&arr[x][y][z]=y", string(b))

	req, err = http.NewRequest(http.MethodDelete, "http://127.0.0.1"+hs.Addr, strings.NewReader("arr[x][y][e]=f&arr[c]p=l&arr[c]z=&key=value&name[]=name1&name[]=name2&name[]=name3&arr[x][y][z]=y"))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err = io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "arr[x][y][e]=f&arr[c]p=l&arr[c]z=&key=value&name[]=name1&name[]=name2&name[]=name3&arr[x][y][z]=y", string(b))
}

func TestHandler_FormData_POST(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":10084", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)

	form := url.Values{}

	form.Add("key", "value")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, strings.NewReader(form.Encode()))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_FormData_POST_Overwrite(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8083", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	form := url.Values{}

	form.Add("key", "value")
	form.Add("key", "value2")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, strings.NewReader(form.Encode()))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value2")
	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})

	t.Cleanup(func() {
		_ = hs.Shutdown(context.Background())
	})
}

func TestHandler_FormData_POST_Form_UrlEncoded_Charset(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8085", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	form := url.Values{}

	form.Add("key", "value")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, strings.NewReader(form.Encode()))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_FormData_PUT(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":17834", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)

	form := url.Values{}

	form.Add("key", "value")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")

	req, err := http.NewRequest("PUT", "http://127.0.0.1"+hs.Addr, strings.NewReader(form.Encode()))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)

	// `{"arr":{"c":{"p":"l","z":""},"x":{"y":{"e":"f","z":"y"}}},"key":"value","name":["name1","name2","name3"]}`
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_FormData_PATCH(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8086", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	form := url.Values{}

	form.Add("key", "value")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")

	req, err := http.NewRequest("PATCH", "http://127.0.0.1"+hs.Addr, strings.NewReader(form.Encode()))
	assert.NoError(t, err)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_Multipart_POST(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8019", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)
	err = w.WriteField("key", "value")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("key", "value")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name1")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name2")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name3")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[x][y][z]", "y")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[x][y][e]", "f")

	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[c]p", "l")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[c]z", "")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the writer: error %v", err)
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_Multipart_PUT(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8020", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)
	err = w.WriteField("key", "value")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("key", "value")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name1")

	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name2")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name3")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[x][y][z]", "y")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[x][y][e]", "f")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[c]p", "l")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[c]z", "")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the writer: error %v", err)
	}

	req, err := http.NewRequest("PUT", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_Multipart_PATCH(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "data", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":34432", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()

		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)
	err = w.WriteField("key", "value")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("key", "value")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name1")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name2")

	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("name[]", "name3")

	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[x][y][z]", "y")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[x][y][e]", "f")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[c]p", "l")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.WriteField("arr[c]z", "")
	if err != nil {
		t.Errorf("error writing the field: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the writer: error %v", err)
	}

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPatch, "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := client.Do(req)
	assert.NoError(t, err)

	defer func() {
		err = r.Body.Close()
		if err != nil {
			t.Errorf("error during the closing Body: error %v", err)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	var res map[string]any
	err = json.Unmarshal(b, &res)
	require.NoError(t, err)
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["p"], "l")
	assert.Equal(t, res["arr"].(map[string]any)["c"].(map[string]any)["z"], "")

	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["z"], "y")
	assert.Equal(t, res["arr"].(map[string]any)["x"].(map[string]any)["y"].(map[string]any)["e"], "f")

	assert.Equal(t, res["key"], "value")

	assert.Equal(t, res["name"], []any{"name1", "name2", "name3"})
}

func TestHandler_Error(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "error", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8177", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	_, r, err := helpers.Get("http://127.0.0.1:8177/?hello=world")
	assert.NoError(t, err)
	defer func() {
		_ = r.Body.Close()
	}()
	assert.Equal(t, 500, r.StatusCode)
}

func TestHandler_Error2(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "error2", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8178", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	_, r, err := helpers.Get("http://127.0.0.1:8178/?hello=world")
	assert.NoError(t, err)
	defer func() {
		_ = r.Body.Close()
	}()
	assert.Equal(t, 500, r.StatusCode)
}

func TestHandler_ResponseDuration(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "echo", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8180", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	body, r, err := helpers.Get("http://127.0.0.1:8180/?hello=world")
	assert.NoError(t, err)
	defer func() {
		_ = r.Body.Close()
	}()

	assert.Equal(t, 201, r.StatusCode)
	assert.Equal(t, "WORLD", body)
}

func TestHandler_ResponseDurationDelayed(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "echoDelay", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8181", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()
}

func TestHandler_ErrorDuration(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "error", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":8182", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	_, r, err := helpers.Get("http://127.0.0.1:8182/?hello=world")
	assert.NoError(t, err)
	defer func() {
		_ = r.Body.Close()
	}()

	assert.Equal(t, 500, r.StatusCode)
}

func TestHandler_IP(t *testing.T) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "ip", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      1,
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: "127.0.0.1:8183", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	body, r, err := helpers.Get("http://127.0.0.1:8183/")
	assert.NoError(t, err)
	defer func() {
		_ = r.Body.Close()
	}()
	assert.Equal(t, 200, r.StatusCode)
	assert.Equal(t, "127.0.0.1", body)
}

func BenchmarkHandler_Listen_Echo(b *testing.B) {
	p, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "echo", "pipes")
		},
		pipe.NewPipeFactory(testLog.ZapLogger()),
		&pool.Config{
			NumWorkers:      uint64(runtime.NumCPU()),
			AllocateTimeout: time.Second * 1000,
			DestroyTimeout:  time.Second * 1000,
		}, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		p.Destroy(context.Background())
	}()

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, p, testLog.ZapLogger())
	assert.NoError(b, err)

	hs := &http.Server{Addr: ":8188", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			b.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			b.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	b.ResetTimer()
	b.ReportAllocs()
	bb := "WORLD"

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:8188/?hello=world", nil)
	require.NoError(b, err)
	client := &http.Client{}

	for n := 0; n < b.N; n++ {
		r, err := client.Do(req)
		require.NoError(b, err)
		// Response might be nil here
		if r != nil {
			br, err := io.ReadAll(r.Body)
			if err != nil {
				b.Errorf("error reading Body: error %v", err)
			}
			if string(br) != bb {
				b.Fail()
			}
			err = r.Body.Close()
			if err != nil {
				b.Errorf("error closing the Body: error %v", err)
			}
		} else {
			b.Errorf("got nil response")
		}
	}
}
