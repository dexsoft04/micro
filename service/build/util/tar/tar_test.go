package tar

import (
	stdtar "archive/tar"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeSources(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func archiveFiles(t *testing.T, root string) []string {
	t.Helper()
	src, err := Archive(root)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	tr := stdtar.NewReader(src)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, header.Name)
	}
	return files
}

func TestArchiveExcludesUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	writeSources(t, root, map[string]string{
		"main.go":                             "package main\nfunc main() {}\n",
		"go.mod":                              "module example.com/app\ngo 1.16\n",
		"go.sum":                              "",
		"proto/service.pb.go":                 "package proto\n",
		"native/func.s":                       "assembly input",
		"native/func.h":                       "header input",
		"native/func.c":                       "C input",
		"native/runtime.syso":                 "system object input",
		"vendor/modules.txt":                  "vendor metadata",
		"vendor/example.com/lib/lib.go":       "package lib\n",
		"vendor/example.com/lib/LICENSE.txt":  "license",
		"vendor/example.com/lib/README.md":    "docs",
		"vendor/example.com/lib/debug.log":    strings.Repeat("log", 1024),
		"vendor/example.com/lib/lib_test.go":  "package lib\n",
		"vendor/example.com/lib/.cache/x.go":  "package cache\n",
		"vendor/example.com/lib/.cache/x.txt": "cache",
		"vendorish/unneeded.bin":              "not a dependency",
		"README.md":                           "docs",
		"docs/guide.md":                       "docs",
		"logs/server.log":                     strings.Repeat("log", 1024),
		"notes.txt":                           "notes",
		"_data.txt":                           "runtime data",
		"notgo.mod":                           "not module metadata",
		"notgo.sum":                           "not module metadata",
		"main_test.go":                        "package main\n",
		"_scratch.go":                         "package main\n",
		".hidden.go":                          "package main\n",
		".git/objects/payload.go":             "package main\n",
		".agents/scratch.go":                  "package main\n",
		".vscode/settings.json":               "{}",
		"sub/.hidden/scratch.go":              "package main\n",
		"node_modules/pkg/main.go":            "package main\n",
		"bin/mcbeam":                          "binary",
		"empty/.keep":                         "",
	})
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"_data.txt", "go.mod", "go.sum", "main.go", "native", "native/func.c", "native/func.h", "native/func.s", "native/runtime.syso", "notes.txt",
		"proto", "proto/service.pb.go", "vendor", "vendor/example.com", "vendor/example.com/lib",
		"vendor/example.com/lib/LICENSE.txt", "vendor/example.com/lib/lib.go", "vendor/modules.txt",
	}
	if got := archiveFiles(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("archive entries = %v, want %v", got, want)
	}
}

func TestArchivePreservesEmbeddedAssets(t *testing.T) {
	root := t.TempDir()
	writeSources(t, root, map[string]string{
		"main.go":                  "package main\nimport \"embed\"\n//go:embed assets templates/*.md \"with space.txt\" all:extra\nvar data embed.FS\nfunc main() {}\n",
		"assets/data.json":         "{}",
		"assets/nested/help.txt":   "help",
		"assets/.hidden":           "excluded",
		"assets/_private":          "excluded",
		"assets/.git/config":       "excluded",
		"assets/submodule/go.mod":  "module example.com/nested\ngo 1.16\n",
		"assets/submodule/data.md": "excluded",
		"templates/page.md":        "embedded markdown",
		"with space.txt":           "embedded text",
		"extra/.hidden.txt":        "explicit all pattern",
		"extra/_private.txt":       "explicit all pattern",
		"extra/.git/config":        "excluded even with all",
		"unreferenced.md":          "excluded",
		"embed_test.go":            "package main\nimport _ \"embed\"\n//go:embed test-only.log\nvar testData string\n",
		"test-only.log":            "excluded",
		"data_windows.go":          "package main\nimport _ \"embed\"\n//go:embed windows.json\nvar windowsData string\n",
		"windows.json":             "cross-platform input",
		"vendor/dep/embed.go":      "package dep\nimport _ \"embed\"\n//go:embed schema.sql\nvar schema string\n",
		"vendor/dep/schema.sql":    "select 1;",
		"vendor/dep/README.md":     "excluded",
	})
	want := []string{
		"assets", "assets/data.json", "assets/nested", "assets/nested/help.txt",
		"assets/submodule", "assets/submodule/go.mod",
		"data_windows.go", "extra", "extra/.hidden.txt", "extra/_private.txt", "main.go",
		"templates", "templates/page.md", "vendor", "vendor/dep", "vendor/dep/embed.go", "vendor/dep/schema.sql",
		"windows.json", "with space.txt",
	}
	if got := archiveFiles(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("archive entries = %v, want %v", got, want)
	}
}

func TestArchiveBuildRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeSources(t, root, map[string]string{
		"go.mod":         "module example.com/app\ngo 1.16\n",
		"main.go":        "package main\nimport (\"embed\"; \"fmt\"; \"os\")\n//go:embed data\nvar files embed.FS\nfunc main() { b, err := files.ReadFile(\"data/result.md\"); if err != nil { panic(err) }; txt, err := os.ReadFile(\"runtime.txt\"); if err != nil { panic(err) }; fmt.Print(string(b) + string(txt)) }\n",
		"data/result.md": "embedded output",
		"runtime.txt":    " and runtime text",
		"README.md":      "unrelated docs",
		"server.log":     "unrelated logs",
	})
	src, err := Archive(root)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Unarchive(src, dst); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dst, "app"), ".")
	cmd.Dir = dst
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build archived source: %v\n%s", err, out)
	}
	run := exec.Command(filepath.Join(dst, "app"))
	run.Dir = dst
	out, err := run.CombinedOutput()
	if err != nil || string(out) != "embedded output and runtime text" {
		t.Fatalf("run archived source: %v, output %q", err, out)
	}
	for _, name := range []string{"README.md", "server.log"} {
		if _, err := os.Stat(filepath.Join(dst, name)); !os.IsNotExist(err) {
			t.Fatalf("unrelated file %s survived round trip: %v", name, err)
		}
	}
}

func TestArchiveEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	writeSources(t, root, map[string]string{"docs/README.md": "excluded"})
	if got := archiveFiles(t, root); len(got) != 0 {
		t.Fatalf("unexpected archive entries: %v", got)
	}
}
