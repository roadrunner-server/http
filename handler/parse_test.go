package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var samples = []struct { //nolint:gochecknoglobals
	in  string
	out []string
}{
	{"key", []string{"key"}},
	{"key[subkey]", []string{"key", "subkey"}},
	{"key[subkey]value", []string{"key", "subkey", "value"}},
	{"key[subkey][value]", []string{"key", "subkey", "value"}},
	{"key[subkey][value][]", []string{"key", "subkey", "value", ""}},
	{"key[subkey] [value][]", []string{"key", "subkey", "value", ""}},
	{"key [ subkey ] [ value ] [ ]", []string{"key", "subkey", "value", ""}},
	{"ключь [ subkey ] [ value ] [ ]", []string{"ключь", "subkey", "value", ""}}, // test non 1-byte symbols
	{"options[0][name]", []string{"options", "0", "name"}},
}

func Test_FetchIndexes(t *testing.T) {
	for i := 0; i < len(samples); i++ {
		if !same(fetchIndexes(samples[i].in), samples[i].out) {
			t.Errorf("got %q, want %q", fetchIndexes(samples[i].in), samples[i].out)
		}
	}
}

func BenchmarkConfig_FetchIndexes(b *testing.B) {
	b.ReportAllocs()
	for _, tt := range samples {
		for n := 0; n < b.N; n++ {
			if !same(fetchIndexes(tt.in), tt.out) {
				b.Fail()
			}
		}
	}
}

func same(in, out []string) bool {
	if len(in) != len(out) {
		return false
	}

	for i := 0; i < len(in); i++ {
		if in[i] != out[i] {
			return false
		}
	}

	return true
}

func TestDataTreePush(t *testing.T) {
	type orderedData []struct {
		key   string
		value []string
	}
	testCases := []struct {
		name    string
		values  orderedData
		wantVal any
		wantErr error
	}{
		{
			name: "non associated array should stay",
			values: orderedData{
				{
					key:   "key[]",
					value: []string{"value2"},
				},
				{
					key:   "key",
					value: []string{""},
				},
			},
			wantVal: []string{"value2"},
		},
		{
			name: "old value should get overwritten by not empty value",
			values: orderedData{
				{
					key:   "key[]",
					value: []string{"value2"},
				},
				{
					key:   "key",
					value: []string{"value1"},
				},
			},
			wantVal: "value1",
		},
		{
			name: "empty string should get overwritten by new dataTree",
			values: orderedData{
				{
					key:   "key",
					value: []string{""},
				},
				{
					key:   "key[options][id]",
					value: []string{"id1"},
				},
				{
					key:   "key[options][value]",
					value: []string{"value1"},
				},
			},
			wantVal: dataTree{
				"options": dataTree{
					"id":    "id1",
					"value": "value1",
				},
			},
		},
		{
			name: "dataTree should not get overwritten by empty string",
			values: orderedData{
				{
					key:   "key[options][id]",
					value: []string{"id1"},
				},
				{
					key:   "key[options][value]",
					value: []string{"value1"},
				},
				{
					key:   "key[]",
					value: []string{""},
				},
			},
			wantVal: dataTree{
				"options": dataTree{
					"id":    "id1",
					"value": "value1",
				},
			},
		},
		{
			name: "there should be error if dataTree goes before scalar value",
			values: orderedData{
				{
					key:   "key[options][id]",
					value: []string{"id1"},
				},
				{
					key:   "key[options][value]",
					value: []string{"value1"},
				},
				{
					key:   "key",
					value: []string{"value"},
				},
			},
			wantErr: errors.New("invalid multiple values to key 'key' in tree"),
		},
		{
			name: "there should be error if scalar value goes before dataTree",
			values: orderedData{
				{
					key:   "key",
					value: []string{"value"},
				},
				{
					key:   "key[options][id]",
					value: []string{"id1"},
				},
				{
					key:   "key[options][value]",
					value: []string{"value1"},
				},
			},
			wantErr: errors.New("invalid multiple values to key 'key' in tree"),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			var (
				d   = make(dataTree)
				err error
			)

			for _, v := range tt.values {
				err = d.push(v.key, v.value)
				if err != nil {
					break
				}
			}
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("want err %+v but got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("want err %+v but got err %+v", tt.wantErr, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("want no err but got err %+v", err)
			}
			if diff := cmp.Diff(d["key"], tt.wantVal); len(diff) > 0 {
				t.Fatalf("diff should be empty: %+v", diff)
			}
		})
	}
}

