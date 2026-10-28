package tar_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/micro/micro/v3/service/build"
	"github.com/micro/micro/v3/service/build/golang"
	"github.com/micro/micro/v3/service/build/util/tar"
)

func TestArchiveBuildIgnoresExternalWorkspace(t *testing.T) {
	t.Setenv("GOWORK", "")
	t.Setenv("GOFLAGS", "")
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	for name, body := range map[string]string{
		"repo/go.mod":      "module example.com/app\ngo 1.20\n",
		"repo/main.go":     "package main\nfunc main() {}\n",
		"repo/go.work":     "go 1.20\nuse (\n.\n../sibling\n)\n",
		"repo/go.work.sum": "",
		"sibling/go.mod":   "module example.com/sibling\ngo 1.20\n",
	} {
		file := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	source, err := tar.Archive(root)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := golang.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(source, build.Archive("tar")); err != nil {
		t.Fatalf("build standalone module after archiving: %v", err)
	}
}
