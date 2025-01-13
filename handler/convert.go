package handler

import (
	"net/http"

	httpV1proto "github.com/roadrunner-server/api/v4/build/http/v1"
)

func convert(headers http.Header) map[string]*httpV1proto.HeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV1proto.HeaderValue, len(headers))

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV1proto.HeaderValue{}
		}

		for _, vv := range v {
			resp[k].Value = append(resp[k].Value, []byte(vv)...)
		}
	}

	return resp
}

func convertCookies(headers map[string]string) map[string]*httpV1proto.HeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV1proto.HeaderValue, len(headers))

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV1proto.HeaderValue{}
		}

		for _, vv := range v {
			resp[k].Value = append(resp[k].Value, byte(vv))
		}
	}

	return resp
}
