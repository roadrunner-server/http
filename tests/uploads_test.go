package tests

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"tests/testLog"

	"github.com/goccy/go-json"
	"github.com/roadrunner-server/http/v4/config"
	"github.com/roadrunner-server/http/v4/handler"
	"github.com/roadrunner-server/sdk/v4/ipc/pipe"
	"github.com/roadrunner-server/sdk/v4/pool"
	staticPool "github.com/roadrunner-server/sdk/v4/pool/static_pool"
	"github.com/stretchr/testify/assert"
)

const testFile = "uploads_test.go"

func TestHandler_Upload_File(t *testing.T) {
	pl, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "upload", "pipes")
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

	h, err := handler.NewHandler(cfg, pl, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":9021", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", errS)
		}
	}()

	go func() {
		errL := hs.ListenAndServe()
		if errL != nil && errL != http.ErrServerClosed {
			t.Errorf("error listening the interface: error %v", errL)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f := mustOpen(testFile)
	defer func() {
		errC := f.Close()
		if errC != nil {
			t.Errorf("failed to close a file: error %v", errC)
		}
	}()
	fw, err := w.CreateFormFile("upload", f.Name())
	assert.NotNil(t, fw)
	assert.NoError(t, err)
	_, err = io.Copy(fw, f)
	if err != nil {
		t.Errorf("error copying the file: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the file: error %v", err)
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		errC := r.Body.Close()
		if errC != nil {
			t.Errorf("error closing the Body: error %v", errC)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	fs := fileString(testFile, 0, "application/octet-stream")

	assert.Equal(t, `{"upload":`+fs+`}`, string(b))
}

func TestHandler_Upload_NestedFile(t *testing.T) {
	pl, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "upload", "pipes")
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

	h, err := handler.NewHandler(cfg, pl, testLog.ZapLogger())

	assert.NoError(t, err)

	hs := &http.Server{Addr: ":9022", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", errS)
		}
	}()

	go func() {
		errL := hs.ListenAndServe()
		if errL != nil && errL != http.ErrServerClosed {
			t.Errorf("error listening the interface: error %v", errL)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f := mustOpen(testFile)
	defer func() {
		errC := hs.Close()
		if errC != nil {
			t.Errorf("failed to close a file: error %v", errC)
		}
	}()
	fw, err := w.CreateFormFile("upload[x][y][z][]", f.Name())
	assert.NotNil(t, fw)
	assert.NoError(t, err)
	_, err = io.Copy(fw, f)
	if err != nil {
		t.Errorf("error copying the file: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the file: error %v", err)
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		errC := r.Body.Close()
		if errC != nil {
			t.Errorf("error closing the Body: error %v", errC)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	fs := fileString(testFile, 0, "application/octet-stream")

	assert.Equal(t, `{"upload":{"x":{"y":{"z":[`+fs+`]}}}}`, string(b))
}

func TestHandler_Upload_File_NoTmpDir(t *testing.T) {
	pl, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "upload", "pipes")
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

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       "--------",
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{".go": {}},
		},
	}

	h, err := handler.NewHandler(cfg, pl, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":9023", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f := mustOpen(testFile)
	defer func() {
		errC := hs.Close()
		if errC != nil {
			t.Errorf("failed to close a file: error %v", errC)
		}
	}()
	fw, err := w.CreateFormFile("upload", f.Name())
	assert.NotNil(t, fw)
	assert.NoError(t, err)
	_, err = io.Copy(fw, f)
	if err != nil {
		t.Errorf("error copying the file: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the file: error %v", err)
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		errC := r.Body.Close()
		if errC != nil {
			t.Errorf("error closing the Body: error %v", errC)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	fs := fileString(testFile, 6, "application/octet-stream")

	assert.Equal(t, `{"upload":`+fs+`}`, string(b))
}

func TestHandler_Upload_File_Forbids(t *testing.T) {
	pl, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "upload", "pipes")
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

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{".go": {}},
			Allowed:   map[string]struct{}{},
		},
	}

	h, err := handler.NewHandler(cfg, pl, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":9024", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f := mustOpen(testFile)
	defer func() {
		errC := hs.Close()
		if errC != nil {
			t.Errorf("failed to close a file: error %v", errC)
		}
	}()
	fw, err := w.CreateFormFile("upload", f.Name())
	assert.NotNil(t, fw)
	assert.NoError(t, err)
	_, err = io.Copy(fw, f)
	if err != nil {
		t.Errorf("error copying the file: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the file: error %v", err)
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		errC := r.Body.Close()
		if errC != nil {
			t.Errorf("error closing the Body: error %v", errC)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	fs := fileString(testFile, 8, "application/octet-stream")

	assert.Equal(t, `{"upload":`+fs+`}`, string(b))
}

func TestHandler_Upload_File_NotAllowed(t *testing.T) {
	pl, err := staticPool.NewPool(context.Background(),
		func(cmd []string) *exec.Cmd {
			return exec.Command("php", "php_test_files/http/client.php", "upload", "pipes")
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

	cfg := &config.Config{
		MaxRequestSize:    1024,
		InternalErrorCode: 500,
		AccessLogs:        false,
		Uploads: &config.Uploads{
			Dir:       os.TempDir(),
			Forbidden: map[string]struct{}{},
			Allowed:   map[string]struct{}{".php": {}},
		},
	}

	h, err := handler.NewHandler(cfg, pl, testLog.ZapLogger())
	assert.NoError(t, err)

	hs := &http.Server{Addr: ":9024", Handler: h, ReadHeaderTimeout: time.Minute * 5}
	defer func() {
		errS := hs.Shutdown(context.Background())
		if errS != nil {
			t.Errorf("error during the shutdown: error %v", err)
		}
	}()

	go func() {
		err = hs.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("error listening the interface: error %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f := mustOpen(testFile)
	defer func() {
		errC := hs.Close()
		if errC != nil {
			t.Errorf("failed to close a file: error %v", errC)
		}
	}()
	fw, err := w.CreateFormFile("upload", f.Name())
	assert.NotNil(t, fw)
	assert.NoError(t, err)
	_, err = io.Copy(fw, f)
	if err != nil {
		t.Errorf("error copying the file: error %v", err)
	}

	err = w.Close()
	if err != nil {
		t.Errorf("error closing the file: error %v", err)
	}

	req, err := http.NewRequest("POST", "http://127.0.0.1"+hs.Addr, &mb)
	assert.NoError(t, err)

	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer func() {
		errC := r.Body.Close()
		if errC != nil {
			t.Errorf("error closing the Body: error %v", errC)
		}
	}()

	b, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)

	fs := fileString(testFile, 8, "application/octet-stream")

	assert.Equal(t, `{"upload":`+fs+`}`, string(b))
}

func mustOpen(f string) *os.File {
	r, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	return r
}

type fInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Mime   string `json:"mime"`
	Error  int    `json:"error"`
	Sha512 string `json:"sha512,omitempty"`
}

func fileString(f string, errNo int, mime string) string {
	s, err := os.Stat(f)
	if err != nil {
		fmt.Println(fmt.Errorf("error stat the file, error: %v", err))
	}

	ff, err := os.Open(f)
	if err != nil {
		fmt.Println(fmt.Errorf("error opening the file, error: %v", err))
	}

	defer func() {
		er := ff.Close()
		if er != nil {
			fmt.Println(fmt.Errorf("error closing the file, error: %v", er))
		}
	}()

	h := sha512.New()
	_, err = io.Copy(h, ff)
	if err != nil {
		fmt.Println(fmt.Errorf("error copying the file, error: %v", err))
	}

	v := &fInfo{
		Name:   s.Name(),
		Size:   s.Size(),
		Error:  errNo,
		Mime:   mime,
		Sha512: hex.EncodeToString(h.Sum(nil)),
	}

	if errNo != 0 {
		v.Sha512 = ""
		v.Size = 0
	}

	r, err := json.Marshal(v)
	if err != nil {
		fmt.Println(fmt.Errorf("error marshaling fInfo, error: %v", err))
	}
	return string(r)
}
