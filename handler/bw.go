package handler

import (
	"io"
)

var _ io.ReadCloser = &bodyWrapper{}

type bodyWrapper struct {
	io.ReadCloser
	read int
}

func (w *bodyWrapper) Read(b []byte) (int, error) {
	n, err := w.ReadCloser.Read(b)
	w.read = n
	return n, err
}

func (w *bodyWrapper) Close() error {
	return w.ReadCloser.Close()
}
