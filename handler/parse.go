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
func (dt dataTree) push(k string, v []string) error {
	keys := fetchIndexes(k)
	if len(keys) <= MaxLevel {
		return dt.mount(keys, v)
	}
	return nil
}

func invalidMultipleValuesErr(key string) error {
	return fmt.Errorf(
		"invalid multiple values to key '%+v' in tree",
		key,
	)
}

// mount mounts data tree recursively.
//
// This can handle this edge case
// Assume that we have the following POST data
//
// _token: NM8eor1JFGRLxfaTNHanGX4en0ZMFtatdz1Muu5Z
// _method: PUT
// _http_referrer: http://localhost/admin/article
// name: Rerum non omnis dicta occaecati dignissimos culpa commodi.
// options:
// options[0][id]: 97b64557-24ad-4099-88f2-0d275874d2e8
// options[0][order_priority]: 1000
// options[0][name]: Adipisci eaque vero laborum reprehenderit id ipsam deserunt.
// id: 97b64557-19ba-49bd-bdec-783e32fbc6e8
// _save_action: save_and_back
//
// If we don't ignore empty options we will lose the whole array of data in key options[x]
// So we will ignore it and process the array of data in options[x] if those present in the request.
// Same is done for fileTree data structure underneath
func (dt dataTree) mount(i, v []string) error {
	if len(i) == 0 {
		return nil
	}

	shouldContinue, err := dt.prepareNewDataNode(i, v)
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
	if len(i) == 1 && len(v) > 0 {
		dt[i[0]] = v[len(v)-1]
		return nil
	}
	if len(i) == 1 {
		dt[i[0]] = v
		return nil
	}

	return dt[i[0]].(dataTree).mount(i[1:], v)
}

func isDataValueEmpty(v []string) bool {
	return len(v) == 0 || (len(v) == 1 && len(v[0]) == 0)
}

func (dt dataTree) prepareNewDataNode(i, v []string) (bool, error) {
	_, ok := dt[i[0]]
	if !ok {
		dt[i[0]] = make(dataTree)
		return true, nil
	}

	_, dataTreeOK := dt[i[0]].(dataTree)
	if !dataTreeOK {
		isOldDataEmpty := false
		switch oldV := dt[i[0]].(type) {
		case string:
			isOldDataEmpty = len(oldV) == 0
		case []string:
			isOldDataEmpty = isDataValueEmpty(oldV)
		}
		if !isOldDataEmpty && isDataValueEmpty(v) {
			return false, nil
		}
		if len(i) == 2 && i[1] == "" {
			return true, nil
		}
		if isOldDataEmpty && len(i) > 1 {
			dt[i[0]] = make(dataTree)
			return true, nil
		}
		if len(i) == 1 {
			return true, nil
		}

		return false, invalidMultipleValuesErr(i[0])
	}

	if len(i) > 1 && len(i[1]) > 0 {
		return true, nil
	}

	if isDataValueEmpty(v) {
		return false, nil
	}

	return false, invalidMultipleValuesErr(i[0])
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

// mount mounts data tree recursively.
func (ft fileTree) mount(i []string, v []*FileUpload) error {
	if len(i) == 0 {
		return nil
	}

	shouldContinue, err := ft.prepareNewFileNode(i, v)
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
	case len(i) == 1 && len(v) > 0:
		ft[i[0]] = v[0]
		return nil
	case len(i) == 1:
		ft[i[0]] = v
		return nil
	}

	return ft[i[0]].(fileTree).mount(i[1:], v)
}

func isFileUploadEmpty(v []*FileUpload) bool {
	return len(v) == 0 || (len(v) == 1 && v[0] == nil)
}

func (ft fileTree) prepareNewFileNode(i []string, v []*FileUpload) (bool, error) {
	_, ok := ft[i[0]]
	if !ok {
		ft[i[0]] = make(fileTree)
		return true, nil
	}

	_, fileTreeOK := ft[i[0]].(fileTree)
	if !fileTreeOK {
		isOldVEmpty := false
		switch oldV := ft[i[0]].(type) {
		case *FileUpload:
			isOldVEmpty = oldV == nil
		case []*FileUpload:
			isOldVEmpty = isFileUploadEmpty(oldV)
		}
		if !isOldVEmpty && isFileUploadEmpty(v) {
			return false, nil
		}
		if len(i) == 2 && i[1] == "" {
			return true, nil
		}
		if isOldVEmpty && len(i) > 1 {
			ft[i[0]] = make(fileTree)
			return true, nil
		}
		if len(i) == 1 {
			return true, nil
		}

		return false, invalidMultipleValuesErr(i[0])
	}

	if len(i) > 1 && len(i[1]) > 0 {
		return true, nil
	}

	if isFileUploadEmpty(v) {
		return false, nil
	}

	return false, invalidMultipleValuesErr(i[0])
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
