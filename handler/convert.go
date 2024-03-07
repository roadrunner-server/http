package handler

import (
	"mime/multipart"
	"net/textproto"

	httpV1Beta "github.com/roadrunner-server/api/v4/build/http/v1beta"
)

func convert(headers map[string][]string) map[string]*httpV1Beta.HeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV1Beta.HeaderValue)

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV1Beta.HeaderValue{}
		}
		resp[k].Value = append(resp[k].Value, v...)
	}

	return resp
}

func convertCookies(headers map[string]string) map[string]*httpV1Beta.HeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV1Beta.HeaderValue)

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV1Beta.HeaderValue{}
		}
		resp[k].Value = append(resp[k].Value, v)
	}

	return resp
}

func convertMimeHeader(headers textproto.MIMEHeader) map[string]*httpV1Beta.HeaderValue {
	if len(headers) == 0 {
		return nil
	}

	resp := make(map[string]*httpV1Beta.HeaderValue)

	for k, v := range headers {
		if resp[k] == nil {
			resp[k] = &httpV1Beta.HeaderValue{}
		}
		resp[k].Value = append(resp[k].Value, v...)
	}

	return resp
}

func convertUploads(upl *Uploads) *httpV1Beta.Uploads {
	if upl == nil {
		return nil
	}
	resp := &httpV1Beta.Uploads{}

	for _, v := range upl.list {
		resp.List = append(resp.List, &httpV1Beta.FileUpload{
			Name:         v.Name,
			Mime:         v.Mime,
			Size:         v.Size,
			Error:        int64(v.Error),
			TempFilename: v.TempFilename,
			Header:       convertFileHeader(v.header),
		})
	}

	return resp
}

func convertFileHeader(upl *multipart.FileHeader) *httpV1Beta.FileHeader {
	if upl == nil {
		return nil
	}

	return &httpV1Beta.FileHeader{
		Size:     upl.Size,
		Filename: upl.Filename,
		Header:   convertMimeHeader(upl.Header),
	}
}
