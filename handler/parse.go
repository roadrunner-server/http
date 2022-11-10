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
func parseData(r *http.Request) (dataTree, error) {
	data := make(dataTree)
	if r.PostForm != nil {
		for k, v := range r.PostForm {
			err := data.push(k, v)
			if err != nil {
				return nil, err
			}
		}
	}

	if r.MultipartForm != nil {
		for k, v := range r.MultipartForm.Value {
			err := data.push(k, v)
			if err != nil {
				return nil, err
			}
		}
	}

	return data, nil
}

// pushes value into data tree.
func (d dataTree) push(k string, v []string) error {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		return d.mount(keys, v)
	}
	return nil
}

func prepareNewDataNode(d dataTree, i []string, v []string) (bool, error) {
	p, ok := d[i[0]]
	_, dataTreeOK := p.(dataTree)
	if ok && dataTreeOK && len(i) == 1 && (len(v) == 0 || len(v[0]) == 0) {
		return false, nil
	}
	if ok && !dataTreeOK {
		oldV := d[i[0]]
		oldVString, stringOk := oldV.(string)
		oldVStringArray, stringArrayOk := oldV.([]string)

		switch {
		case stringOk && len(oldVString) == 0,
			stringArrayOk && len(oldVStringArray) == 0:
			d[i[0]] = make(dataTree)
		default:
			return false, fmt.Errorf(
				"invalid value in dataTree. key: %+v, val: %+v, tree: %+v",
				i[0],
				v,
				d,
			)
		}
	}
	if !ok {
		d[i[0]] = make(dataTree)
	}
	return true, nil
}

// mount mounts data tree recursively.
func (d dataTree) mount(i []string, v []string) error {
	if len(i) == 0 {
		return nil
	}

	ok, err := prepareNewDataNode(d, i, v)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if len(i) == 2 && i[1] == "" {
		// non associated array of elements
		d[i[0]] = v
		return nil
	}

	if len(i) == 1 {
		if len(v) == 1 {
			d[i[0]] = v[0]
			return nil
		}
		d[i[0]] = v
		return nil
	}

	return d[i[0]].(dataTree).mount(i[1:], v)
}

// parse incoming dataTree request into JSON (including contentMultipart form dataTree)
func parseUploads(r *http.Request, uid, gid int) (*Uploads, error) {
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
		err := u.tree.push(k, files)
		if err != nil {
			return nil, err
		}
	}

	return u, nil
}

// pushes new file upload into it's proper place.
func (d fileTree) push(k string, v []*FileUpload) error {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		return d.mount(keys, v)
	}
	return nil
}

func prepareNewFileNode(d fileTree, i []string, v []*FileUpload) (bool, error) {
	p, ok := d[i[0]]
	_, fileTreeOK := p.(fileTree)
	if ok && fileTreeOK && len(i) == 1 && (len(v) == 0 || v[0] == nil) {
		return false, nil
	}
	if ok && !fileTreeOK {
		oldV := d[i[0]]
		oldVFileUpload, fileUploadOK := oldV.(*FileUpload)
		oldVFileUploadArray, fileUploadArrayOK := oldV.([]*FileUpload)
		switch {
		case fileUploadOK && oldVFileUpload == nil,
			fileUploadArrayOK && len(oldVFileUploadArray) == 0:
			d[i[0]] = make(fileTree)
		default:
			return false, fmt.Errorf(
				"invalid value in fileTree. key: %+v, val: %+v, tree: %+v",
				i[0],
				v,
				d,
			)
		}
	}
	if !ok {
		d[i[0]] = make(fileTree)
	}
	return true, nil
}

// mount mounts data tree recursively.
func (d fileTree) mount(i []string, v []*FileUpload) error {
	if len(i) == 0 {
		return nil
	}

	ok, err := prepareNewFileNode(d, i, v)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if len(i) == 2 && i[1] == "" {
		// non associated array of elements
		d[i[0]] = v
		return nil
	}
	if len(i) == 1 && len(v) == 1 {
		d[i[0]] = v[0]
		return nil
	}
	if len(i) == 1 {
		d[i[0]] = v
		return nil
	}

	return d[i[0]].(fileTree).mount(i[1:], v)
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
