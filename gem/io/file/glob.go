package file

import (
	"os"
	"path/filepath"
	"strings"
)

func glob(patterns []string, flags int, base string, onFound func(f string)error) (err error) {
	if base == "" {
		base = "."
	}
	fd, err := os.Open(base)
	if err != nil {
		return err
	}

	files, err := fd.Readdir(-1)
	_= fd.Close()
	if err != nil {
		return err
	}

	if flags&fnmDotmatch != 0 {
		if err := checkDotDirs(base, patterns, flags, onFound); err != nil {
			return err
		}
	}

	for _, f := range files {
		fname := filepath.Join(base, f.Name())
		if err := checkFName(fname, patterns, flags, onFound); err != nil {
			return err
		}

		if f.IsDir() {
			for _, pattern := range patterns {
				if !strings.Contains(pattern,"**") && !strings.HasPrefix(pattern, fname) {
					continue
				}
				if err := glob([]string{pattern}, flags, fname, onFound); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkFName(name string, patterns []string, flags int, onFound func(f string)error) (err error) {
	for _, pattern := range patterns {
		ok, err := fnmatch(name, pattern, flags)
		if err != nil {
			return err
		}
		if ok {
			if err := onFound(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDotDirs(base string, patterns []string, flags int, onFound func(f string)error) (err error) {
	// Fix Golangs dotdot fix
	dot, dotdot := "/.", "/.."
	base = filepath.Clean(base)
	if base == "" || base == "." {
		base = ""
		dot, dotdot = ".", ".."
	}

	if err := checkFName(base + dot, patterns, flags, onFound); err != nil {
		return err
	}
	if err := checkFName(base + dotdot, patterns, flags, onFound); err != nil {
		return err
	}
	return nil
}

