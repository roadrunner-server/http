package helpers

import (
	"bytes"
	"io"
	"net/http"
	"os"
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

func All(fn string) string {
	f, err := os.Open(fn)
	if err != nil {
		panic(err)
	}

	b := new(bytes.Buffer)
	_, err = io.Copy(b, f)
	if err != nil {
		return ""
	}

	err = f.Close()
	if err != nil {
		return ""
	}

	return b.String()
}
