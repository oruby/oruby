package stdlib

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
		def("__chmod", 2, func(a []oruby.Value) error { return chmodFile(str(a[1]), a[0]) })
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

func chmodFile(path string, mode oruby.Value) error {
	if mode.IsInteger() {
		return os.Chmod(path, fs.FileMode(mode.Int()))
	}
	if !mode.IsString() {
		return errors.New("mode must be an integer or symbolic string")
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	current := uint32(info.Mode().Perm())
	if info.Mode()&fs.ModeSetuid != 0 {
		current |= 04000
	}
	if info.Mode()&fs.ModeSetgid != 0 {
		current |= 02000
	}
	if info.Mode()&fs.ModeSticky != 0 {
		current |= 01000
	}
	updated, err := applySymbolicMode(current, mode.String(), info.IsDir())
	if err != nil {
		return err
	}
	fileMode := fs.FileMode(updated & 0777)
	if updated&04000 != 0 {
		fileMode |= fs.ModeSetuid
	}
	if updated&02000 != 0 {
		fileMode |= fs.ModeSetgid
	}
	if updated&01000 != 0 {
		fileMode |= fs.ModeSticky
	}
	return os.Chmod(path, fileMode)
}

func applySymbolicMode(current uint32, mode string, isDir bool) (uint32, error) {
	if mode == "" {
		return 0, errors.New("invalid symbolic mode")
	}

	classBits := [3]uint32{0700, 0070, 0007}
	for _, clause := range strings.Split(mode, ",") {
		if clause == "" {
			return 0, errors.New("invalid symbolic mode")
		}
		index := 0
		who := [3]bool{}
		for index < len(clause) {
			switch clause[index] {
			case 'u':
				who[0] = true
			case 'g':
				who[1] = true
			case 'o':
				who[2] = true
			case 'a':
				who = [3]bool{true, true, true}
			default:
				goto whoDone
			}
			index++
		}
	whoDone:
		if !who[0] && !who[1] && !who[2] {
			who = [3]bool{true, true, true}
		}
		if index >= len(clause) || (clause[index] != '+' && clause[index] != '-' && clause[index] != '=') {
			return 0, errors.New("invalid symbolic mode")
		}
		operator := clause[index]
		index++

		var selected, requested uint32
		for class, selectedForClass := range who {
			if selectedForClass {
				selected |= classBits[class]
				switch class {
				case 0:
					selected |= 04000
				case 1:
					selected |= 02000
				case 2:
					selected |= 01000
				}
			}
		}

		for ; index < len(clause); index++ {
			switch clause[index] {
			case 'r', 'w', 'x':
				for class, selectedForClass := range who {
					if selectedForClass {
						bit := uint32(1)
						if clause[index] == 'r' {
							bit = 4
						} else if clause[index] == 'w' {
							bit = 2
						}
						requested |= bit << uint(6-class*3)
					}
				}
			case 'X':
				if isDir || current&0111 != 0 {
					for class, selectedForClass := range who {
						if selectedForClass {
							requested |= 1 << uint(6-class*3)
						}
					}
				}
			case 'u', 'g', 'o':
				source := 0
				if clause[index] == 'g' {
					source = 1
				} else if clause[index] == 'o' {
					source = 2
				}
				for destination, selectedForClass := range who {
					if selectedForClass {
						bits := current & classBits[source]
						if source > destination {
							bits <<= uint((source - destination) * 3)
						} else {
							bits >>= uint((destination - source) * 3)
						}
						requested |= bits & classBits[destination]
					}
				}
			case 's':
				if who[0] {
					requested |= 04000
				}
				if who[1] {
					requested |= 02000
				}
			case 't':
				if who[2] {
					requested |= 01000
				}
			default:
				return 0, errors.New("invalid symbolic mode")
			}
		}

		switch operator {
		case '+':
			current |= requested
		case '-':
			current &^= requested
		case '=':
			current = current&^selected | requested
		}
	}
	return current & 07777, nil
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
