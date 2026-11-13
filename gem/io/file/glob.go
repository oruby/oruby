package file

import (
	"os"
	"strings"
)

func glob(patterns []string, flags int, base string, onFound func(f string) error) (err error) {
	for _, pattern := range patterns {
		pBase := base
		if pBase == "" {
			magic := strings.IndexAny(pattern, "*?[{")
			if magic >= 0 {
				lastSlash := strings.LastIndex(pattern[:magic], "/")
				if lastSlash >= 0 {
					pBase = pattern[:lastSlash]
					if pBase == "" {
						pBase = "/"
					}
				}
			}
		}
		if err := globInternal(pattern, flags, pBase, onFound); err != nil {
			return err
		}
	}
	return nil
}

func globInternal(pattern string, flags int, base string, onFound func(f string) error) error {
	openDir := base
	if openDir == "" {
		openDir = "."
	}
	fd, err := os.Open(openDir)
	if err != nil {
		return nil
	}

	files, err := fd.Readdir(-1)
	_ = fd.Close()
	if err != nil {
		return nil
	}

	if flags&fnmDotmatch != 0 {
		if err := checkDotDirs(base, []string{pattern}, flags, onFound); err != nil {
			return err
		}
	}

	for _, f := range files {
		fname := f.Name()
		if base != "" {
			if base == "/" {
				fname = "/" + f.Name()
			} else {
				fname = base + "/" + f.Name()
			}
		}

		if err := checkFName(fname, []string{pattern}, flags, onFound); err != nil {
			return err
		}

		if f.IsDir() {
			if strings.Contains(pattern, "**") || strings.HasPrefix(pattern, fname) {
				if err := globInternal(pattern, flags, fname, onFound); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkFName(name string, patterns []string, flags int, onFound func(f string) error) (err error) {
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

func checkDotDirs(base string, patterns []string, flags int, onFound func(f string) error) (err error) {
	dot, dotdot := "/.", "/.."
	if base == "" || base == "." {
		base = ""
		dot, dotdot = ".", ".."
	}

	if err := checkFName(base+dot, patterns, flags, onFound); err != nil {
		return err
	}
	if err := checkFName(base+dotdot, patterns, flags, onFound); err != nil {
		return err
	}
	return nil
}
