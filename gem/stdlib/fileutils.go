package stdlib

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oruby/oruby"
)

func init() {
	oruby.LazyGem("fileutils", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("FileUtils")
		def := func(name string, n int, f func(args []oruby.Value) error) {
			mod.DefineClassMethod(name, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
				if err := f(mrb.GetArgs().Slice()); err != nil {
					return mrb.RaiseError(err)
				}
				return mrb.NilValue()
			}, mrb.ArgsReq(uint32(n)))
		}
		str := func(v oruby.Value) string { return v.String() }
		def("__mkdir", 2, func(a []oruby.Value) error { return os.Mkdir(str(a[0]), fs.FileMode(a[1].Int())) })
		def("__mkdir_p", 2, func(a []oruby.Value) error { return os.MkdirAll(str(a[0]), fs.FileMode(a[1].Int())) })
		def("__rmdir", 1, func(a []oruby.Value) error { return syscall.Rmdir(str(a[0])) })
		def("__rm", 1, func(a []oruby.Value) error { return syscall.Unlink(str(a[0])) })
		def("__rm_r", 1, func(a []oruby.Value) error {
			if _, err := os.Lstat(str(a[0])); err != nil {
				return err
			}
			return os.RemoveAll(str(a[0]))
		})
		def("__cp", 3, func(a []oruby.Value) error { return copyFile(str(a[0]), str(a[1]), a[2].Bool()) })
		def("__cp_r", 4, func(a []oruby.Value) error { return copyTree(str(a[0]), str(a[1]), a[2].Bool(), a[3].Bool()) })
		def("__mv", 2, func(a []oruby.Value) error { return moveFile(str(a[0]), str(a[1])) })
		def("__ln", 2, func(a []oruby.Value) error { return os.Link(str(a[0]), str(a[1])) })
		def("__ln_s", 2, func(a []oruby.Value) error { return os.Symlink(str(a[0]), str(a[1])) })
		def("__chmod", 2, func(a []oruby.Value) error { return os.Chmod(str(a[1]), fs.FileMode(a[0].Int())) })
		def("__touch", 2, func(a []oruby.Value) error {
			name := str(a[0])
			t := time.Now()
			if !a[1].IsNil() {
				t = time.Unix(0, int64(a[1].Float64()*1e9))
			}
			if _, err := os.Stat(name); errors.Is(err, fs.ErrNotExist) {
				f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0666)
				if err != nil {
					return err
				}
				f.Close()
			}
			return os.Chtimes(name, t, t)
		})
		mod.DefineClassMethod("__identical", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			args := mrb.GetArgs()
			a, err1 := os.ReadFile(args.Item(0).String())
			b, err2 := os.ReadFile(args.Item(1).String())
			return mrb.BoolValue(err1 == nil && err2 == nil && bytes.Equal(a, b))
		}, mrb.ArgsReq(2))
		load(mrb, "fileutils")
		return nil
	})
}

func copyFile(src, dst string, preserve bool) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return &fs.PathError{Op: "cp", Path: src, Err: syscall.EISDIR}
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if preserve {
		os.Chmod(dst, fi.Mode().Perm())
		os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
	return nil
}

func copyTree(src, dst string, preserve, deref bool) error {
	stat := os.Lstat
	if deref {
		stat = os.Stat
	}
	fi, err := stat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case fi.IsDir():
		if err := os.MkdirAll(dst, fi.Mode().Perm()|0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), preserve, false); err != nil {
				return err
			}
		}
		if preserve {
			os.Chmod(dst, fi.Mode().Perm())
			os.Chtimes(dst, fi.ModTime(), fi.ModTime())
		}
		return nil
	}
	return copyFile(src, dst, preserve)
}

func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var le *os.LinkError
	if errors.As(err, &le) && errors.Is(le.Err, syscall.EXDEV) {
		if err := copyTree(src, dst, true, false); err != nil {
			return err
		}
		return os.RemoveAll(src)
	}
	return err
}
