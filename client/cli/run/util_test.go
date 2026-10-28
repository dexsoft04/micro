package runtime

import (
	stdtar "archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pb "github.com/micro/micro/v3/proto/runtime"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/runtime"
	"github.com/micro/micro/v3/service/runtime/source/git"
	"github.com/urfave/cli/v2"
)

type uploadTestClient struct {
	client.Client
	stream *uploadTestStream
}

func (c *uploadTestClient) Stream(context.Context, client.Request, ...client.CallOption) (client.Stream, error) {
	return c.stream, nil
}

type uploadTestStream struct {
	client.Stream
	data bytes.Buffer
}

func (s *uploadTestStream) Send(message interface{}) error {
	_, err := s.data.Write(message.(*pb.UploadRequest).Data)
	return err
}

func (s *uploadTestStream) Close() error { return nil }

func (s *uploadTestStream) Recv(message interface{}) error {
	message.(*pb.UploadResponse).Id = "source://test"
	return nil
}

func TestUploadFileListMatchesSentArchive(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":             "module example.com/app\ngo 1.16\n",
		"main.go":            "package main\nimport _ \"embed\"\n//go:embed embedded.md\nvar data string\nfunc main() {}\n",
		"embedded.md":        "embedded markdown",
		"runtime.txt":        "runtime data",
		"README.md":          "excluded docs",
		"app.log":            "excluded logs",
		".hidden/data.txt":   "excluded data",
		"vendor/modules.txt": "vendor metadata",
		"vendor/dep/dep.go":  "package dep\n",
		"vendor/dep/app.log": "excluded logs",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	original := client.DefaultClient
	t.Cleanup(func() { client.DefaultClient = original })
	wantFiles := []string{"embedded.md", "go.mod", "main.go", "runtime.txt", "vendor/dep/dep.go", "vendor/modules.txt"}
	wantOutput := "Files to upload (6):\n  " + strings.Join(wantFiles, "\n  ") + "\n"
	var baseline []byte
	for _, tc := range []struct {
		name string
		show bool
		repo bool
	}{
		{name: "default", repo: true},
		{name: "repo file list", show: true, repo: true},
		{name: "standalone file list", show: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &uploadTestStream{}
			client.DefaultClient = &uploadTestClient{Client: original, stream: stream}
			app := cli.NewApp()
			var output bytes.Buffer
			app.Writer = &output
			app.Flags = flags
			app.Action = func(ctx *cli.Context) error {
				source := &git.Source{Local: true, FullPath: root}
				if tc.repo {
					source.LocalRepoRoot = root
					// The archive is rooted at the repo, not the selected subdirectory.
					source.FullPath = filepath.Join(root, "vendor", "dep")
				}
				id, err := upload(ctx, &runtime.Service{Name: "test", Version: "latest"}, source)
				if err == nil && id != "source://test" {
					t.Fatalf("unexpected source ID: %q", id)
				}
				return err
			}
			args := []string{"mcbeam"}
			if tc.show {
				args = append(args, "--show-upload-files")
			}
			if err := app.Run(args); err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.show {
				want = wantOutput
			}
			if output.String() != want {
				t.Fatalf("file list = %q, want %q", output.String(), want)
			}
			var gotFiles []string
			reader := stdtar.NewReader(bytes.NewReader(stream.data.Bytes()))
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if !header.FileInfo().IsDir() {
					gotFiles = append(gotFiles, header.Name)
				}
			}
			if !reflect.DeepEqual(gotFiles, wantFiles) {
				t.Fatalf("sent files = %v, want %v", gotFiles, wantFiles)
			}
			if baseline == nil {
				baseline = append([]byte(nil), stream.data.Bytes()...)
			} else if !bytes.Equal(stream.data.Bytes(), baseline) {
				t.Fatal("listing files changed the uploaded archive")
			}
		})
	}
}

func TestVendorWarnsForActiveWorkspace(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "module")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	for file, body := range map[string]string{
		filepath.Join(root, "go.mod"):  "module example.com/app\ngo 1.20\n",
		filepath.Join(root, "main.go"): "package main\nfunc main() {}\n",
		filepath.Join(base, "go.work"): "go 1.20\nuse ./module\n",
	} {
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOPROXY", "off")
	for _, mode := range []string{"", filepath.Join(base, "go.work"), "off"} {
		t.Run("GOWORK="+mode, func(t *testing.T) {
			t.Setenv("GOWORK", mode)
			var warnings bytes.Buffer
			err := vendorDependencies(root, &warnings)
			if mode == "off" {
				if err != nil || warnings.Len() != 0 {
					t.Fatalf("standalone vendor: %v, %s", err, &warnings)
				}
			} else {
				if !strings.Contains(warnings.String(), "Warning: active Go workspace "+filepath.Join(base, "go.work")) || !strings.Contains(warnings.String(), "will not be uploaded") {
					t.Fatalf("missing warning before vendor result %v: %s", err, &warnings)
				}
				if err == nil {
					t.Fatal("workspace must not be silently disabled")
				}
			}
		})
	}
}

func TestVendorWithoutWorkspaceDoesNotWarn(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.com/app\ngo 1.20\n", "main.go": "package main\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOWORK", "")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "")
	var warnings bytes.Buffer
	if err := vendorDependencies(root, &warnings); err != nil {
		t.Fatal(err)
	}
	if warnings.Len() != 0 {
		t.Fatalf("unexpected warning: %s", &warnings)
	}
}
