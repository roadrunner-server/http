package helpers

import (
	"io"
	"net/http"
)

func Get(url string) (string, *http.Response, error) {
	r, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return "", nil, err
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "", nil, err
	}

	err = r.Body.Close()
	if err != nil {
		return "", nil, err
	}

	return string(b), r, err
}
