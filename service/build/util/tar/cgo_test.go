package tar

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestArchivePreservesCgoResourcesWithinSourceRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSources(t, outside, map[string]string{"liboutside.a": "must not upload"})
	writeSources(t, root, map[string]string{
		"native/cgo.go":                  "package native\n/*\n#cgo CFLAGS: -include ${SRCDIR}/generated/config.native\n#cgo LDFLAGS: -L${SRCDIR}/../support -L" + outside + " -L${SRCDIR}/../linked -L${SRCDIR}/.cache -Wl,--version-script=${SRCDIR}/../linker/version.native\n*/\nimport \"C\"\n",
		"native/libanswer.a":             "static library",
		"native/libanswer.so.1.2":        "versioned library",
		"native/libanswer.so.log":        "not a library",
		"native/answer.obj":              "object file",
		"native/_table.inc":              "auxiliary header",
		"native/generated/config.native": "explicitly referenced file",
		"native/README.md":               "excluded docs",
		"native/debug.log":               "retained conservatively",
		"native/go.work":                 "go 1.20\nuse ../outside\n",
		"native/go.work.sum":             "",
		"native/tool.exe":                "excluded executable",
		"native/tool":                    "excluded executable",
		"native/.cache/libsecret.a":      "excluded hidden directory",
		"native/other/other.go":          "package other\n",
		"native/other/unrelated.a":       "belongs to a separate non-CGO package",
		"support/libsupport.a":           "library search directory",
		"support/README.md":              "excluded docs",
		"support/build.log":              "excluded logs",
		"linker/version.native":          "explicit linker script",
		"unrelated/libunused.a":          "unreferenced library",
	})
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	// Exercise a relative root as well as absolute paths in CGO directives.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, root)
	if err != nil {
		t.Fatal(err)
	}
	got := archiveFiles(t, rel)
	for _, want := range []string{
		"native/libanswer.a", "native/libanswer.so.1.2", "native/answer.obj", "native/_table.inc",
		"native/generated/config.native", "support/libsupport.a", "linker/version.native",
		"native/libanswer.so.log", "native/README.md", "native/debug.log", "native/tool.exe", "native/tool",
		"support/README.md", "support/build.log",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing native build input %s in %v", want, got)
		}
	}
	for _, excluded := range []string{
		"native/.cache/libsecret.a", "native/other/unrelated.a",
		"unrelated/libunused.a", "linked/liboutside.a", "native/go.work", "native/go.work.sum",
	} {
		if slices.Contains(got, excluded) {
			t.Errorf("unexpected resource %s in %v", excluded, got)
		}
	}
}

func TestArchivePreservesConditionalCgoInputs(t *testing.T) {
	root := t.TempDir()
	source := "package native\r\n/*\r\n#cgo windows CFLAGS: -include ${SRCDIR}/../support/windows.native\r\n#cgo archive_custom_tag CFLAGS: -I${SRCDIR}/../tagged\r\n*/\r\nimport \"C\"\r\n"
	writeSources(t, root, map[string]string{
		"native/native.go":       source,
		"support/windows.native": "#define VALUE 1\n",
		"tagged/table.dat":       "#define VALUE 2\n",
		"native/line.go":         "package native\n// #cgo archive_line_tag LDFLAGS: ${SRCDIR}/../line/answer.o\nimport \"C\"\n",
		"line/answer.o":          "native object",
	})
	archive, err := Archive(root)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Unarchive(archive, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"support/windows.native", "tagged/table.dat", "line/answer.o"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dst, "native/native.go"))
	if err != nil || string(got) != source {
		t.Fatalf("archived source changed: %v", err)
	}
}

func TestArchiveCgoStaticLibraryBuildRoundTrip(t *testing.T) {
	for _, tool := range []string{"cc", "ar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("native toolchain unavailable: %s", tool)
		}
	}
	base := t.TempDir()
	root := filepath.Join(base, "source")
	dep := filepath.Join(base, "dependency")
	writeSources(t, dep, map[string]string{
		"go.mod": "module example.com/native\ngo 1.16\n",
		"native.go": `package native
/*
#cgo LDFLAGS: ${SRCDIR}/libanswer.a
#define ANSWER_HEADER "table.dat"
#include ANSWER_HEADER
#include "answer.inc"
*/
import "C"
func Answer() int { return int(C.answer()) + int(C.BONUS) }
`,
		"answer.inc": "int answer(void);\n",
		"table.dat":  "#define BONUS 1\n",
		"README.md":  "unrelated docs",
		"debug.log":  "unrelated logs",
	})
	writeSources(t, root, map[string]string{
		"go.mod":  "module example.com/app\ngo 1.16\nrequire example.com/native v0.0.0\nreplace example.com/native => ../dependency\n",
		"main.go": "package main\nimport (\"fmt\"; \"example.com/native\")\nfunc main() { fmt.Print(native.Answer()) }\n",
	})
	writeSources(t, base, map[string]string{"answer.c": "int answer(void) { return 42; }\n"})
	command := func(dir string, name string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=1", "CC=cc")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return out
	}
	command(base, "cc", "-c", "answer.c", "-o", "answer.o")
	command(base, "ar", "rcs", filepath.Join(dep, "libanswer.a"), "answer.o")
	command(root, "go", "mod", "vendor")
	command(root, "go", "build", "-mod=vendor", "-o", filepath.Join(base, "before"), ".")
	src, err := Archive(root)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Unarchive(src, dst); err != nil {
		t.Fatal(err)
	}
	command(dst, "go", "build", "-mod=vendor", "-o", filepath.Join(base, "after"), ".")
	if out := command(base, filepath.Join(base, "after")); strings.TrimSpace(string(out)) != "43" {
		t.Fatalf("unexpected native library result: %q", out)
	}
	for _, name := range []string{"README.md", "debug.log"} {
		if _, err := os.Stat(filepath.Join(dst, "vendor", "example.com", "native", name)); err != nil {
			t.Fatalf("CGO package resource was not preserved: %s (%v)", name, err)
		}
	}
}
