package handler

import (
	"fmt"
	"net/url"
	"testing"
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

func TestPushWithMultipleLevelPostData(t *testing.T) {
	postForm := url.Values{
		"id": []string{
			"97b27435-38e3-44d2-b97b-89d82fd6c212",
		},
		"options": []string{
			"",
		},
		"options[0][id]": []string{
			"97b27435-3cb7-40f1-9637-e406465e63ed",
		},
		"options[0][name]": []string{
			"Reiciendis et impedit quod id.",
		},
		"options[0][value]": []string{
			"",
		},
		"options[1][id]": []string{
			"97b27435-3d8a-4f68-b034-88ef3b6cd161",
		},
		"options[1][name]": []string{
			"Mollitia aut assumenda non tempora.",
		},
		"options[1][value]": []string{
			"",
		},
		"options[2][id]": []string{
			"97b27435-3e5a-461a-abee-3c7b3d50ef14",
		},
		"options[2][value]": []string{
			"",
		},
		"options[2][name]": []string{
			"Libero ipsa doloremque non rerum enim.",
		},
	}

	d := make(dataTree)
	for k, v := range postForm {
		d.push(k, v)
	}
	optionDataTree := d["options"].(dataTree)
	if len(optionDataTree) != 3 {
		t.Fatal(fmt.Sprintf("invalid length of options: %+v", optionDataTree))
	}
	for k, v := range optionDataTree {
		if len(v.(dataTree)) != 3 {
			t.Fatal(fmt.Sprintf("invalid length of options[%s]: %+v", k, v))
		}
	}
}

func TestPushWithMultipleLevelFileUpload(t *testing.T) {
	postForm := map[string][]*FileUpload{
		"id": {
			&FileUpload{
				Name: "file-upload-id",
			},
		},
		"options": nil,
		"options[0][id]": {
			&FileUpload{
				Name: "file-upload-0-id",
			},
		},
		"options[0][name]": {
			&FileUpload{
				Name: "file-upload-0-name",
			},
		},
		"options[0][value]": {
			nil,
		},
		"options[1][id]": {
			&FileUpload{
				Name: "file-upload-1-id",
			},
		},
		"options[1][name]": {
			&FileUpload{
				Name: "file-upload-1-name",
			},
		},
		"options[1][value]": {
			nil,
		},
	}

	d := make(fileTree)
	for k, v := range postForm {
		d.push(k, v)
	}
	optionFileTree := d["options"].(fileTree)
	if len(optionFileTree) != 2 {
		t.Fatal(fmt.Sprintf("invalid length of options: %+v", optionFileTree))
	}
	for k, v := range optionFileTree {
		if len(v.(fileTree)) != 3 {
			t.Fatal(fmt.Sprintf("invalid length of options[%s]: %+v", k, d))
		}
	}
}
