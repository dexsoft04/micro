// Package tar is a wrapper around archive/tar, it's used for archiving and unarchiving source from
// and into folders on the host. Because runtime has many cases where it streams source via RPC,
// this package encapsulates the shared logic.
package tar

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
)

// Unarchive decodes the source in a tar and writes it to a directory
func Unarchive(src io.Reader, dir string) error {
	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		path := filepath.Join(dir, hdr.Name)
		bytes, err := ioutil.ReadAll(tr)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if _, verr := os.Stat(path); os.IsNotExist(verr) {
				err = os.Mkdir(path, os.ModePerm)
			}
		case tar.TypeReg:
			err = ioutil.WriteFile(path, bytes, os.ModePerm)
		default:
			err = fmt.Errorf("Unknown tar header type flag: %v", string(hdr.Typeflag))
		}

		if err != nil {
			return err
		}
	}
	return nil
}

// Archive a local directory, including only Go build inputs and embedded assets.
func Archive(dir string) (io.Reader, error) {
	files, err := sourceFiles(dir)
	if err != nil {
		return nil, err
	}

	// Create a tar writer and a buffer to store the archive
	tf := bytes.NewBuffer(nil)
	tw := tar.NewWriter(tf)
	defer tw.Close()

	// Include only directories needed by the selected files, in parent-first order.
	entries := make(map[string]bool)
	for file := range files {
		entries[file] = true
		for parent := filepath.Dir(file); parent != "."; parent = filepath.Dir(parent) {
			entries[parent] = true
		}
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}

		// generate and write tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}

		// Since os.FileInfo's Name method only returns the base name of the file it describes, it is
		// necessary to modify Header.Name to provide the full path name of the file. See:
		// https://golang.org/src/archive/tar/common.go?s=22088:22153#L626
		header.Name = filepath.ToSlash(name)

		// write the header to the archive
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}

		// there is no body if it's a directory
		if info.IsDir() {
			continue
		}

		// read the contents of the file
		bytes, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, err
		}

		// write the contents of the file to the tar
		if _, err := tw.Write(bytes); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	// Return the archive
	return tf, nil
}
