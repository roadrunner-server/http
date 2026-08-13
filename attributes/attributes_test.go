package attributes

import (
	"context"
	"net/http"
	"testing"

	rrcontext "github.com/roadrunner-server/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withPsrValue returns a request whose context holds an arbitrary value under
// the psr attributes key.
func withPsrValue(value any) *http.Request {
	r := &http.Request{}
	return r.WithContext(context.WithValue(r.Context(), rrcontext.PsrContextKey, value))
}

func TestAllAttributes(t *testing.T) {
	r := &http.Request{}
	r = Init(r)

	require.NoError(t, Set(r, "key", "value"))
	assert.Equal(t, map[string][]string{"key": {"value"}}, All(r))
}

func TestAllAttributesNone(t *testing.T) {
	r := &http.Request{}
	r = Init(r)

	assert.Equal(t, map[string][]string{}, All(r))
}

func TestAllAttributesNone2(t *testing.T) {
	r := &http.Request{}

	assert.Nil(t, All(r))
}

func TestGetAttribute(t *testing.T) {
	r := &http.Request{}
	r = Init(r)

	require.NoError(t, Set(r, "key", "value"))
	assert.Equal(t, []string{"value"}, Get(r, "key"))
}

func TestGetAttributeNone(t *testing.T) {
	r := &http.Request{}
	r = Init(r)
	assert.Nil(t, Get(r, "key"))
}

func TestGetAttributeNone2(t *testing.T) {
	r := &http.Request{}

	assert.Nil(t, Get(r, "key"))
}

func TestSetAttribute(t *testing.T) {
	r := &http.Request{}
	r = Init(r)

	require.NoError(t, Set(r, "key", "value"))
	assert.Equal(t, []string{"value"}, Get(r, "key"))
}

func TestSetAttributeNone(t *testing.T) {
	r := &http.Request{}
	err := Set(r, "key", "value")
	assert.Error(t, err)
	assert.Nil(t, Get(r, "key"))
}

// All accepts the two foreign map shapes middleware may store under the key.
func TestAllForeignMapShapes(t *testing.T) {
	multiValue := withPsrValue(map[string][]string{"a": {"1", "2"}})
	assert.Equal(t, map[string][]string{"a": {"1", "2"}}, All(multiValue))

	singleValue := withPsrValue(map[string]string{"a": "1"})
	assert.Equal(t, map[string][]string{"a": {"1"}}, All(singleValue))
}

func TestAllUnexpectedType(t *testing.T) {
	assert.Nil(t, All(withPsrValue(42)))
}

// Init must not overwrite attributes stored by an earlier middleware.
func TestInitKeepsExistingBag(t *testing.T) {
	r := Init(&http.Request{})
	require.NoError(t, Set(r, "key", "value"))

	same := Init(r)
	assert.Same(t, r, same)
	assert.Equal(t, []string{"value"}, Get(same, "key"))
}

func TestGetUnexpectedType(t *testing.T) {
	assert.Nil(t, Get(withPsrValue(42), "key"))
}

func TestSetUnexpectedType(t *testing.T) {
	err := Set(withPsrValue(42), "key", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected type stored under")
}

// Set appends to an existing key instead of replacing it.
func TestSetAppends(t *testing.T) {
	r := Init(&http.Request{})

	require.NoError(t, Set(r, "key", "v1"))
	require.NoError(t, Set(r, "key", "v2"))

	assert.Equal(t, []string{"v1", "v2"}, Get(r, "key"))
}

func TestAttrsGetNil(t *testing.T) {
	assert.Equal(t, "", attrs(nil).get("key"))
}

func TestAttrsDelete(t *testing.T) {
	assert.NotPanics(t, func() { attrs(nil).Delete("key") })

	bag := attrs{"key": {"value"}, "other": {"value"}}
	bag.Delete("key")
	assert.Equal(t, attrs{"other": {"value"}}, bag)
}
