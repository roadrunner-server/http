package handler

import (
	"mime/multipart"
	"net/textproto"

	httpV1Beta "github.com/roadrunner-server/api/v4/build/http/v1beta"
)

func convert(headers map[string][]string) *httpV1Beta.Header {
	if len(headers) == 0 {
		return nil
	}

	resp := &httpV1Beta.Header{
		Header: map[string]*httpV1Beta.HeaderValue{},
	}

	for k, v := range headers {
		if resp.Header[k] == nil {
			resp.Header[k] = &httpV1Beta.HeaderValue{}
		}
		resp.Header[k].Value = append(resp.Header[k].Value, v...)
	}

	return resp
}

func convertCookies(headers map[string]string) *httpV1Beta.Header {
	if len(headers) == 0 {
		return nil
	}

	resp := &httpV1Beta.Header{
		Header: map[string]*httpV1Beta.HeaderValue{},
	}

	for k, v := range headers {
		if resp.Header[k] == nil {
			resp.Header[k] = &httpV1Beta.HeaderValue{}
		}
		resp.Header[k].Value = append(resp.Header[k].Value, v)
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
	resp := &httpV1Beta.Uploads{
		Uploads: make(map[string]*httpV1Beta.FileUploadArray),
	}

	for k, v := range upl.tree {
		switch tr := v.(type) {
		case []*FileUpload:
			if resp.Uploads[k] == nil {
				resp.Uploads[k] = &httpV1Beta.FileUploadArray{
					Tree: make([]*httpV1Beta.FileUpload, 0, 5),
				}
			}

			for _, vv := range tr {
				resp.Uploads[k].Tree = append(resp.Uploads[k].Tree, &httpV1Beta.FileUpload{
					Name:         vv.Name,
					Mime:         vv.Mime,
					Size:         vv.Size,
					Error:        int64(vv.Error),
					TempFilename: vv.TempFilename,
					Header:       convertFileHeader(vv.header),
				})
			}

		case *FileUpload:
			if resp.Uploads[k] == nil {
				resp.Uploads[k] = &httpV1Beta.FileUploadArray{
					Tree: make([]*httpV1Beta.FileUpload, 0, 5),
				}
			}
			resp.Uploads[k].Tree = append(resp.Uploads[k].Tree, &httpV1Beta.FileUpload{
				Name:         tr.Name,
				Mime:         tr.Mime,
				Size:         tr.Size,
				Error:        int64(tr.Error),
				TempFilename: tr.TempFilename,
				Header:       convertFileHeader(tr.header),
			})
		}
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
