package handler

import (
	"errors"
	"mime/multipart"
	"net/http"
)

// statusError carries an explicit HTTP status code through an error chain.
// Call sites that know the correct response code wrap with withStatus;
// handleRequestErr unwraps via errors.As so the wrapped status wins over
// the default 4xx classification.
type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }
func (e *statusError) Status() int   { return e.status }

func withStatus(status int, err error) error {
	if err == nil {
		return nil
	}
	return &statusError{status: status, err: err}
}

// classifyParseErr promotes payload-size errors (*http.MaxBytesError and
// multipart.ErrMessageTooLarge) to 413 by wrapping with withStatus. Other
// errors pass through unchanged so they hit handleRequestErr's 400 default —
// every error reaching this helper originates from parsing client input.
func classifyParseErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok || errors.Is(err, multipart.ErrMessageTooLarge) {
		return withStatus(http.StatusRequestEntityTooLarge, err)
	}
	return err
}
