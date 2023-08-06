package handler

import (
	"fmt"
	"net/http"
)

// MaxLevel defines maximum tree depth for incoming request data and files.
const MaxLevel = 127

type dataTree map[string]any
type fileTree map[string]any

// parsePostForm parses incoming request body into data tree.
func parsePostForm(r *http.Request) (dataTree, error) {
	data := make(dataTree, 2)

	if r.PostForm != nil {
		for k, v := range r.PostForm {
			err := data.push(k, v)
			if err != nil {
				return nil, err
			}
		}
	}

	return data, nil
}

// parseMultipartData parses incoming request body into data tree.
func parseMultipartData(r *http.Request) (dataTree, error) {
	data := make(dataTree, 2)

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
// This is written to handle this very edge case
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
func (dt dataTree) mount(keys, v []string) error {
	if len(keys) == 0 {
		return nil
	}

	done, err := prepareTreeNode(dt, keys, v)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	// getting all elements of non associated array
	if len(keys) == 2 && keys[1] == "" {
		dt[keys[0]] = v
		return nil
	}
	// only getting the last elements
	if len(keys) == 1 && len(v) > 0 {
		dt[keys[0]] = v[len(v)-1]
		return nil
	}
	if len(keys) == 1 {
		dt[keys[0]] = v
		return nil
	}

	return dt[keys[0]].(dataTree).mount(keys[1:], v)
}

func prepareTreeNode[T dataTree | fileTree, V []string | []*FileUpload](tree T, i []string, v V) (bool, error) {
	if _, ok := tree[i[0]]; !ok {
		tree[i[0]] = make(T)
		return false, nil
	}

	_, isBranch := tree[i[0]].(T)
	isDataInTreeEmpty := isDataEmpty(tree[i[0]])
	isIncomingValueEmpty := isDataEmpty(v)
	isLeafNodeIncoming := len(i) == 1 || (len(i) == 2 && len(i[1]) == 0)

	if !isBranch {
		if !isDataInTreeEmpty {
			// we have leaf node with value but there is incoming branch data in the input
			if len(i) > 1 && len(i[1]) > 0 {
				return true, invalidMultipleValuesErr(i[0])
			}

			// we have a non-empty leaf node and there is incoming empty value
			if isIncomingValueEmpty {
				return true, nil
			}
		}

		// we have an empty leaf node and there is incoming value
		if isDataInTreeEmpty && !isIncomingValueEmpty {
			tree[i[0]] = make(T)
			return false, nil
		}
	}

	if isBranch && isLeafNodeIncoming {
		// we have a branch with tree data but there is incoming value in the input
		if !isIncomingValueEmpty {
			return true, invalidMultipleValuesErr(i[0])
		}

		// we have a branch with tree data but there is incoming empty value
		if isIncomingValueEmpty {
			return true, nil
		}
	}

	return false, nil
}

func isDataEmpty(v any) bool {
	switch actualV := v.(type) {
	case string:
		return len(actualV) == 0
	case []string:
		return len(actualV) == 0 || (len(actualV) == 1 && len(actualV[0]) == 0)
	case []*FileUpload:
		return len(actualV) == 0 || (len(actualV) == 1 && actualV[0] == nil)
	default:
		return v == nil
	}
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

	done, err := prepareTreeNode(ft, i, v)
	if err != nil {
		return err
	}
	if done {
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
