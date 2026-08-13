package handler

import (
	"bytes"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

// uploadContent is the payload every fixture file carries.
const uploadContent = "content"

// newFileHeader round-trips a single part through the multipart reader so the
// resulting header carries a real backing store, the same as a served request.
func newFileHeader(t *testing.T, filename string) *multipart.FileHeader {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fw.Write([]byte(uploadContent)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	form, err := multipart.NewReader(&buf, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })

	return form.File["file"][0]
}

func TestFileUploadOpen_ForbiddenExtension_IsCaseInsensitive(t *testing.T) {
	f := NewUpload(newFileHeader(t, "x.PHP"), 0, 0)

	err := f.Open(t.TempDir(), map[string]struct{}{".php": {}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.Error != UploadErrorExtension {
		t.Errorf("Error = %d, want %d", f.Error, UploadErrorExtension)
	}
	if f.TempFilename != "" {
		t.Errorf("TempFilename = %q, want no temp file", f.TempFilename)
	}
}

func TestFileUploadOpen_NotInAllowList_Rejected(t *testing.T) {
	f := NewUpload(newFileHeader(t, "x.png"), 0, 0)

	err := f.Open(t.TempDir(), nil, map[string]struct{}{".jpg": {}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Error != UploadErrorExtension {
		t.Errorf("Error = %d, want %d", f.Error, UploadErrorExtension)
	}
	if f.TempFilename != "" {
		t.Errorf("TempFilename = %q, want no temp file", f.TempFilename)
	}
}

func TestFileUploadOpen_InAllowList_CopiedToTempDir(t *testing.T) {
	dir := t.TempDir()
	f := NewUpload(newFileHeader(t, "x.JPG"), 0, 0)

	err := f.Open(dir, nil, map[string]struct{}{".jpg": {}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Error != UploadErrorOK {
		t.Fatalf("Error = %d, want %d", f.Error, UploadErrorOK)
	}
	if f.Size != int64(len(uploadContent)) {
		t.Errorf("Size = %d, want %d", f.Size, len(uploadContent))
	}

	data, err := os.ReadFile(f.TempFilename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != uploadContent {
		t.Errorf("temp file = %q, want %q", data, uploadContent)
	}
}

func TestFileUploadOpen_MissingTempDir_ReportsNoTmpDir(t *testing.T) {
	f := NewUpload(newFileHeader(t, "x.txt"), 0, 0)

	err := f.Open(filepath.Join(t.TempDir(), "does-not-exist"), nil, nil)
	if err == nil {
		t.Fatal("expected an error from os.CreateTemp")
	}
	if f.Error != UploadErrorNoTmpDir {
		t.Errorf("Error = %d, want %d", f.Error, UploadErrorNoTmpDir)
	}
}

func TestFileUploadOpen_ChownDenied_ReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may chown to any uid, the failure arm is unreachable")
	}

	// uid/gid 1 is a system account; chowning to it requires privileges.
	f := NewUpload(newFileHeader(t, "x.txt"), 1, 1)

	if err := f.Open(t.TempDir(), nil, nil); err == nil {
		t.Fatal("expected an error from Chown")
	}
}

func TestUploadsOpen_LogsPerFileErrors(t *testing.T) {
	u := &Uploads{list: []*FileUpload{NewUpload(newFileHeader(t, "x.txt"), 0, 0)}}

	u.Open(slog.New(slog.DiscardHandler), filepath.Join(t.TempDir(), "does-not-exist"), nil, nil)

	if u.list[0].Error != UploadErrorNoTmpDir {
		t.Errorf("Error = %d, want %d", u.list[0].Error, UploadErrorNoTmpDir)
	}
}

func TestUploadsClear_RemovesTempFiles(t *testing.T) {
	dir := t.TempDir()

	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A non-empty directory cannot be removed, which drives the error branch.
	stubborn := filepath.Join(dir, "stubborn")
	if err := os.MkdirAll(filepath.Join(stubborn, "child"), 0o700); err != nil {
		t.Fatal(err)
	}

	u := &Uploads{list: []*FileUpload{
		{TempFilename: ""},
		{TempFilename: filepath.Join(dir, "never-created")},
		{TempFilename: present},
		{TempFilename: stubborn},
	}}

	u.Clear(slog.New(slog.DiscardHandler))

	if exists(present) {
		t.Error("the temp file was not removed")
	}
	if !exists(stubborn) {
		t.Error("the non-empty directory was unexpectedly removed")
	}
}

func TestUploadsMarshalJSON_EmitsTree(t *testing.T) {
	u := &Uploads{tree: fileTree{"f": &FileUpload{Name: "x.txt"}}}

	data, err := u.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"name":"x.txt"`)) {
		t.Errorf("MarshalJSON() = %s, want the file name", data)
	}
}

func TestExists_ReportsPresence(t *testing.T) {
	if exists(filepath.Join(t.TempDir(), "definitely-not-here")) {
		t.Error("exists() reported a missing path as present")
	}
	if !exists(t.TempDir()) {
		t.Error("exists() reported an existing directory as missing")
	}
}
