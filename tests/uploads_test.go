package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"testing"

	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
	"github.com/roadrunner-server/http/v6/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tests/helpers"
)

const testFile = "uploads_test.go"

// uploadOut is the response shape that upload.php produced — preserving JSON
// field order so tests can string-compare against fileString().
type uploadOut struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Mime   string `json:"mime"`
	Error  int    `json:"error"`
	Sha512 string `json:"sha512,omitempty"`
}

// transformUploadLeaf converts a single FileUpload-shaped JSON object into
// upload.php's response shape: drop tmpName, add sha512 of the temp file
// contents on success, blank size + sha512 on error.
func transformUploadLeaf(file map[string]any) *uploadOut {
	out := &uploadOut{}
	out.Name, _ = file["name"].(string)
	out.Mime, _ = file["mime"].(string)
	if s, ok := file["size"].(float64); ok {
		out.Size = int64(s)
	}
	if e, ok := file["error"].(float64); ok {
		out.Error = int(e)
	}
	if out.Error == 0 {
		if tmp, _ := file["tmpName"].(string); tmp != "" {
			out.Sha512, _ = helpers.HashFileSHA512(tmp)
		}
	} else {
		out.Size = 0
	}
	return out
}

// walkUploads descends into the upload tree from req.Uploads, replacing each
// FileUpload-shaped leaf (anything with a "tmpName" key) with an uploadOut,
// preserving the branch structure.
func walkUploads(v any) any {
	switch val := v.(type) {
	case map[string]any:
		if _, isLeaf := val["tmpName"]; isLeaf {
			return transformUploadLeaf(val)
		}
		out := make(map[string]any, len(val))
		for k, sub := range val {
			out[k] = walkUploads(sub)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, sub := range val {
			out[i] = walkUploads(sub)
		}
		return out
	default:
		return val
	}
}

func uploadResponder(r *httpV2.HttpHandlerRequest) *httpV2.HttpHandlerResponse {
	tree, _ := helpers.DecodeUploadsTree(r.GetUploads())
	body, _ := json.Marshal(walkUploads(tree))
	return helpers.MakeResp(200, body, nil)
}

// uploadCfg returns a base Config and lets the caller override the Uploads
// allow/forbid policy.
func uploadCfg(dir string, forbid, allow map[string]struct{}) *config.Config {
	c := defaultCfg()
	c.Uploads.Dir = dir
	c.Uploads.Forbidden = forbid
	c.Uploads.Allowed = allow
	return c
}

func TestHandler_Upload_File(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:9021", defaultCfg(), uploadResponder)
	defer env.close(t)

	body := doMultipartUpload(t, "http://127.0.0.1:9021", "upload")
	fs := fileString(t, 0)
	assert.Equal(t, `{"upload":`+fs+`}`, body)
}

func TestHandler_Upload_NestedFile(t *testing.T) {
	env := newHandlerEnv(t, "127.0.0.1:9022", defaultCfg(), uploadResponder)
	defer env.close(t)

	body := doMultipartUpload(t, "http://127.0.0.1:9022", "upload[x][y][z][]")
	fs := fileString(t, 0)
	assert.Equal(t, `{"upload":{"x":{"y":{"z":[`+fs+`]}}}}`, body)
}

func TestHandler_Upload_File_NoTmpDir(t *testing.T) {
	cfg := uploadCfg("--------", map[string]struct{}{}, map[string]struct{}{".go": {}})

	env := newHandlerEnv(t, "127.0.0.1:9023", cfg, uploadResponder)
	defer env.close(t)

	body := doMultipartUpload(t, "http://127.0.0.1:9023", "upload")
	fs := fileString(t, 6) // UploadErrorNoTmpDir
	assert.Equal(t, `{"upload":`+fs+`}`, body)
}

func TestHandler_Upload_File_Forbids(t *testing.T) {
	cfg := uploadCfg(os.TempDir(), map[string]struct{}{".go": {}}, map[string]struct{}{})

	env := newHandlerEnv(t, "127.0.0.1:9024", cfg, uploadResponder)
	defer env.close(t)

	body := doMultipartUpload(t, "http://127.0.0.1:9024", "upload")
	fs := fileString(t, 8) // UploadErrorExtension
	assert.Equal(t, `{"upload":`+fs+`}`, body)
}

func TestHandler_Upload_File_NotAllowed(t *testing.T) {
	cfg := uploadCfg(os.TempDir(), map[string]struct{}{}, map[string]struct{}{".php": {}})

	env := newHandlerEnv(t, "127.0.0.1:9025", cfg, uploadResponder)
	defer env.close(t)

	body := doMultipartUpload(t, "http://127.0.0.1:9025", "upload")
	fs := fileString(t, 8) // UploadErrorExtension
	assert.Equal(t, `{"upload":`+fs+`}`, body)
}

func doMultipartUpload(t *testing.T, urlStr, fieldName string) string {
	t.Helper()

	var mb bytes.Buffer
	w := multipart.NewWriter(&mb)

	f, err := os.Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fw, err := w.CreateFormFile(fieldName, f.Name())
	require.NoError(t, err)
	_, err = io.Copy(fw, f)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, urlStr, &mb)
	require.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()

	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, r.StatusCode)
	return string(body)
}

// fileString builds the expected JSON for the testFile leaf so each test's
// assertion can string-compare against the responder's output.
func fileString(t *testing.T, errNo int) string {
	t.Helper()
	stat, err := os.Stat(testFile)
	require.NoError(t, err)

	v := &uploadOut{
		Name:  stat.Name(),
		Size:  stat.Size(),
		Mime:  "application/octet-stream",
		Error: errNo,
	}
	if errNo == 0 {
		v.Sha512, err = helpers.HashFileSHA512(testFile)
		require.NoError(t, err)
	} else {
		v.Size = 0
	}

	out, err := json.Marshal(v)
	require.NoError(t, err)
	return string(out)
}
