package tests

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"

	"tests/helpers"
	"tests/testLog"

	"github.com/roadrunner-server/gzip/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/roadrunner-server/http/v6/handler"
	staticPool "github.com/roadrunner-server/pool/v2/pool/static_pool"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFile is the file the upload tests post; the worker reports its metadata back.
const testFile = "uploads_test.go"

func TestHandler_Upload(t *testing.T) {
	s := helpers.ServeHandler(t, []string{"php_test_files/http/client.php", "upload", "pipes"}, nil, nil)

	for _, tt := range []struct {
		name string
		// field is the multipart field name the file is posted under
		field string
		// uploads overrides the handler uploads config; nil keeps the default
		uploads *config.Uploads
		// errNo is the PHP upload error the worker reports
		errNo int
		want  func(file string) string
	}{
		{
			name:  "temp dir accepts the file",
			field: "upload",
			errNo: 0,
			want:  flatUpload,
		},
		{
			name:  "nested field name keeps the array shape",
			field: "upload[x][y][z][]",
			errNo: 0,
			want:  nestedUpload,
		},
		{
			name:  "unwritable upload dir",
			field: "upload",
			uploads: &config.Uploads{
				Dir:       "--------",
				Forbidden: map[string]struct{}{},
				Allowed:   map[string]struct{}{".go": {}},
			},
			errNo: 6, // UPLOAD_ERR_NO_TMP_DIR
			want:  flatUpload,
		},
		{
			name:  "forbidden extension",
			field: "upload",
			uploads: &config.Uploads{
				Dir:       os.TempDir(),
				Forbidden: map[string]struct{}{".go": {}},
				Allowed:   map[string]struct{}{},
			},
			errNo: 8, // UPLOAD_ERR_EXTENSION
			want:  flatUpload,
		},
		{
			name:  "extension outside the allow list",
			field: "upload",
			uploads: &config.Uploads{
				Dir:       os.TempDir(),
				Forbidden: map[string]struct{}{},
				Allowed:   map[string]struct{}{".php": {}},
			},
			errNo: 8,
			want:  flatUpload,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			url := s.URL
			if tt.uploads != nil {
				url = serveUploads(t, s.Pool, tt.uploads)
			}

			mb, contentType := uploadBody(t, tt.field)

			code, body := sendBody(t, http.MethodPost, url, contentType, mb)

			assert.Equal(t, 200, code)
			assert.Equal(t, tt.want(fileString(testFile, tt.errNo, "application/octet-stream")), body)
		})
	}
}

// A finished multipart request leaves no upload temp files behind.
func TestHTTPMultipartFormTmpFiles(t *testing.T) {
	_, stop := helpers.Start(t, "configs/.rr-http-multipart.yaml", []any{
		&server.Plugin{},
		&gzip.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithConfigVersion("2023.3.1"), helpers.WithProbe("http://127.0.0.1:55667"))

	tmpdir := os.TempDir()
	png := path.Join(tmpdir, "test.png")

	f, err := os.Create(png)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { _ = os.Remove(png) })

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	require.NoError(t, w.WriteField("name", "John"))
	require.NoError(t, w.WriteField("age", "23"))

	fw, err := w.CreateFormFile("photo", png)
	require.NoError(t, err)

	photo, err := os.Open(png)
	require.NoError(t, err)

	_, err = io.Copy(fw, photo)
	require.NoError(t, err)
	require.NoError(t, photo.Close())
	require.NoError(t, w.Close())

	code, _ := sendBody(t, http.MethodPost, "http://127.0.0.1:55667/employee", w.FormDataContentType(), &body)
	assert.Equal(t, http.StatusOK, code)

	// the handler clears the temp copies before the request goroutine returns, so
	// stopping the container drains them
	stop()

	files, err := os.ReadDir(tmpdir)
	require.NoError(t, err)

	for _, fl := range files {
		assert.NotContains(t, fl.Name(), "uploads")
	}
}

// flatUpload is the response for a file posted under a plain field name.
func flatUpload(file string) string {
	return `{"upload":` + file + `}`
}

// nestedUpload is the response for a file posted under upload[x][y][z][].
func nestedUpload(file string) string {
	return `{"upload":{"x":{"y":{"z":[` + file + `]}}}}`
}

// serveUploads wraps the pool in a second handler with the given uploads config
// and serves it on an ephemeral port.
func serveUploads(t *testing.T, p *staticPool.Pool, uploads *config.Uploads) string {
	t.Helper()

	cfg := helpers.DefaultHandlerConfig()
	cfg.Uploads = uploads

	h, err := handler.NewHandler(cfg, p, testLog.SlogLogger())
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return srv.URL
}

// uploadBody builds a multipart body posting testFile under the given field name.
func uploadBody(t *testing.T, field string) (*bytes.Buffer, string) {
	t.Helper()

	f, err := os.Open(testFile)
	require.NoError(t, err)

	defer func() {
		_ = f.Close()
	}()

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	fw, err := w.CreateFormFile(field, f.Name())
	require.NoError(t, err)

	_, err = io.Copy(fw, f)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return &mb, w.FormDataContentType()
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
		fmt.Println(fmt.Errorf("error stat the file, error: %w", err))
	}

	ff, err := os.Open(f)
	if err != nil {
		fmt.Println(fmt.Errorf("error opening the file, error: %w", err))
	}

	defer func() {
		er := ff.Close()
		if er != nil {
			fmt.Println(fmt.Errorf("error closing the file, error: %w", er))
		}
	}()

	h := sha512.New()
	_, err = io.Copy(h, ff)
	if err != nil {
		fmt.Println(fmt.Errorf("error copying the file, error: %w", err))
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
		fmt.Println(fmt.Errorf("error marshaling fInfo, error: %w", err))
	}
	return string(r)
}
