package handler

import (
	"errors"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestStatusError_Wrapping(t *testing.T) {
	base := errors.New("boom")
	wrapped := withStatus(http.StatusTeapot, base)

	if wrapped.Error() != "boom" {
		t.Fatalf("Error(): got %q, want %q", wrapped.Error(), "boom")
	}

	sErr, ok := errors.AsType[*statusError](wrapped)
	if !ok {
		t.Fatal("errors.AsType[*statusError] failed")
	}
	if sErr.Status() != http.StatusTeapot {
		t.Errorf("Status(): got %d, want %d", sErr.Status(), http.StatusTeapot)
	}
	if !errors.Is(wrapped, base) {
		t.Error("errors.Is should unwrap to the base error")
	}
}

func TestStatusError_WithStatusNil(t *testing.T) {
	if got := withStatus(http.StatusBadRequest, nil); got != nil {
		t.Errorf("withStatus(_, nil): got %v, want nil", got)
	}
}

func TestClassifyParseErr(t *testing.T) {
	t.Run("nil passthrough", func(t *testing.T) {
		if got := classifyParseErr(nil); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("MaxBytesError promotes to 413", func(t *testing.T) {
		err := classifyParseErr(&http.MaxBytesError{Limit: 1024})
		sErr, ok := errors.AsType[*statusError](err)
		if !ok || sErr.Status() != http.StatusRequestEntityTooLarge {
			t.Errorf("got %v, want 413 wrapper", err)
		}
	})

	t.Run("ErrMessageTooLarge promotes to 413", func(t *testing.T) {
		err := classifyParseErr(multipart.ErrMessageTooLarge)
		sErr, ok := errors.AsType[*statusError](err)
		if !ok || sErr.Status() != http.StatusRequestEntityTooLarge {
			t.Errorf("got %v, want 413 wrapper", err)
		}
	})

	t.Run("unknown error passes through unwrapped", func(t *testing.T) {
		// "invalid semicolon separator in query" is a plain errors.New from
		// url.ParseQuery — by passing through (no statusError wrapper), it
		// lands on handleRequestErr's 400 default. Protects issue #2353.
		base := errors.New("invalid semicolon separator in query")
		got := classifyParseErr(base)
		if !errors.Is(got, base) {
			t.Errorf("expected base error preserved in chain; got %v", got)
		}
		if _, ok := errors.AsType[*statusError](got); ok {
			t.Error("plain errors must not be wrapped with a status")
		}
	})
}
