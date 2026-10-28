package tar

import (
	"bytes"
	"go/build"
	"go/scanner"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var cgoDirective = regexp.MustCompile(`#cgo[ \t]+([^:\r\n]+):`)

// Archive all conditional native inputs, not just those for the uploading host.
// Only the metadata parser sees rewritten comments; archived source is unchanged.
func archiveBuildContext() build.Context {
	ctx := build.Default
	open := ctx.OpenFile
	ctx.UseAllFiles = true
	ctx.CgoEnabled = true
	ctx.OpenFile = func(name string) (io.ReadCloser, error) {
		var data []byte
		var err error
		if open == nil {
			data, err = os.ReadFile(name)
		} else {
			var r io.ReadCloser
			r, err = open(name)
			if err == nil {
				data, err = io.ReadAll(r)
				r.Close()
			}
		}
		if err != nil {
			return nil, err
		}
		if filepath.Ext(name) != ".go" {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
		var scan scanner.Scanner
		file := token.NewFileSet().AddFile(name, -1, len(data))
		scan.Init(file, data, func(token.Position, string) {}, scanner.ScanComments)
		var result bytes.Buffer
		last := 0
		for {
			pos, kind, _ := scan.Scan()
			if kind == token.EOF {
				break
			}
			if kind != token.COMMENT {
				continue
			}
			start := file.Offset(pos)
			// Scanner strips CRs in comments; locate the original ending instead.
			end := len(data)
			if bytes.HasPrefix(data[start:], []byte("/*")) {
				n := bytes.Index(data[start:], []byte("*/"))
				if n < 0 {
					break
				}
				end = start + n + 2
			} else if n := bytes.IndexByte(data[start:], '\n'); n >= 0 {
				end = start + n
			} else {
				end = len(data)
			}
			result.Write(data[last:start])
			result.Write(cgoDirective.ReplaceAllFunc(data[start:end], func(line []byte) []byte {
				fields := strings.Fields(strings.TrimSuffix(string(line), ":"))
				switch flag := fields[len(fields)-1]; flag {
				case "CFLAGS", "CPPFLAGS", "CXXFLAGS", "FFLAGS", "LDFLAGS", "pkg-config":
					return []byte("#cgo " + flag + ":")
				}
				return line
			}))
			last = end
		}
		result.Write(data[last:])
		return io.NopCloser(bytes.NewReader(result.Bytes())), nil
	}
	return ctx
}

func addCgoFiles(root, dir string, pkg *build.Package, packages, files map[string]bool) error {
	// Native dependencies in a CGO package need not be Go source files.
	if err := addNativePath(root, dir, false, packages, files); err != nil {
		return err
	}
	flags := append([]string{}, pkg.CgoCFLAGS...)
	flags = append(flags, pkg.CgoCPPFLAGS...)
	flags = append(flags, pkg.CgoCXXFLAGS...)
	flags = append(flags, pkg.CgoFFLAGS...)
	flags = append(flags, pkg.CgoLDFLAGS...)
	var expanded []string
	for _, flag := range flags {
		if strings.HasPrefix(flag, "-Wl,") {
			expanded = append(expanded, strings.Split(strings.TrimPrefix(flag, "-Wl,"), ",")...)
		} else if flag != "-Xlinker" {
			expanded = append(expanded, flag)
		}
	}
	flags = expanded
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		var name string
		switch flag {
		case "-I", "-L", "-F", "-isystem", "-iquote", "-idirafter", "-include", "-imacros", "-T", "--script", "--version-script":
			if i+1 < len(flags) {
				i++
				name = flags[i]
			}
		default:
			switch {
			case strings.HasPrefix(flag, "-I"), strings.HasPrefix(flag, "-L"), strings.HasPrefix(flag, "-F"):
				name = flag[2:]
			case strings.HasPrefix(flag, "--script="), strings.HasPrefix(flag, "--version-script="):
				_, name, _ = strings.Cut(flag, "=")
			case !strings.HasPrefix(flag, "-"):
				name = flag
			}
		}
		if name == "" {
			continue
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		if err := addNativePath(root, name, true, packages, files); err != nil {
			return err
		}
	}
	return nil
}

func addNativePath(root, name string, explicit bool, packages, files map[string]bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return err
	}
	if filepath.VolumeName(root) != filepath.VolumeName(name) {
		return nil
	}
	rel, err := filepath.Rel(root, name)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// System libraries are supplied by the build host, not the source archive.
		return nil
	}
	// Reject symlinks and excluded directories along the entire referenced path.
	current := root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || (info.IsDir() && skipSourceDir(part)) {
				return nil
			}
		}
	}
	return filepath.WalkDir(name, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if file != name && (skipSourceDir(entry.Name()) || (!explicit && packages[file])) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		// Native includes may use arbitrary suffixes or macro-generated names.
		// Preserve the package/search-directory contents instead of guessing
		// dependencies from extensions. Workspace metadata remains local.
		if entry.Name() == "go.work" || entry.Name() == "go.work.sum" {
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		files[rel] = true
		return nil
	})
}
