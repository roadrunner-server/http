package handler

import (
	"net/http"

	httpV2 "github.com/roadrunner-server/api-go/v6/http/v2"
)

func convert(headers http.Header) map[string]*httpV2.HttpHeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV2.HttpHeaderValue, len(headers))
	for k, v := range headers {
		resp[k] = &httpV2.HttpHeaderValue{Values: v}
	}
	return resp
}

func convertCookies(cookies map[string]string) map[string]*httpV2.HttpHeaderValue {
	if len(cookies) == 0 {
		return nil
	}

	resp := make(map[string]*httpV2.HttpHeaderValue, len(cookies))
	for k, v := range cookies {
		resp[k] = &httpV2.HttpHeaderValue{Values: []string{v}}
	}
	return resp
}

func convertAttributes(attrs map[string][]string) map[string]*httpV2.HttpHeaderValue {
	if len(attrs) == 0 {
		return nil
	}

	resp := make(map[string]*httpV2.HttpHeaderValue, len(attrs))
	for k, v := range attrs {
		resp[k] = &httpV2.HttpHeaderValue{Values: v}
	}
	return resp
}
