package helpers

import (
	"net/http"
)

// HeaderContainsUpgrade .. https://golang.org/pkg/net/http/#Hijacker
func HeaderContainsUpgrade(r *http.Request) bool {
	if _, ok := r.Header["Upgrade"]; ok {
		return true
	}
	return false
}
