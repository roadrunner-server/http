package helpers

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertGet issues a GET and asserts the response status code and body.
func AssertGet(t *testing.T, url string, wantCode int, wantBody string) {
	t.Helper()

	code, body := GetBody(t, url)
	assert.Equal(t, wantCode, code)
	assert.Equal(t, wantBody, body)
}

// GetBody issues a GET and returns the status code and the body. The body is
// read and closed before returning.
func GetBody(t *testing.T, url string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = r.Body.Close()
	}()

	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	return r.StatusCode, string(b)
}
