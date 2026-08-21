package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// recorderWriter records every call forwarded by the wrapper.
type recorderWriter struct {
	hdr   http.Header
	codes []int
	body  []byte
}

func (r *recorderWriter) Header() http.Header {
	if r.hdr == nil {
		r.hdr = http.Header{}
	}
	return r.hdr
}

func (r *recorderWriter) WriteHeader(code int) {
	r.codes = append(r.codes, code)
}

func (r *recorderWriter) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

func newTestWrapper() (*wrapper, *recorderWriter) {
	rec := &recorderWriter{}
	return &wrapper{code: http.StatusOK, w: rec}, rec
}

// A 1xx informational header does not lock the response: the final status
// written after it must reach the underlying writer (issue roadrunner#2381).
func TestWrapper_EarlyHintThenFinal(t *testing.T) {
	w, rec := newTestWrapper()

	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusNotFound)

	assert.Equal(t, []int{http.StatusEarlyHints, http.StatusNotFound}, rec.codes)
	assert.Equal(t, http.StatusNotFound, w.code)
}

func TestWrapper_SecondFinalDropped(t *testing.T) {
	w, rec := newTestWrapper()

	w.WriteHeader(http.StatusOK)
	w.WriteHeader(http.StatusInternalServerError)

	assert.Equal(t, []int{http.StatusOK}, rec.codes)
	assert.Equal(t, http.StatusOK, w.code)
}

// A 1xx after the final status is invalid and must not reach the writer.
func TestWrapper_HintAfterFinalDropped(t *testing.T) {
	w, rec := newTestWrapper()

	w.WriteHeader(http.StatusOK)
	w.WriteHeader(http.StatusEarlyHints)

	assert.Equal(t, []int{http.StatusOK}, rec.codes)
}

// net/http treats 101 as a final status, so it locks the wrapper too.
func TestWrapper_SwitchingProtocolsIsFinal(t *testing.T) {
	w, rec := newTestWrapper()

	w.WriteHeader(http.StatusSwitchingProtocols)
	w.WriteHeader(http.StatusOK)

	assert.Equal(t, []int{http.StatusSwitchingProtocols}, rec.codes)
	assert.Equal(t, http.StatusSwitchingProtocols, w.code)
}

// A body written without a final status keeps the implicit 200 for the log.
func TestWrapper_HintThenBodyKeepsImplicit200(t *testing.T) {
	w, rec := newTestWrapper()

	w.WriteHeader(http.StatusEarlyHints)
	_, err := w.Write([]byte("body"))

	assert.NoError(t, err)
	assert.Equal(t, []int{http.StatusEarlyHints}, rec.codes)
	assert.Equal(t, http.StatusOK, w.code)
	assert.Equal(t, 4, w.write)

	// the response is locked by the body write
	w.WriteHeader(http.StatusNotFound)
	assert.Equal(t, []int{http.StatusEarlyHints}, rec.codes)
	assert.Equal(t, http.StatusOK, w.code)
}

func TestWrapper_ResetClearsState(t *testing.T) {
	w, _ := newTestWrapper()

	w.WriteHeader(http.StatusNotFound)
	_, err := w.Write([]byte("body"))
	assert.NoError(t, err)

	w.reset()

	assert.Equal(t, http.StatusOK, w.code)
	assert.False(t, w.wc)
	assert.Zero(t, w.read)
	assert.Zero(t, w.write)
}
