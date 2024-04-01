package attributes

import (
	"context"
	"errors"
	"net/http"

	"github.com/roadrunner-server/sdk/v4/utils"
)

type attrs map[string][]string

func (v attrs) get(key string) any {
	if v == nil {
		return ""
	}

	return v[key]
}

func (v attrs) set(key string, value string) {
	if v[key] == nil {
		v[key] = []string{value}
		return
	}
	v[key] = append(v[key], value)
}

func (v attrs) del(key string) {
	delete(v, key)
}

// Init returns request with new context and attribute bag.
func Init(r *http.Request) *http.Request {
	// do not overwrite psr attributes
	if val := r.Context().Value(utils.PsrContextKey); val == nil {
		return r.WithContext(context.WithValue(r.Context(), utils.PsrContextKey, attrs{}))
	}

	return r
}

// All returns all context attributes.
func All(r *http.Request) map[string][]string {
	v := r.Context().Value(utils.PsrContextKey)
	if v == nil {
		return nil
	}

	switch t := v.(type) {
	case attrs:
		return t
	case map[string][]string:
		return t
	case map[string]string:
		newm := make(map[string][]string)
		for k, v := range t {
			newm[k] = []string{v}
		}

		return newm
	default:
		return nil
	}
}

// Get gets the value from request context. It replaces any existing
// values.
func Get(r *http.Request, key string) any {
	v := r.Context().Value(utils.PsrContextKey)
	if v == nil {
		return nil
	}

	return v.(attrs).get(key)
}

// Set sets the key to value. It replaces any existing
// values. Context specific.
func Set(r *http.Request, key string, value string) error {
	v := r.Context().Value(utils.PsrContextKey)
	if v == nil {
		return errors.New("unable to find `psr:attributes` context key")
	}

	v.(attrs).set(key, value)
	return nil
}

// Delete deletes values associated with attribute key.
func (v attrs) Delete(key string) {
	if v == nil {
		return
	}

	v.del(key)
}
