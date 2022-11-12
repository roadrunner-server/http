package handler

import (
	"net/url"
	"reflect"
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

func TestPushWithOneLevelPostWithOverwrite(t *testing.T) {
	form := url.Values{}
	form.Add("key", "value")
	form.Add("key", "value2")
	form.Add("name[]", "name1")
	form.Add("name[]", "name2")
	form.Add("name[]", "name3")
	form.Add("arr[x][y][z]", "y")
	form.Add("arr[x][y][e]", "f")
	form.Add("arr[c]p", "l")
	form.Add("arr[c]z", "")

	var (
		d   = make(dataTree)
		err error
	)
	for k, v := range form {
		err = d.push(k, v)
		if err != nil {
			t.Fatal(err)
		}
	}

	if !reflect.DeepEqual(d["key"], "value2") {
		t.Fatalf("overwriting expected. tree is %+v", d)
	}
}

func TestPushWithMultipleLevelPostDataNoErr(t *testing.T) {
	postForm := url.Values{
		"id": []string{
			"97b27435-38e3-44d2-b97b-89d82fd6c212",
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
	}

	// request has empty plain value to same key
	// which will have structured value later
	d := make(dataTree)
	err := d.push("options", []string{""})
	if err != nil {
		t.Fatalf("%+v", err)
	}
	for k, v := range postForm {
		err = d.push(k, v)
		if err != nil {
			t.Fatal(err)
		}
	}
	optionDataTree := d["options"].(dataTree)
	if len(optionDataTree) != 2 {
		t.Fatalf("invalid length of options: %+v", optionDataTree)
	}
	for k, v := range optionDataTree {
		if len(v.(dataTree)) != 3 {
			t.Fatalf("invalid length of options[%s]: %+v", k, v)
		}
	}

	// request has empty array value to a key,
	// which will have structured value later
	d = make(dataTree)
	err = d.push("options[]", []string{})
	if err != nil {
		t.Fatalf("%+v", err)
	}
	for k, v := range postForm {
		err = d.push(k, v)
		if err != nil {
			t.Fatal(err)
		}
	}

	// request has empty array value to a key,
	// which previously have structured value
	d = make(dataTree)
	for k, v := range postForm {
		err = d.push(k, v)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = d.push("options[]", []string{})
	if err != nil {
		t.Fatalf("%+v", err)
	}
}

func TestPushWithMultipleLevelPostDataWithErr(t *testing.T) {
	postForm := url.Values{
		"id": []string{
			"97b27435-38e3-44d2-b97b-89d82fd6c212",
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
	}

	var (
		d   = make(dataTree)
		err error
	)
	for k, v := range postForm {
		err = d.push(k, v)
		if err != nil {
			t.Fatalf("%+v", err)
		}
	}
	err = d.push("options", []string{"invalid-data"})
	if err == nil {
		t.Fatal("there should have error")
	}
	t.Logf("got err: %+v", err)

	d = make(dataTree)
	err = d.push("options", []string{"invalid-data"})
	if err != nil {
		t.Fatalf("%+v", err)
	}
	for k, v := range postForm {
		err = d.push(k, v)
		if err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("there should have error")
	}
	t.Logf("got err: %+v", err)
}

func TestPushWithMultipleLevelFileUploadNoErr(t *testing.T) {
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
		err := d.push(k, v)
		if err != nil {
			t.Fatal(err)
		}
	}
	optionFileTree := d["options"].(fileTree)
	if len(optionFileTree) != 2 {
		t.Fatalf("invalid length of options: %+v", optionFileTree)
	}
	for k, v := range optionFileTree {
		if len(v.(fileTree)) != 3 {
			t.Fatalf("invalid length of options[%s]: %+v", k, d)
		}
	}
}

func TestPushWithMultipleLevelFileUploadWithErr(t *testing.T) {
	postForm := map[string][]*FileUpload{
		"id": {
			&FileUpload{
				Name: "file-upload-id",
			},
		},
		"options": {
			&FileUpload{
				Name: "file-upload-root-options",
			},
		},
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

	var (
		d   = make(fileTree)
		err error
	)
	for k, v := range postForm {
		err = d.push(k, v)
		if err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("there should have error")
	}
	t.Logf("got err: %+v", err)
}