func TestFileTreePush(t *testing.T) {
	type orderedData []struct {
		key   string
		value []*FileUpload
	}
	testCases := []struct {
		name    string
		values  orderedData
		wantVal any
		wantErr error
	}{
		{
			name: "non associated array should stay",
			values: orderedData{
				{
					key: "key[]",
					value: []*FileUpload{
						{
							Name: "value2",
						},
					},
				},
				{
					key:   "key",
					value: []*FileUpload{},
				},
			},
			wantVal: []*FileUpload{
				{
					Name: "value2",
				},
			},
		},
		{
			name: "old value should get overwritten by not empty value",
			values: orderedData{
				{
					key: "key[]",
					value: []*FileUpload{
						{
							Name: "value2",
						},
					},
				},
				{
					key: "key",
					value: []*FileUpload{
						{
							Name: "value1",
						},
					},
				},
			},
			wantVal: &FileUpload{Name: "value1"},
		},
		{
			name: "empty value should get overwritten by new fileTree",
			values: orderedData{
				{
					key:   "key",
					value: []*FileUpload{},
				},
				{
					key: "key[options][id]",
					value: []*FileUpload{
						{
							Name: "id1",
						},
					},
				},
				{
					key: "key[options][value]",
					value: []*FileUpload{
						{
							Name: "value1",
						},
					},
				},
			},
			wantVal: fileTree{
				"options": fileTree{
					"id":    &FileUpload{Name: "id1"},
					"value": &FileUpload{Name: "value1"},
				},
			},
		},
		{
			name: "fileTree should not get overwritten by empty string",
			values: orderedData{
				{
					key: "key[options][id]",
					value: []*FileUpload{
						{
							Name: "id1",
						},
					},
				},
				{
					key: "key[options][value]",
					value: []*FileUpload{
						{
							Name: "value1",
						},
					},
				},
				{
					key:   "key[]",
					value: []*FileUpload{},
				},
			},
			wantVal: fileTree{
				"options": fileTree{
					"id":    &FileUpload{Name: "id1"},
					"value": &FileUpload{Name: "value1"},
				},
			},
		},
		{
			name: "there should be error if both fileTree and file upload present #1",
			values: orderedData{
				{
					key: "key[options][id]",
					value: []*FileUpload{
						{
							Name: "id1",
						},
					},
				},
				{
					key: "key[options][value]",
					value: []*FileUpload{
						{
							Name: "value1",
						},
					},
				},
				{
					key: "key",
					value: []*FileUpload{
						{
							Name: "value",
						},
					},
				},
			},
			wantErr: errors.New("invalid multiple values to key 'key' in tree"),
		},
		{
			name: "there should be error if both fileTree and scalar value present #2",
			values: orderedData{
				{
					key: "key",
					value: []*FileUpload{
						{
							Name: "value",
						},
					},
				},
				{
					key: "key[options][id]",
					value: []*FileUpload{
						{
							Name: "id1",
						},
					},
				},
				{
					key: "key[options][value]",
					value: []*FileUpload{
						{
							Name: "value1",
						},
					},
				},
			},
			wantErr: errors.New("invalid multiple values to key 'key' in tree"),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			var (
				d   = make(fileTree)
				err error
			)

			for _, v := range tt.values {
				err = d.push(v.key, v.value)
				if err != nil {
					break
				}
			}
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("want err %+v but got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("want err %+v but got err %+v", tt.wantErr, err)
				}

				return
			}
			if diff := cmp.Diff(d["key"], tt.wantVal, cmpopts.IgnoreUnexported(FileUpload{})); len(diff) > 0 {
				t.Fatalf("diff should be empty: %+v", diff)
			}
		})
	}
}
