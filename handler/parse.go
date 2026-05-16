package handler

import (
	"fmt"
	"net/http"
	"strings"
)

// MaxLevel caps tree depth for incoming form keys to protect against
// pathological inputs like "a[a][a]...[a]".
const MaxLevel = 127

// ErrMaxLevelExceeded is returned by push when a key's bracket-notation
// depth exceeds MaxLevel.
var ErrMaxLevelExceeded = fmt.Errorf("form key depth exceeds MaxLevel (%d)", MaxLevel)

type dataTree map[string]any
type fileTree map[string]any

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
	if len(keys) > MaxLevel {
		return ErrMaxLevelExceeded
	}
	return mount(dt, keys, v, lastValue)
}

func invalidMultipleValuesErr(key string) error {
	return fmt.Errorf(
		"invalid multiple values to key '%+v' in tree",
		key,
	)
}

// lastValue picks the final string value for a leaf. PHP's $_POST semantics:
// when the same key arrives more than once (e.g., "a=1&a=2"), the last wins.
func lastValue(v []string) any { return v[len(v)-1] }

// firstFile picks the first file for a scalar leaf. PHP's $_FILES doesn't
// permit multiple files at the same scalar key — multi-file uploads must use
// "name[]" notation, which lands in mount's non-associative branch instead.
func firstFile(v []*FileUpload) any { return v[0] }

// mount walks the key path iteratively, creating intermediate nodes as needed,
// and stores the value at the terminal position. Conflict resolution between
// existing and incoming nodes is handled by prepareTreeNode.
func mount[T dataTree | fileTree, V []string | []*FileUpload](
	tree T, keys []string, v V, leafVal func(V) any,
) error {
	for len(keys) > 0 {
		done, err := prepareTreeNode(tree, keys, v)
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		// non-associated array (e.g., name[])
		if len(keys) == 2 && keys[1] == "" {
			tree[keys[0]] = v
			return nil
		}
		// leaf node
		if len(keys) == 1 {
			if len(v) > 0 {
				tree[keys[0]] = leafVal(v)
			} else {
				tree[keys[0]] = v
			}
			return nil
		}

		// descend into child map
		next, ok := tree[keys[0]].(T)
		if !ok {
			tree[keys[0]] = make(T, 1)
			next = tree[keys[0]].(T)
		}
		tree = next
		keys = keys[1:]
	}
	return nil
}

// prepareTreeNode resolves the collision when a key already exists in the
// tree. Notable edge case: a bare key like "options" arriving with an empty
// value alongside "options[0][name]" with real data — without ignoring the
// empty leaf, the whole nested array would be lost.
func prepareTreeNode[T dataTree | fileTree, V []string | []*FileUpload](
	tree T, keys []string, v V,
) (done bool, err error) {
	existing, exists := tree[keys[0]]
	if !exists {
		tree[keys[0]] = make(T)
		return false, nil
	}

	_, isBranch := existing.(T)
	existingEmpty := isDataEmpty(existing)
	incomingEmpty := isDataEmpty(v)
	isLeaf := len(keys) == 1 || (len(keys) == 2 && keys[1] == "")

	switch {
	case !isBranch && existingEmpty && !incomingEmpty:
		// empty leaf → incoming has data: replace with fresh node
		tree[keys[0]] = make(T)
		return false, nil
	case !isBranch && !existingEmpty && len(keys) > 1 && len(keys[1]) > 0:
		// non-empty leaf vs incoming branch: conflict
		return true, invalidMultipleValuesErr(keys[0])
	case !isBranch && !existingEmpty && incomingEmpty:
		// non-empty leaf vs empty incoming: keep existing
		return true, nil
	case isBranch && isLeaf:
		// existing branch vs incoming scalar at the terminal level: empty
		// incoming is ignored (keeps the branch), non-empty is a conflict.
		if incomingEmpty {
			return true, nil
		}
		return true, invalidMultipleValuesErr(keys[0])
	default:
		// continue descent (branch→branch, empty→empty, or leaf overwrite)
		return false, nil
	}
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

// pushes new file upload into its proper place.
func (ft fileTree) push(k string, v []*FileUpload) error {
	keys := fetchIndexes(k)
	if len(keys) > MaxLevel {
		return ErrMaxLevelExceeded
	}
	return mount(ft, keys, v, firstFile)
}

// fetchIndexes parses a PHP-style bracket-notation field name into key segments.
// e.g. "key[sub][idx]" → ["key", "sub", "idx"], "key[]" → ["key", ""]
func fetchIndexes(s string) []string {
	const (
		stNormal = iota // building current segment outside brackets
		stOpen          // just saw '['
		stClose         // just saw ']'
	)

	keys := make([]string, 0, 4)
	var buf strings.Builder
	state := stNormal

	for _, c := range s {
		switch c {
		case ' ':
			continue
		case '[':
			if state == stNormal {
				keys = append(keys, buf.String())
				buf.Reset()
			}
			state = stOpen
		case ']':
			keys = append(keys, buf.String())
			buf.Reset()
			state = stClose
		default:
			buf.WriteRune(c)
			state = stNormal
		}
	}

	if buf.Len() > 0 || len(keys) == 0 {
		keys = append(keys, buf.String())
	}
	return keys
}
