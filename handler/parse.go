package handler

import (
	"fmt"
	"net/http"
)

// MaxLevel defines maximum tree depth for incoming request data and files.
const MaxLevel = 127

type dataTree map[string]any
type fileTree map[string]any

// parseData parses incoming request body into data tree.
func parseData(r *http.Request) dataTree {
	data := make(dataTree)
	if r.PostForm != nil {
		for k, v := range r.PostForm {
			data.push(k, v)
		}
	}

	if r.MultipartForm != nil {
		for k, v := range r.MultipartForm.Value {
			data.push(k, v)
		}
	}

	return data
}

// pushes value into data tree.
func (d dataTree) push(k string, v []string) {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		d.mount(keys, v)
	}
}

// mount mounts data tree recursively.
func (d dataTree) mount(i []string, v []string) {
	if len(i) == 0 {
		return
	}

	p, ok := d[i[0]]
	if ok {
		_, dataTreeOK := p.(dataTree)
		if !dataTreeOK {
			oldV := d[i[0]]
			oldVString, stringOk := oldV.(string)
			oldVStringArray, stringArrayOk := oldV.([]string)
			if stringOk && len(oldVString) == 0 {
				d[i[0]] = make(dataTree)
			} else if stringArrayOk && len(oldVStringArray) == 0 {
				d[i[0]] = make(dataTree)
			} else {
				panic(
					fmt.Sprintf(
						"invalid value in dataTree. key: %+v, val: %+v, tree: %+v",
						i[0],
						v,
						d,
					),
				)
			}
		}
		if dataTreeOK && len(i) == 1 && (len(v) == 0 || len(v[0]) == 0) {
			return
		}
	} else {
		d[i[0]] = make(dataTree)
	}

	if len(i) == 2 && i[1] == "" {
		// non associated array of elements
		d[i[0]] = v
		return
	}

	if len(i) == 1 {
		if len(v) == 1 {
			d[i[0]] = v[0]
			return
		}
		d[i[0]] = v
		return
	}

	d[i[0]].(dataTree).mount(i[1:], v)
}

// parse incoming dataTree request into JSON (including contentMultipart form dataTree)
func parseUploads(r *http.Request, uid, gid int) *Uploads {
	u := &Uploads{
		tree: make(fileTree),
		list: make([]*FileUpload, 0),
	}

	for k, v := range r.MultipartForm.File {
		files := make([]*FileUpload, 0, len(v))
		for _, f := range v {
			files = append(files, NewUpload(f, uid, gid))
		}

		u.list = append(u.list, files...)
		u.tree.push(k, files)
	}

	return u
}

// pushes new file upload into it's proper place.
func (d fileTree) push(k string, v []*FileUpload) {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		d.mount(keys, v)
	}
}

// mount mounts data tree recursively.
func (d fileTree) mount(i []string, v []*FileUpload) {
	if len(i) == 0 {
		return
	}

	p, ok := d[i[0]]
	if ok {
		_, fileTreeOK := p.(fileTree)
		if !fileTreeOK {
			oldV := d[i[0]]
			oldVFileUpload, fileUploadOK := oldV.(*FileUpload)
			oldVFileUploadArray, fileUploadArrayOK := oldV.([]*FileUpload)
			if fileUploadOK && oldVFileUpload == nil {
				d[i[0]] = make(fileTree)
			} else if fileUploadArrayOK && len(oldVFileUploadArray) == 0 {
				d[i[0]] = make(fileTree)
			} else {
				panic(
					fmt.Sprintf(
						"invalid value in fileTree. key: %+v, val: %+v, tree: %+v",
						i[0],
						v,
						d,
					),
				)
			}
		}
		if fileTreeOK && len(i) == 1 && (len(v) == 0 || v[0] == nil) {
			return
		}
	} else {
		d[i[0]] = make(fileTree)
	}

	if len(i) == 2 && i[1] == "" {
		// non associated array of elements
		d[i[0]] = v
		return
	}

	if len(i) == 1 {
		if len(v) == 1 {
			d[i[0]] = v[0]
			return
		}
		d[i[0]] = v
		return
	}

	d[i[0]].(fileTree).mount(i[1:], v)
}

// fetchIndexes parses input name and splits it into separate indexes list.
func fetchIndexes(s string) []string {
	const empty = ""
	var (
		pos  int
		ch   string
		keys = make([]string, 1)
	)

	for _, c := range s {
		ch = string(c)
		switch ch {
		case " ":
			// ignore all spaces
			continue
		case "[":
			pos = 1
			continue
		case "]":
			if pos == 1 {
				keys = append(keys, empty)
			}
			pos = 2
		default:
			if pos == 1 || pos == 2 {
				keys = append(keys, empty)
			}

			keys[len(keys)-1] += ch
			pos = 0
		}
	}

	return keys
}
