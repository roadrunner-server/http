package handler

import (
	"errors"
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
func (dt dataTree) push(k string, v []string) error {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		return dt.mount(keys, v)
	}
	return nil
}

func prepareNewDataNode(dt dataTree, i []string, v []string) (bool, error) {
	if len(i) == 0 {
		return false, errors.New("should not prepare new node")
	}

	_, ok := dt[i[0]]
	if !ok {
		dt[i[0]] = make(dataTree)
		return true, nil
	}

	potentialErr := fmt.Errorf(
		"invalid value in dataTree. key: %+v, val: %+v, tree: %+v",
		i[0],
		v,
		dt,
	)

	_, dataTreeOK := dt[i[0]].(dataTree)
	if !dataTreeOK {
		switch oldV := dt[i[0]].(type) {
		case string:
			if len(oldV) == 0 {
				dt[i[0]] = make(dataTree)
				return true, nil
			}
		case []string:
			if len(oldV) == 0 {
				dt[i[0]] = make(dataTree)
				return true, nil
			}
		}
		return false, potentialErr
	}

	if len(i) > 1 {
		return true, nil
	}

	if len(v) == 0 || len(v[0]) == 0 {
		return false, nil
	}

	return false, potentialErr
}

// mount mounts data tree recursively.
func (dt dataTree) mount(i []string, v []string) error {
	if len(i) == 0 {
		return nil
	}

	shouldContinue, err := prepareNewDataNode(dt, i, v)
	if err != nil {
		return err
	}
	if !shouldContinue {
		return nil
	}

	if len(i) == 2 && i[1] == "" {
		// non associated array of elements
		dt[i[0]] = v
		return nil
	}
	if len(i) == 1 && len(v) == 1 {
		dt[i[0]] = v[0]
		return nil
	}
	if len(i) == 1 {
		dt[i[0]] = v
		return nil
	}

	return dt[i[0]].(dataTree).mount(i[1:], v)
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
func (ft fileTree) push(k string, v []*FileUpload) error {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		return ft.mount(keys, v)
	}
	return nil
}

func prepareNewFileNode(ft fileTree, i []string, v []*FileUpload) (bool, error) {
	if len(i) == 0 {
		return false, errors.New("should not prepare new node")
	}

	_, ok := ft[i[0]]
	if !ok {
		ft[i[0]] = make(fileTree)
		return true, nil
	}

	potentialErr := fmt.Errorf(
		"invalid value in fileTree. key: %+v, val: %+v, tree: %+v",
		i[0],
		v,
		ft,
	)

	_, fileTreeOK := ft[i[0]].(fileTree)
	if !fileTreeOK {
		switch oldV := ft[i[0]].(type) {
		case *FileUpload:
			if oldV == nil {
				ft[i[0]] = make(fileTree)
				return true, nil
			}
		case []*FileUpload:
			if len(oldV) == 0 {
				ft[i[0]] = make(fileTree)
				return true, nil
			}
		}

		return false, potentialErr
	}

	if len(i) > 1 {
		return true, nil
	}

	if len(v) == 0 || v[0] == nil {
		return false, nil
	}

	return false, potentialErr
}

// mount mounts data tree recursively.
func (ft fileTree) mount(i []string, v []*FileUpload) error {
	if len(i) == 0 {
		return nil
	}

	shouldContinue, err := prepareNewFileNode(ft, i, v)
	if err != nil {
		return err
	}
	if !shouldContinue {
		return nil
	}

	switch {
	case len(i) == 2 && i[1] == "":
		// non associated array of elements
		ft[i[0]] = v
		return nil
	case len(i) == 1 && len(v) == 1:
		ft[i[0]] = v[0]
		return nil
	case len(i) == 1:
		ft[i[0]] = v
		return nil
	}

	return ft[i[0]].(fileTree).mount(i[1:], v)
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
