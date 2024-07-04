package tests

import (
	"net/http"
	"testing"

	"github.com/roadrunner-server/http/v5/attributes"
	"github.com/stretchr/testify/assert"
)

func TestAllAttributes(t *testing.T) {
	r := &http.Request{}
	r = attributes.Init(r)

	err := attributes.Set(r, "key", "value")
	if err != nil {
		t.Errorf("error during the Set: error %v", err)
	}

	assert.Equal(t, map[string][]string{"key": {"value"}}, attributes.All(r))
}

func TestAllAttributesNone(t *testing.T) {
	r := &http.Request{}
	r = attributes.Init(r)

	assert.Equal(t, attributes.All(r), map[string][]string{})
}

func TestAllAttributesNone2(t *testing.T) {
	r := &http.Request{}

	assert.Nil(t, attributes.All(r))
}

func TestGetAttribute(t *testing.T) {
	r := &http.Request{}
	r = attributes.Init(r)

	err := attributes.Set(r, "key", "value")
	if err != nil {
		t.Errorf("error during the Set: error %v", err)
	}
	assert.Equal(t, attributes.Get(r, "key"), []string{"value"})
}

func TestGetAttributeNone(t *testing.T) {
	r := &http.Request{}
	r = attributes.Init(r)
	assert.Nil(t, attributes.Get(r, "key"))
}

func TestGetAttributeNone2(t *testing.T) {
	r := &http.Request{}

	assert.Equal(t, attributes.Get(r, "key"), nil)
}

func TestSetAttribute(t *testing.T) {
	r := &http.Request{}
	r = attributes.Init(r)

	err := attributes.Set(r, "key", "value")
	if err != nil {
		t.Errorf("error during the Set: error %v", err)
	}
	assert.Equal(t, []string{"value"}, attributes.Get(r, "key"))
}

func TestSetAttributeNone(t *testing.T) {
	r := &http.Request{}
	err := attributes.Set(r, "key", "value")
	assert.Error(t, err)
	assert.Equal(t, attributes.Get(r, "key"), nil)
}
