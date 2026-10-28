package tar

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func shouldArchive(file string) bool {
	name := filepath.Base(file)
	if strings.HasPrefix(name, ".") {
		return false
	}
	// Text files can be runtime data and include vendor/modules.txt.
	if filepath.Ext(name) == ".txt" {
		return true
	}
	if strings.HasPrefix(name, "_") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	switch name {
	case "go.mod", "go.sum":
		return true
	}
	// Go's native source formats, including assembly and CGO inputs.
	switch filepath.Ext(name) {
	case ".go", ".c", ".cc", ".cpp", ".cxx", ".m", ".h", ".hh", ".hpp", ".hxx",
		".f", ".F", ".for", ".f90", ".s", ".S", ".sx", ".swig", ".swigcxx", ".syso":
		return true
	}
	return false
}

func skipSourceDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__"
}

func sourceFiles(root string) (map[string]bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	files := make(map[string]bool)
	packages := make(map[string]bool)
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if file != root && skipSourceDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		if shouldArchive(rel) {
			files[rel] = true
			if filepath.Ext(file) == ".go" {
				packages[filepath.Dir(file)] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	ctx := archiveBuildContext()
	for dir := range packages {
		// ImportDir still returns patterns when mutually exclusive source files
		// declare different packages. The remote builder validates the source.
		pkg, _ := ctx.ImportDir(dir, 0)
		if len(pkg.CgoFiles) > 0 {
			if err := addCgoFiles(root, dir, pkg, packages, files); err != nil {
				return nil, fmt.Errorf("collect native build files in %s: %w", dir, err)
			}
		}
		for _, pattern := range pkg.EmbedPatterns {
			if err := addEmbeddedFiles(root, dir, pattern, files); err != nil {
				return nil, fmt.Errorf("collect embedded files in %s (%s): %w", dir, pattern, err)
			}
		}
	}
	return files, nil
}

func addEmbeddedFiles(root, dir, pattern string, files map[string]bool) error {
	all := strings.HasPrefix(pattern, "all:")
	glob := strings.TrimPrefix(pattern, "all:")
	if glob == "." || !fs.ValidPath(glob) {
		return fmt.Errorf("invalid embed pattern")
	}
	matches, err := fs.Glob(os.DirFS(dir), glob)
	if err != nil {
		return err
	}
	for _, match := range matches {
		// Never follow symbolic links or cross nested module/VCS boundaries.
		valid := true
		parts := strings.Split(match, "/")
		for i, part := range parts {
			if part == ".git" || part == ".hg" || part == ".svn" || part == ".bzr" {
				valid = false
				break
			}
			file := filepath.Join(dir, filepath.FromSlash(path.Join(parts[:i+1]...)))
			info, err := os.Lstat(file)
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				valid = false
				break
			}
			if info.IsDir() {
				if _, err := os.Stat(filepath.Join(file, "go.mod")); err == nil {
					valid = false
					break
				}
			}
		}
		if !valid {
			continue
		}
		err := fs.WalkDir(os.DirFS(dir), match, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			base := entry.Name()
			if name != match && (base == ".git" || base == ".hg" || base == ".svn" || base == ".bzr" ||
				(!all && (strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")))) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			file := filepath.Join(dir, filepath.FromSlash(name))
			if entry.IsDir() {
				if _, err := os.Stat(filepath.Join(file, "go.mod")); err == nil {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() {
				rel, err := filepath.Rel(root, file)
				if err != nil {
					return err
				}
				files[rel] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
