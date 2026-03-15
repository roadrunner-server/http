package handler

import (
	"net/http"

	httpV2proto "github.com/roadrunner-server/api-go/v6/http/v2"
)

func convert(headers http.Header) map[string]*httpV2proto.HttpHeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV2proto.HttpHeaderValue, len(headers))

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV2proto.HttpHeaderValue{}
		}

		for _, vv := range v {
			resp[k].Values = append(resp[k].GetValues(), []byte(vv))
		}
	}

	return resp
}

func convertCookies(headers map[string]string) map[string]*httpV2proto.HttpHeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV2proto.HttpHeaderValue, len(headers))

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV2proto.HttpHeaderValue{}
		}

		resp[k].Values = append(resp[k].GetValues(), []byte(v))
	}

	return resp
}
