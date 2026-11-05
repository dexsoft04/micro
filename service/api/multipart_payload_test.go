package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/micro/micro/v3/service/api"
)

func multipartPayloadRequest(t *testing.T, payload []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, value := range []string{"first", "second"} {
		if err := w.WriteField("tag", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []struct {
		field string
		data  []byte
	}{{"upload", payload}, {"upload", []byte("ignored second file")}, {"empty", []byte{}}} {
		part, err := w.CreateFormFile(file.field, file.field+".txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/fixture/echo", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func assertMultipartPayload(t *testing.T, data, want []byte) {
	t.Helper()
	var got struct {
		Tag    string `json:"tag"`
		Upload []byte `json:"upload"`
		Empty  []byte `json:"empty"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tag != "first,second" || !bytes.Equal(got.Upload, want) || len(got.Empty) != 0 {
		t.Fatalf("multipart payload changed: tag=%q upload_bytes=%d empty_bytes=%d", got.Tag, len(got.Upload), len(got.Empty))
	}
}

func assertNoMultipartFiles(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "multipart-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("temporary multipart files remain: %v", files)
	}
}

func TestMultipartPayloadInMemory(t *testing.T) {
	payload := []byte("normal file contents")
	r := multipartPayloadRequest(t, payload)
	data, err := api.RequestPayload(r)
	if err != nil {
		t.Fatal(err)
	}
	assertMultipartPayload(t, data, payload)
}

func TestMultipartPayloadClonedRequestCleansOwnedForm(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	payload := bytes.Repeat([]byte("a"), (10<<20)+1)
	for _, parseError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "query_parse_error"}[parseError], func(t *testing.T) {
			original := multipartPayloadRequest(t, payload)
			if parseError {
				// ParseMultipartForm still builds the form after ParseForm fails.
				original.URL.RawQuery = "tag=%zz"
			}
			cloned := original.Clone(context.Background())
			data, err := api.RequestPayload(cloned)
			if parseError {
				if err == nil || data != nil || !strings.Contains(err.Error(), "invalid URL escape") {
					t.Fatalf("expected original query parse error without payload, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assertMultipartPayload(t, data, payload)
			}
			if original.MultipartForm != nil || cloned.MultipartForm == nil {
				t.Fatal("fixture did not exercise a form owned only by the cloned request")
			}
			assertNoMultipartFiles(t, dir)
		})
	}
}

func assertNoOpenMultipartFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(target, filepath.Join(dir, "multipart-")) {
			t.Fatalf("multipart file still open on fd %s: %s", entry.Name(), target)
		}
	}
}

func TestMultipartPayloadPreservesCallerFormAndClosesFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("immediate file descriptor assertion uses Linux procfs")
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	// Prove explicit Close rather than finalizer cleanup. This test is serial.
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	payload := []byte("normal disk-backed contents")
	r := multipartPayloadRequest(t, payload)
	if err := r.ParseMultipartForm(0); err != nil {
		t.Fatal(err)
	}
	defer r.MultipartForm.RemoveAll()
	owned := r.MultipartForm
	data, err := api.RequestPayload(r)
	if err != nil {
		t.Fatal(err)
	}
	assertMultipartPayload(t, data, payload)
	assertNoOpenMultipartFiles(t, dir)
	if r.MultipartForm != owned {
		t.Fatal("caller-owned form replaced")
	}
	f, _, err := r.FormFile("upload")
	if err != nil {
		t.Fatalf("caller-owned temporary file removed: %v", err)
	}
	read, readErr := io.ReadAll(f)
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(read, payload) {
		t.Fatalf("caller cannot reuse form: read=%v close=%v", readErr, closeErr)
	}
	assertNoOpenMultipartFiles(t, dir)
}

func TestMultipartPayloadClosesFileOnReadError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("directory read error and file descriptor assertion use Linux")
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	r := multipartPayloadRequest(t, []byte("normal disk-backed contents"))
	if err := r.ParseMultipartForm(0); err != nil {
		t.Fatal(err)
	}
	defer r.MultipartForm.RemoveAll()
	files, err := filepath.Glob(filepath.Join(dir, "multipart-*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("expected temporary files, files=%v error=%v", files, err)
	}
	// A locally controlled file becoming a directory lets Open succeed and
	// Read fail, without replacing filesystem or multipart internals.
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(file, 0700); err != nil {
			t.Fatal(err)
		}
	}
	data, err := api.RequestPayload(r)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "read" || data != nil {
		t.Fatalf("expected original file read error without payload, got %v", err)
	}
	assertNoOpenMultipartFiles(t, dir)
	for _, file := range files {
		if info, err := os.Stat(file); err != nil || !info.IsDir() {
			t.Fatalf("caller-owned temporary path removed: %v", err)
		}
	}
}
