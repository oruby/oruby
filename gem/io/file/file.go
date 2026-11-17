package file

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/oruby/oruby"
)

type typeError struct{ msg string }

func (e *typeError) Error() string { return e.msg }

type argumentError struct{ msg string }

func (e *argumentError) Error() string { return e.msg }

type compatibilityError struct{ msg string }

func (e *compatibilityError) Error() string { return e.msg }

func init() {
	oruby.Gem("io/file", func(mrb *oruby.MrbState) interface{} {
		mrb.Require("io")
		cIO := initIOMethods(mrb)

		fileClass := mrb.DefineClass("File", cIO)
		fileClass.AttachType((*os.File)(nil))

		consts := initFileConsts(mrb, fileClass)
		fileClass.Include(consts)
		initFileStat(mrb, fileClass)
		initFileTest(mrb)
		initTmpFile(mrb, fileClass)
		initDir(mrb)

		mrb.DefineClassMethod(fileClass, "absolute_path", fileAbsolutePath, mrb.ArgsArg(1, 1))
		mrb.DefineClassMethod(fileClass, "absolute_path?", fileIsAbsolutePath, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "atime", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "basename", fileBasename, mrb.ArgsArg(1, 1))
		proxyClassMethodToStat(fileClass, "birthtime", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "blockdev?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "chardev?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "chmod", fileChmod, mrb.ArgsReq(1)|mrb.ArgsRest())
		mrb.DefineClassMethod(fileClass, "chown", fileChown, mrb.ArgsReq(2)|mrb.ArgsRest())
		proxyClassMethodToStat(fileClass, "ctime", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "delete", fileUnlink, mrb.ArgsAny())
		proxyClassMethodToStat(fileClass, "directory?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "dirname", fileDirname, mrb.ArgsArg(1, 1))
		proxyClassMethodToStat(fileClass, "empty?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "executable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "executable_real?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "exist?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "expand_path", fileExpandPath, mrb.ArgsArg(1, 1))
		mrb.DefineClassMethod(fileClass, "extname", fileExtname, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "file?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "match?", fileMatch, mrb.ArgsArg(2, 1))
		mrb.DefineClassMethod(fileClass, "fnmatch", fileFnMatch, mrb.ArgsArg(2, 1))
		mrb.DefineClassMethod(fileClass, "fnmatch?", fileFnMatch, mrb.ArgsArg(2, 1))
		mrb.DefineClassMethod(fileClass, "ftype", fileFtype, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "grpowned?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "identical?", fileIdentical, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "join", fileJoin, mrb.ArgsAny())
		mrb.DefineClassMethod(fileClass, "lchown", fileLchown, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "link", fileLink, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "lstat", fileLStat, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "lchmod", fileLchmod, mrb.ArgsReq(1)|mrb.ArgsRest())
		//lutime
		mrb.DefineClassMethod(fileClass, "mkfifo", fileMkfifio, mrb.ArgsArg(1, 1))
		proxyClassMethodToStat(fileClass, "mtime", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "owned?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "path", filePath, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "pipe?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "readable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "readable_real?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "readlink", fileReadlink, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "realdirpath", fileRealdirpath, mrb.ArgsArg(1, 1))
		mrb.DefineClassMethod(fileClass, "realpath", fileRealpath, mrb.ArgsArg(1, 1))
		mrb.DefineClassMethod(fileClass, "rename", fileRename, mrb.ArgsReq(2))
		proxyClassMethodToStat(fileClass, "setgid?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "setuid?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "size", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "size?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "socket?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "split", fileSplit, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "stat", fileStat, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "sticky?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "symlink", fileSymlink, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "symlink?", fileIsSymlink, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "truncate", fileTruncate, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "umask", fileUmask, mrb.ArgsOpt(1))
		mrb.DefineClassMethod(fileClass, "unlink", fileUnlink, mrb.ArgsAny())
		mrb.DefineClassMethod(fileClass, "utime", fileUtime, mrb.ArgsReq(2)|mrb.ArgsRest())
		mrb.DefineClassMethod(fileClass, "lutime", fileLutime, mrb.ArgsReq(2)|mrb.ArgsRest())
		proxyClassMethodToStat(fileClass, "world_readable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "world_writable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "writable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "writable_real?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "zero?", mrb.ArgsReq(1))

		mrb.Eval(`
class File
  def self.open(*args, **kw, &block)
    f = new(*args, **kw)
    if block
      begin
        yield f
      ensure
        begin
          f.close
        rescue IOError => e
          raise unless e.message.include?("closed stream")
        end
      end
    else
      f
    end
  end
end

module Kernel
  def open(*args, **kw, &block)
    File.open(*args, **kw, &block)
  end
  module_function :open
end
`)
		mrb.DefineMethod(fileClass, "initialize", fileInit, mrb.ArgsReq(1)|mrb.ArgsOpt(2)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())

		proxyMethodToStat(fileClass, "atime", mrb.ArgsNone())
		mrb.DefineMethod(fileClass, "chmod", fileFChmod, mrb.ArgsReq(1))
		mrb.DefineMethod(fileClass, "chown", fileFChown, mrb.ArgsReq(2))
		proxyMethodToStat(fileClass, "birthtime", mrb.ArgsNone())
		proxyMethodToStat(fileClass, "ctime", mrb.ArgsNone())
		mrb.DefineMethod(fileClass, "flock", fileFlock, mrb.ArgsReq(1))
		mrb.DefineMethod(fileClass, "lstat", fileFLStat, mrb.ArgsNone())
		proxyMethodToStat(fileClass, "mtime", mrb.ArgsNone())
		proxyMethodToStat(fileClass, "size", mrb.ArgsNone())
		mrb.DefineMethod(fileClass, "to_path", fileToPath, mrb.ArgsNone())
		mrb.AliasMethod(fileClass, mrb.Intern("path"), mrb.Intern("to_path"))
		mrb.DefineMethod(fileClass, "truncate", fileFTruncate, mrb.ArgsReq(1))
		return nil
	})
}

func proxyClassMethodToStat(fileClass oruby.RClass, name string, args oruby.MrbAspec) {
	fileClass.DefineClassMethod(name, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ai := mrb.GCArenaSave()
		defer mrb.GCArenaRestore(ai)

		rawArgs := mrb.GetArgs().Slice()
		if len(rawArgs) != 1 {
			return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(rawArgs))
		}

		stat, err := getStat(mrb, rawArgs[0])
		if err != nil {
			if _, ok := err.(*typeError); ok {
				return raisePathError(mrb, err)
			}
			if _, ok := err.(*compatibilityError); ok {
				return raisePathError(mrb, err)
			}
			if _, ok := err.(*argumentError); ok {
				return raisePathError(mrb, err)
			}
			if strings.HasSuffix(name, "?") {
				if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
					if name == "size?" || name == "world_readable?" || name == "world_writable?" {
						return oruby.Nil
					}
					return oruby.False
				}
			}
			return mrb.SysFail(err)
		}
		if name == "exist?" || name == "exists?" {
			return oruby.True
		}
		statClass := mrb.DataValue(stat)
		ret, err := mrb.FuncallWithBlock(statClass, mrb.GetMID())
		if err != nil {
			return mrb.RaiseError(err)
		}

		return ret
	}, args)
}

func proxyMethodToStat(fileClass oruby.RClass, name string, args oruby.MrbAspec) {
	fileClass.DefineMethod(name, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ai := mrb.GCArenaSave()
		defer mrb.GCArenaRestore(ai)

		rawArgs := mrb.GetArgs().Slice()
		if len(rawArgs) != 0 {
			return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 0)", len(rawArgs))
		}

		if res, err := mrb.Funcall(self, mrb.Intern("closed?")); err == nil && res.Bool() {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		data := mrb.Data(self)
		if data == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}

		f, ok := data.(*os.File)
		if !ok || f == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "uninitialized stream")
		}

		stat, err := f.Stat()
		if err != nil {
			if strings.HasSuffix(name, "?") {
				if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
					if name == "size?" || name == "world_readable?" || name == "world_writable?" {
						return oruby.Nil
					}
					return oruby.False
				}
			}
			return mrb.SysFail(err)
		}
		if name == "exist?" || name == "exists?" {
			return oruby.True
		}

		ret, err := mrb.FuncallWithBlock(mrb.DataValue(stat), mrb.GetMID())
		if err != nil {
			return mrb.RaiseError(err)
		}

		return ret
	}, args)
}

func fileUnlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := mrb.GetArgs().Slice()
	for _, item := range names {
		p, err := pathString(mrb, item)
		if err != nil {
			return raisePathError(mrb, err)
		}
		if err := os.Remove(p); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(names))
}

func fileRename(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2)", len(args))
	}
	from, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	to, err := pathString(mrb, args[1])
	if err != nil {
		return raisePathError(mrb, err)
	}
	if err := os.Rename(from, to); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileSymlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2)", len(args))
	}
	target, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	link, err := pathString(mrb, args[1])
	if err != nil {
		return raisePathError(mrb, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileLink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2)", len(args))
	}
	target, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	link, err := pathString(mrb, args[1])
	if err != nil {
		return raisePathError(mrb, err)
	}
	if err := os.Link(target, link); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func parseMode(mrb *oruby.MrbState, arg oruby.Value) (int, error) {
	if mrb.ClassOf(arg).Name() == "Bignum" {
		return 0, oruby.ERangeError("bignum too big to convert into 'int'")
	}
	if arg.IsInt() {
		return arg.Int(), nil
	}
	if mrb.RespondTo(arg, mrb.Intern("to_int")) {
		v, err := mrb.Funcall(arg, mrb.Intern("to_int"))
		if err != nil {
			return 0, err
		}
		if mrb.ClassOf(v).Name() == "Bignum" {
			return 0, oruby.ERangeError("bignum too big to convert into 'int'")
		}
		if v.IsInt() {
			return v.Int(), nil
		}
	}
	return 0, oruby.ETypeError("no implicit conversion of %s into Integer", mrb.ClassOf(arg).Name())
}

func parseID(mrb *oruby.MrbState, arg oruby.Value) (int, error) {
	if arg.IsNil() {
		return -1, nil
	}
	return parseMode(mrb, arg)
}

func fileChmod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 1 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1+)")
	}
	modeInt, err := parseMode(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}
	mode := os.FileMode(modeInt)
	for _, pathArg := range args[1:] {
		p, err := pathString(mrb, pathArg)
		if err != nil {
			return raisePathError(mrb, err)
		}
		if err := os.Chmod(p, mode); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(args) - 1)
}

func fileChown(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2+)", len(args))
	}
	uid, err := parseID(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}
	gid, err := parseID(mrb, args[1])
	if err != nil {
		return mrb.RaiseError(err)
	}

	for _, pathArg := range args[2:] {
		p, err := pathString(mrb, pathArg)
		if err != nil {
			return raisePathError(mrb, err)
		}
		if err := os.Chown(p, uid, gid); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(args) - 2)
}

func fileLchown(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2+)", len(args))
	}
	uid, err := parseID(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}
	gid, err := parseID(mrb, args[1])
	if err != nil {
		return mrb.RaiseError(err)
	}

	for _, pathArg := range args[2:] {
		p, err := pathString(mrb, pathArg)
		if err != nil {
			return raisePathError(mrb, err)
		}
		if err := os.Lchown(p, uid, gid); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(args) - 2)
}

func fileReadlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	ret, err := os.Readlink(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	val := mrb.StrNew(ret)
	return val // in the file system encoding, as MRI returns it
}

func fileIsSymlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	stat, err := os.Lstat(name)
	if err != nil {
		return oruby.False
	}
	return oruby.Bool(stat.Mode()&os.ModeSymlink != 0)
}

func isFileSep(c byte) bool {
	if runtime.GOOS == "windows" {
		return c == '/' || c == '\\'
	}
	return c == '/'
}

func joinTwoPaths(left, right string) string {
	lEnd := len(left)
	for lEnd > 0 && isFileSep(left[lEnd-1]) {
		lEnd--
	}
	sepLeft := left[lEnd:]
	leftStripped := left[:lEnd]

	rStart := 0
	for rStart < len(right) && isFileSep(right[rStart]) {
		rStart++
	}
	sepRight := right[:rStart]
	rightStripped := right[rStart:]

	var sep string
	if len(sepRight) > 0 {
		sep = sepRight
	} else if len(sepLeft) > 0 {
		sep = sepLeft
	} else {
		sep = "/"
	}

	return leftStripped + sep + rightStripped
}

func pathString(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	if mrb.NilP(v) {
		return "", &typeError{msg: "no implicit conversion of nil into String"}
	}
	var s string
	var orig oruby.Value = v
	if oruby.MrbStringP(v) {
		s = v.String()
	} else if mrb.RespondTo(v, mrb.Intern("to_path")) {
		ret, err := mrb.Funcall(v, mrb.Intern("to_path"))
		if err != nil {
			return "", err
		}
		if !oruby.MrbStringP(ret) {
			return "", &typeError{msg: "no implicit conversion into String"}
		}
		orig = ret
		s = ret.String()
	} else if mrb.RespondTo(v, mrb.Intern("to_str")) {
		ret, err := mrb.Funcall(v, mrb.Intern("to_str"))
		if err != nil {
			return "", err
		}
		if !oruby.MrbStringP(ret) {
			return "", &typeError{msg: "no implicit conversion into String"}
		}
		orig = ret
		s = ret.String()
	} else {
		return "", &typeError{msg: fmt.Sprintf("no implicit conversion of %s into String", mrb.ClassOf(v).Name())}
	}

	symEncoding := mrb.Intern("encoding")
	if mrb.RespondTo(orig, symEncoding) {
		if enc, err := mrb.Funcall(orig, symEncoding); err == nil && !mrb.NilP(enc) {
			symAsciiCompat := mrb.Intern("ascii_compatible?")
			if mrb.RespondTo(enc, symAsciiCompat) {
				if compat, err := mrb.Funcall(enc, symAsciiCompat); err == nil && !compat.Bool() {
					return "", &compatibilityError{msg: "ASCII incompatible encoding"}
				}
			}
		}
	}
	if strings.Contains(s, "\x00") {
		return "", &argumentError{msg: "string contains null byte"}
	}
	if oruby.MrbStringP(orig) {
		s = mrb.FSPath(orig)
	}

	return s, nil
}

func preserveEncoding(mrb *oruby.MrbState, src oruby.Value, dst oruby.MrbValue) oruby.MrbValue {
	symEncoding := mrb.Intern("encoding")
	symForceEnc := mrb.Intern("force_encoding")
	if oruby.MrbStringP(src) && mrb.RespondTo(src, symEncoding) {
		if enc, err := mrb.Funcall(src, symEncoding); err == nil && !mrb.NilP(enc) {
			// on macOS a non-UTF-8 path was converted to UTF-8 (pathString);
			// the result goes back to the path's encoding, as MRI returns it
			if runtime.GOOS == "darwin" && !isASCII(src.String()) {
				if name, err := mrb.Funcall(enc, mrb.Intern("name")); err == nil && name.String() != "UTF-8" && name.String() != "ASCII-8BIT" {
					mrb.Funcall(dst, symForceEnc, mrb.StrNew("UTF-8"))
					if back, err := mrb.Funcall(dst, mrb.Intern("encode"), enc); err == nil && oruby.MrbStringP(back) {
						mrb.Funcall(dst, mrb.Intern("replace"), back) // in place: some callers keep dst
						mrb.Funcall(dst, symForceEnc, enc)
						return dst
					}
					mrb.ExcClear()
				}
			}
			mrb.Funcall(dst, symForceEnc, enc)
		}
	}
	return dst
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func flattenFileJoin(mrb *oruby.MrbState, args []oruby.Value, seen map[int]bool) ([]string, error) {
	var elements []string

	for _, arg := range args {
		if mrb.ArrayP(arg) {
			id := mrb.ObjID(arg)
			if seen[id] {
				return nil, &argumentError{msg: "recursive array"}
			}
			seen[id] = true
			l := oruby.RArrayLen(arg)
			if l == 0 {
				delete(seen, id)
				elements = append(elements, "")
				continue
			}
			sub := make([]oruby.Value, l)
			for i := 0; i < l; i++ {
				sub[i] = mrb.AryRef(arg, i)
			}
			res, err := flattenFileJoin(mrb, sub, seen)
			delete(seen, id)
			if err != nil {
				return nil, err
			}
			elements = append(elements, res...)
		} else {
			s, err := pathString(mrb, arg)
			if err != nil {
				return nil, err
			}
			if strings.Contains(s, "\x00") {
				return nil, &argumentError{msg: "string contains null byte"}
			}
			elements = append(elements, s)
		}
	}
	return elements, nil
}

func fileJoin(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	seen := make(map[int]bool)
	elements, err := flattenFileJoin(mrb, args, seen)
	if err != nil {
		if _, ok := err.(*typeError); ok {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		if _, ok := err.(*argumentError); ok {
			return mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		return mrb.Raise(mrb.EStandardErrorClass(), err.Error())
	}
	if len(elements) == 0 {
		return mrb.StrNew("")
	}
	res := elements[0]
	for _, elem := range elements[1:] {
		res = joinTwoPaths(res, elem)
	}
	ret := mrb.StrNew(res)
	symEncoding := mrb.Intern("encoding")
	symForceEnc := mrb.Intern("force_encoding")
	for _, arg := range args {
		if oruby.MrbStringP(arg) && mrb.RespondTo(arg, symEncoding) {
			if enc, err := mrb.Funcall(arg, symEncoding); err == nil && !mrb.NilP(enc) {
				mrb.Funcall(ret, symForceEnc, enc)
				break
			}
		}
	}
	return ret
}

func fileExpandPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 1 || len(args) > 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1..2)", len(args))
	}

	if modEnc := mrb.EncodingClass(); !modEnc.IsNil() {
		if mrb.RespondTo(modEnc, mrb.Intern("default_external")) {
			if defExt, err := mrb.Funcall(modEnc, mrb.Intern("default_external")); err == nil && !mrb.NilP(defExt) {
				if compat, err := mrb.Funcall(defExt, mrb.Intern("ascii_compatible?")); err == nil && !compat.Bool() {
					compatClass := mrb.ClassGetUnder(modEnc, "CompatibilityError")
					return mrb.Raise(compatClass, "ASCII incompatible encoding: "+mrb.String(defExt))
				}
			}
		}
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	var dir string
	if len(args) == 2 && !args[1].IsNil() {
		dir, err = pathString(mrb, args[1])
		if err != nil {
			return raisePathError(mrb, err)
		}
	}

	leadingSlashes := 0
	for leadingSlashes < len(name) && name[leadingSlashes] == '/' {
		leadingSlashes++
	}

	expandTilde := func(p string) (string, error) {
		if !strings.HasPrefix(p, "~") {
			return p, nil
		}
		var home string
		envHome, hasHome := os.LookupEnv("HOME")
		if hasHome {
			if envHome == "" {
				return "", &argumentError{msg: "HOME is empty"}
			}
			if !filepath.IsAbs(envHome) {
				return "", &argumentError{msg: "non-absolute home"}
			}
			home = envHome
		} else {
			u, err := user.Current()
			if err != nil || u == nil || u.HomeDir == "" {
				return "", &argumentError{msg: "user home not found"}
			}
			home = u.HomeDir
		}
		if p == "~" {
			return home, nil
		}
		if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
			return filepath.Join(home, p[2:]), nil
		}
		slashIdx := strings.IndexAny(p, "/\\")
		var targetUser string
		var rest string
		if slashIdx == -1 {
			targetUser = p[1:]
		} else {
			targetUser = p[1:slashIdx]
			rest = p[slashIdx+1:]
		}
		u, err := user.Lookup(targetUser)
		if err != nil || u == nil {
			return "", &argumentError{msg: fmt.Sprintf("user %s doesn't exist", targetUser)}
		}
		if rest == "" {
			return u.HomeDir, nil
		}
		return filepath.Join(u.HomeDir, rest), nil
	}

	var pth string
	if strings.HasPrefix(name, "~") {
		exp, err := expandTilde(name)
		if err != nil {
			return raisePathError(mrb, err)
		}
		pth = exp
	} else {
		isAbs := false
		if runtime.GOOS == "windows" {
			isAbs = filepath.IsAbs(name)
		} else {
			isAbs = strings.HasPrefix(name, "/")
		}

		if isAbs {
			pth = name
		} else {
			baseDir := dir
			if baseDir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return mrb.SysFail(err)
				}
				baseDir = cwd
			} else {
				if strings.HasPrefix(baseDir, "~") {
					exp, err := expandTilde(baseDir)
					if err != nil {
						return raisePathError(mrb, err)
					}
					baseDir = exp
				}
				if !filepath.IsAbs(baseDir) {
					cwd, err := os.Getwd()
					if err != nil {
						return mrb.SysFail(err)
					}
					baseDir = filepath.Join(cwd, baseDir)
				}
			}
			pth = filepath.Join(baseDir, name)
		}
	}

	pth = filepath.Clean(pth)
	if leadingSlashes > 1 && strings.HasPrefix(pth, "/") {
		pth = strings.Repeat("/", leadingSlashes) + strings.TrimPrefix(pth, "/")
	}
	ret := mrb.StrNew(pth)
	return preserveEncoding(mrb, args[0], ret)
}

func rubyExtname(path string) string {
	lastSep := -1
	for i := len(path) - 1; i >= 0; i-- {
		if isFileSep(path[i]) {
			lastSep = i
			break
		}
	}
	base := path[lastSep+1:]
	if len(base) == 0 {
		return ""
	}

	allDots := true
	for i := 0; i < len(base); i++ {
		if base[i] != '.' {
			allDots = false
			break
		}
	}
	if allDots {
		return ""
	}

	lastDot := strings.LastIndexByte(base, '.')
	if lastDot <= 0 {
		return ""
	}
	return base[lastDot:]
}

func raisePathError(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if _, ok := err.(*compatibilityError); ok {
		compatClass := mrb.ClassGetUnder(mrb.EncodingClass(), "CompatibilityError")
		return mrb.Raise(compatClass, err.Error())
	}
	if _, ok := err.(*typeError); ok {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}
	if _, ok := err.(*argumentError); ok {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return mrb.SysFail(err)
}

func fileExtname(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	res := rubyExtname(name)
	ret := mrb.StrNew(res)
	return preserveEncoding(mrb, args[0], ret)
}

func filePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	ret := mrb.StrNew(name)
	return preserveEncoding(mrb, args[0], ret)
}

func getStat(mrb *oruby.MrbState, f oruby.Value) (os.FileInfo, error) {
	if mrb.RespondTo(f, mrb.Intern("to_io")) {
		if v, err := mrb.Funcall(f, mrb.Intern("to_io")); err == nil && !mrb.NilP(v) {
			f = v
		}
	}
	if f.IsData() {
		if s, ok := mrb.Data(f).(os.FileInfo); ok {
			return s, nil
		}
		if file, ok := mrb.Data(f).(*os.File); ok {
			return file.Stat()
		}
	}
	// as in MRI: an IO (to_io) or a path (to_path / String), never obj.stat
	s, err := pathString(mrb, f)
	if err != nil {
		return nil, err
	}
	return os.Stat(s)
}

func fileIdentical(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2)", len(args))
	}
	f1, f2 := args[0], args[1]

	stat1, err1 := getStat(mrb, f1)
	if err1 != nil {
		if _, ok := err1.(*typeError); ok {
			return raisePathError(mrb, err1)
		}
		if _, ok := err1.(*compatibilityError); ok {
			return raisePathError(mrb, err1)
		}
		if _, ok := err1.(*argumentError); ok {
			return raisePathError(mrb, err1)
		}
		return oruby.False
	}
	stat2, err2 := getStat(mrb, f2)
	if err2 != nil {
		if _, ok := err2.(*typeError); ok {
			return raisePathError(mrb, err2)
		}
		if _, ok := err2.(*compatibilityError); ok {
			return raisePathError(mrb, err2)
		}
		if _, ok := err2.(*argumentError); ok {
			return raisePathError(mrb, err2)
		}
		return oruby.False
	}

	return oruby.Bool(os.SameFile(stat1, stat2))
}

func singleDirname(path string) string {
	if len(path) == 0 {
		return "."
	}
	pEnd := len(path)
	for pEnd > 0 && isFileSep(path[pEnd-1]) {
		pEnd--
	}
	if pEnd == 0 {
		return "/"
	}

	stripped := path[:pEnd]
	lastSlash := -1
	for i := len(stripped) - 1; i >= 0; i-- {
		if isFileSep(stripped[i]) {
			lastSlash = i
			break
		}
	}
	if lastSlash < 0 {
		return "."
	}

	for lastSlash > 0 && isFileSep(stripped[lastSlash-1]) {
		lastSlash--
	}
	if lastSlash == 0 {
		return "/"
	}
	res := stripped[:lastSlash]
	if runtime.GOOS != "windows" {
		leadSlashes := 0
		for leadSlashes < len(res) && isFileSep(res[leadSlashes]) {
			leadSlashes++
		}
		if leadSlashes > 1 {
			res = "/" + res[leadSlashes:]
		}
	}
	return res
}

func fileDirname(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) == 0 || len(args) > 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1..2)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	level := 1
	if len(args) == 2 {
		lArg := args[1]
		if lArg.IsInt() {
			level = lArg.Int()
		} else if mrb.RespondTo(lArg, mrb.Intern("to_int")) {
			v, err := mrb.Funcall(lArg, mrb.Intern("to_int"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			level = v.Int()
		} else {
			return mrb.Raisef(mrb.ETypeError(), "no implicit conversion of %s into Integer", mrb.ClassOf(lArg).Name())
		}
		if level < 0 {
			return mrb.Raisef(mrb.EArgumentError(), "negative level: %d", level)
		}
	}
	res := name
	for l := 0; l < level; l++ {
		res = singleDirname(res)
	}
	ret := mrb.StrNew(res)
	return preserveEncoding(mrb, args[0], ret)
}

func fileSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	dir := singleDirname(name)
	base := rubyBasename(name, "")
	retDir := preserveEncoding(mrb, args[0], mrb.StrNew(dir))
	retBase := preserveEncoding(mrb, args[0], mrb.StrNew(base))
	return mrb.AryNewFromValues(retDir.(oruby.Value), retBase.(oruby.Value))
}

func parseTruncateSize(mrb *oruby.MrbState, arg oruby.Value) (int64, error) {
	if mrb.ClassOf(arg).Name() == "Bignum" {
		return 0, oruby.ERangeError("bignum too big to convert into 'int'")
	}
	if arg.IsInt() {
		return int64(arg.Int()), nil
	}
	if mrb.RespondTo(arg, mrb.Intern("to_int")) {
		v, err := mrb.Funcall(arg, mrb.Intern("to_int"))
		if err != nil {
			return 0, err
		}
		if mrb.ClassOf(v).Name() == "Bignum" {
			return 0, oruby.ERangeError("bignum too big to convert into 'int'")
		}
		if v.IsInt() {
			return int64(v.Int()), nil
		}
	}
	return 0, oruby.ETypeError("no implicit conversion of %s into Integer", mrb.ClassOf(arg).Name())
}

func fileTruncate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	size, err := parseTruncateSize(mrb, args[1])
	if err != nil {
		return mrb.RaiseError(err)
	}
	if size < 0 {
		return mrb.SysFail(syscall.EINVAL)
	}
	if err := os.Truncate(name, size); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	stat, err := getStat(mrb, arg)
	if err != nil {
		if _, ok := err.(*typeError); ok {
			return raisePathError(mrb, err)
		}
		if _, ok := err.(*compatibilityError); ok {
			return raisePathError(mrb, err)
		}
		if _, ok := err.(*argumentError); ok {
			return raisePathError(mrb, err)
		}
		return mrb.SysFail(err)
	}
	return mrb.DataValue(stat)
}

func fileLStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if arg.IsData() {
		if file, ok := mrb.Data(arg).(*os.File); ok {
			stat, err := file.Stat()
			if err != nil {
				return mrb.SysFail(err)
			}
			return mrb.DataValue(stat)
		}
	}
	name, err := pathString(mrb, arg)
	if err != nil {
		return raisePathError(mrb, err)
	}
	stat, err := os.Lstat(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	return mrb.DataValue(stat)
}

func fileFtype(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(rawArgs))
	}
	arg := rawArgs[0]
	var stat os.FileInfo
	var err error
	if arg.IsData() {
		if file, ok := mrb.Data(arg).(*os.File); ok {
			stat, err = file.Stat()
		}
	}
	if stat == nil {
		name, pErr := pathString(mrb, arg)
		if pErr != nil {
			return raisePathError(mrb, pErr)
		}
		stat, err = os.Lstat(name)
	}
	if err != nil {
		return mrb.SysFail(err)
	}

	mode := stat.Mode()
	if mode&os.ModeSymlink != 0 {
		return mrb.StrNew("link")
	}
	if stat.IsDir() {
		return mrb.StrNew("directory")
	}
	if mode&os.ModeCharDevice != 0 {
		return mrb.StrNew("characterSpecial")
	}
	if mode&os.ModeSocket != 0 {
		return mrb.StrNew("socket")
	}
	if mode&os.ModeDevice != 0 {
		return mrb.StrNew("blockSpecial")
	}
	if mode&os.ModeNamedPipe != 0 {
		return mrb.StrNew("fifo")
	}
	if stat.Mode().IsRegular() {
		return mrb.StrNew("file")
	}
	return mrb.StrNew("unknown")
}

func fileFLStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if file, ok := mrb.Data(self).(*os.File); ok {
		stat, err := file.Stat()
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.DataValue(stat)
	}
	return mrb.Raise(mrb.ClassGet("IOError"), "uninitialized stream")
}

func fileMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	p, n, f := mrb.GetArgs3("", "", 0)
	name := n.String()
	pattern := p.String()
	flags := f.Int()

	if flags&fnmCasefold != 0 {
		name = strings.ToLower(name)
		pattern = strings.ToLower(pattern)
	}

	if (flags&fnmNoescape != 0) && (runtime.GOOS != "windows") {
		pattern = strings.ReplaceAll(pattern, "\\", "\\\\")
	}

	ok, err := filepath.Match(pattern, name)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Bool(ok)
}

func rubyBasename(path, suffix string) string {
	if len(path) == 0 {
		return ""
	}
	pEnd := len(path)
	for pEnd > 0 && isFileSep(path[pEnd-1]) {
		pEnd--
	}
	if pEnd == 0 {
		return "/"
	}

	stripped := path[:pEnd]
	lastSlash := -1
	for i := len(stripped) - 1; i >= 0; i-- {
		if isFileSep(stripped[i]) {
			lastSlash = i
			break
		}
	}
	base := stripped
	if lastSlash >= 0 {
		base = stripped[lastSlash+1:]
	}

	if suffix == ".*" {
		dot := strings.LastIndexByte(base, '.')
		if dot > 0 {
			allDots := true
			for i := 0; i < len(base); i++ {
				if base[i] != '.' {
					allDots = false
					break
				}
			}
			if !allDots {
				base = base[:dot]
			}
		}
	} else if len(suffix) > 0 && strings.HasSuffix(base, suffix) {
		base = base[:len(base)-len(suffix)]
	}

	return base
}

func fileBasename(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) == 0 || len(args) > 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1..2)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	suffix := ""
	if len(args) == 2 {
		s, err := pathString(mrb, args[1])
		if err != nil {
			return raisePathError(mrb, err)
		}
		suffix = s
	}
	res := rubyBasename(name, suffix)
	ret := mrb.StrNew(res)
	return preserveEncoding(mrb, args[0], ret)
}

func fileRealpath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) < 1 || len(rawArgs) > 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1..2)", len(rawArgs))
	}
	pth, err := pathString(mrb, rawArgs[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	if pth == "/" {
		ret := mrb.StrNew("/")
		return preserveEncoding(mrb, rawArgs[0], ret)
	}
	if !filepath.IsAbs(pth) {
		var dir string
		if len(rawArgs) == 2 && !rawArgs[1].IsNil() {
			dir, err = pathString(mrb, rawArgs[1])
			if err != nil {
				return raisePathError(mrb, err)
			}
		}
		if dir == "" {
			dir, _ = os.Getwd()
		} else if !filepath.IsAbs(dir) {
			pwd, _ := os.Getwd()
			dir = filepath.Join(pwd, dir)
		}
		pth = filepath.Join(dir, pth)
	}
	realPth, err := filepath.EvalSymlinks(pth)
	if err != nil {
		return mrb.SysFail(err)
	}
	absPth, err := filepath.Abs(realPth)
	if err != nil {
		return mrb.SysFail(err)
	}
	ret := mrb.StrNew(absPth)
	return preserveEncoding(mrb, rawArgs[0], ret)
}

func resolveRealdirpath(pth string) (string, error) {
	curr := pth
	visited := make(map[string]bool)
	for i := 0; i < 64; i++ {
		fi, err := os.Lstat(curr)
		if err != nil {
			// curr does not exist
			break
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			// Exists and is not a symlink!
			realPth, err := filepath.EvalSymlinks(curr)
			if err != nil {
				return "", err
			}
			return filepath.Abs(realPth)
		}
		// It is a symlink
		if visited[curr] {
			return "", syscall.ELOOP
		}
		visited[curr] = true
		linkTarget, err := os.Readlink(curr)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(linkTarget) {
			curr = filepath.Join(filepath.Dir(curr), linkTarget)
		} else {
			curr = linkTarget
		}
	}
	if visited[curr] {
		return "", syscall.ELOOP
	}
	// curr does not exist. Its directory must exist, and base is appended.
	dirPart := filepath.Dir(curr)
	basePart := filepath.Base(curr)
	realDir, err := filepath.EvalSymlinks(dirPart)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(realDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(absDir, basePart), nil
}

func fileRealdirpath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) < 1 || len(rawArgs) > 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1..2)", len(rawArgs))
	}
	pth, err := pathString(mrb, rawArgs[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	if pth == "/" {
		ret := mrb.StrNew("/")
		return preserveEncoding(mrb, rawArgs[0], ret)
	}
	var dir string
	if len(rawArgs) == 2 && !rawArgs[1].IsNil() {
		dir, err = pathString(mrb, rawArgs[1])
		if err != nil {
			return raisePathError(mrb, err)
		}
	}
	if !filepath.IsAbs(pth) {
		if dir == "" {
			dir, _ = os.Getwd()
		} else if !filepath.IsAbs(dir) {
			pwd, _ := os.Getwd()
			dir = filepath.Join(pwd, dir)
		}
		pth = filepath.Join(dir, pth)
	}
	realPth, err := resolveRealdirpath(pth)
	if err != nil {
		return mrb.SysFail(err)
	}
	ret := mrb.StrNew(realPth)

	// Check encoding
	if oruby.MrbStringP(rawArgs[0]) {
		src := rawArgs[0]
		encVal, _ := mrb.Funcall(src, mrb.Intern("encoding"))
		encName := ""
		if !mrb.NilP(encVal) {
			if s, err := mrb.Funcall(encVal, mrb.Intern("to_s")); err == nil && oruby.MrbStringP(s) {
				encName = s.String()
			}
		}
		hasNonAscii := false
		for i := 0; i < len(realPth); i++ {
			if realPth[i] >= 0x80 {
				hasNonAscii = true
				break
			}
		}
		if hasNonAscii && (strings.Contains(encName, "ISO") || strings.Contains(encName, "ASCII")) {
			if len(rawArgs) == 2 && !rawArgs[1].IsNil() {
				preserveEncoding(mrb, rawArgs[1], ret)
			}
		} else {
			preserveEncoding(mrb, src, ret)
		}
	}
	return ret
}

func fileExist(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(rawArgs))
	}
	_, err := getStat(mrb, rawArgs[0])
	if err != nil {
		if _, ok := err.(*typeError); ok {
			return raisePathError(mrb, err)
		}
		if _, ok := err.(*compatibilityError); ok {
			return raisePathError(mrb, err)
		}
		if _, ok := err.(*argumentError); ok {
			return raisePathError(mrb, err)
		}
		return oruby.False
	}
	return oruby.True
}

func fileAbsolutePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 1 || len(args) > 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1..2)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}

	var dir string
	if len(args) == 2 && !args[1].IsNil() {
		dir, err = pathString(mrb, args[1])
		if err != nil {
			return raisePathError(mrb, err)
		}
	}

	var isAbs bool
	if runtime.GOOS == "windows" {
		isAbs = filepath.IsAbs(name)
	} else {
		isAbs = strings.HasPrefix(name, "/")
	}

	var pth string
	if isAbs {
		pth = filepath.Clean(name)
	} else {
		baseDir := dir
		if baseDir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return mrb.SysFail(err)
			}
			baseDir = cwd
		} else if !filepath.IsAbs(baseDir) {
			cwd, err := os.Getwd()
			if err != nil {
				return mrb.SysFail(err)
			}
			baseDir = filepath.Join(cwd, baseDir)
		}
		pth = filepath.Clean(filepath.Join(baseDir, name))
	}

	ret := mrb.StrNew(pth)
	return preserveEncoding(mrb, args[0], ret)
}

func fileIsAbsolutePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	name, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	if runtime.GOOS == "windows" {
		return oruby.Bool(filepath.IsAbs(name))
	}
	return oruby.Bool(strings.HasPrefix(name, "/"))
}

func fileFTruncate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if res, err := mrb.Funcall(self, mrb.Intern("closed?")); err == nil && res.Bool() {
		return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
	}
	data := mrb.Data(self)
	if data == nil {
		return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
	}
	f, ok := data.(*os.File)
	if !ok || f == nil {
		return mrb.Raise(mrb.ClassGet("IOError"), "uninitialized stream")
	}
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	size, err := parseTruncateSize(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}
	if size < 0 {
		return mrb.SysFail(syscall.EINVAL)
	}

	if err := f.Truncate(size); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EBADF) {
			return mrb.Raise(mrb.ClassGet("IOError"), "not opened for writing")
		}
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileFChmod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	args := mrb.GetArgs().Slice()
	if len(args) != 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 1)", len(args))
	}
	modeInt, err := parseMode(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err := f.Chmod(os.FileMode(modeInt)); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileFChown(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	args := mrb.GetArgs().Slice()
	if len(args) < 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2)", len(args))
	}
	uid, err := parseID(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}
	gid, err := parseID(mrb, args[1])
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err := f.Chown(uid, gid); err != nil {
		return mrb.SysFail(err)
	}

	return oruby.Integer(0)
}

func fileToPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.Data(self)
	if data == nil {
		return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
	}
	f, ok := data.(*os.File)
	if !ok || f == nil {
		return mrb.Raise(mrb.ClassGet("IOError"), "uninitialized stream")
	}
	ret := mrb.StrNew(f.Name())
	ivPath := mrb.GetIV(self, "@path")
	if !mrb.NilP(ivPath) {
		return preserveEncoding(mrb, ivPath, ret)
	}
	return ret
}

func openFile(mrb *oruby.MrbState, args oruby.RArgs) (*os.File, error) {
	if args.Len() == 0 {
		return nil, &argumentError{msg: "wrong number of arguments (given 0, expected 1..3)"}
	}
	if args.Len() > 3 {
		return nil, &argumentError{msg: fmt.Sprintf("wrong number of arguments (given %d, expected 1..3)", args.Len())}
	}
	first := args.Item(0)
	if first.IsInt() || (!first.IsNil() && mrb.RespondTo(first, mrb.Intern("to_int"))) {
		fd := first.Int()
		if !first.IsInt() {
			v, err := mrb.Funcall(first, mrb.Intern("to_int"))
			if err != nil {
				return nil, err
			}
			fd = v.Int()
		}
		if fd < 0 {
			return nil, syscall.EBADF
		}
		var stat syscall.Stat_t
		if err := syscall.Fstat(fd, &stat); err != nil {
			return nil, err
		}
		if args.Len() > 1 && (args.Item(1).IsString() || args.Item(1).IsInteger()) {
			flags, err := modeToFlags(mrb, args.Item(1))
			if err != nil {
				return nil, err
			}
			if err := checkFdMode(fd, flags); err != nil {
				return nil, err
			}
		}
		f := os.NewFile(uintptr(fd), fmt.Sprintf("fd:%d", fd))
		return f, nil
	}
	name, err := pathString(mrb, first)
	if err != nil {
		return nil, err
	}
	flags := 0
	optFlags := 0
	perm := 0666

	var optHash oruby.Value
	kw := mrb.KeywordArgs()
	if !kw.IsNil() && mrb.HashSize(kw) > 0 {
		optHash = kw.Value()
	} else if args.Len() > 0 {
		last := args.Item(-1)
		if last.IsHash() {
			optHash = last
		} else if mrb.RespondTo(last, mrb.Intern("to_hash")) {
			h, err := mrb.Funcall(last, mrb.Intern("to_hash"))
			if err == nil && h.IsHash() {
				optHash = h
			}
		}
	}

	if !optHash.IsNil() {
		optFlags, err = modeToFlags(mrb, optHash)
		if err != nil {
			return nil, err
		}
	}

	switch args.Len() {
	case 2:
		if args.Item(1).IsString() || args.Item(1).IsInteger() {
			flags, err = modeToFlags(mrb, args.Item(1))
			if err != nil {
				return nil, err
			}
		}
	case 3:
		flags, err = modeToFlags(mrb, args.Item(1))
		if err != nil {
			return nil, err
		}
		if args.Item(2).IsInteger() {
			perm = args.Item(2).Int()
		}
	}

	flags = flags | optFlags
	if flags == 0 {
		flags = os.O_RDONLY
	}

	return os.OpenFile(name, flags, os.FileMode(perm))
}

// :mode Same as mode parameter
// :flags Specifies file open flags as integer. If mode parameter is given, this parameter will be bitwise-ORed.
// :external_encoding External encoding for the IO.
// :internal_encoding Internal encoding for the IO. “-” is a synonym for the default internal encoding.
//
//	If the value is nil no conversion occurs.
//
// :encoding Specifies external and internal encodings as “extern:intern”.
// :textmode If the value is truth value, same as “t” in argument mode.
// :binmode  If the value is truth value, same as “b” in argument mode.
// :autoclose If the value is false, the fd will be kept open after this IO instance gets finalized.
func fileOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	raw := args.Slice()
	newArgs := make([]interface{}, len(raw))
	for i, v := range raw {
		newArgs[i] = v
	}
	kw := mrb.KeywordArgs()
	if !kw.IsNil() && mrb.HashSize(kw) > 0 {
		if len(raw) == 0 || !raw[len(raw)-1].IsHash() {
			newArgs = append(newArgs, kw.Value())
		}
	}
	file, err := mrb.ClassPtr(self).New(newArgs...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if mrb.Exc() != nil {
		return mrb.Exc()
	}
	if block.IsNil() {
		return file
	}

	res := mrb.Yield(block, file)
	blockExc := mrb.Exc()

	ret, err := mrb.Funcall(file, mrb.Intern("close"))
	_ = ret
	if blockExc != nil {
		mrb.SetExc(blockExc)
		return blockExc
	}

	if err != nil {
		if strings.Contains(err.Error(), "closed stream") {
			mrb.ExcClear()
		} else {
			return mrb.RaiseError(err)
		}
	}

	if mrb.Exc() != nil {
		return mrb.Exc()
	}
	return res
}

func fileInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	if !block.IsNil() {
		_, _ = mrb.Funcall(mrb.KernelModule(), mrb.Intern("warn"), mrb.StrNew("warning: File::new() does not take block; use File::open() instead"))
	}
	f, err := openFile(mrb, args)
	if err != nil {
		return raisePathError(mrb, err)
	}

	first := args.Item(0)
	if first.IsInt() || (!first.IsNil() && mrb.RespondTo(first, mrb.Intern("to_int"))) {
		mrb.SetIV(self, "@path", mrb.NilValue())
	} else {
		mrb.SetIV(self, "@path", first)
	}

	var extEnc, intEnc oruby.Value
	modeStr := ""
	if args.Len() > 1 && args.Item(1).IsString() {
		modeStr = args.Item(1).String()
	}
	if strings.Contains(modeStr, ":") {
		parts := strings.Split(modeStr, ":")
		if len(parts) >= 2 {
			extEnc = findEncoding(mrb, parts[1])
		}
		if len(parts) >= 3 {
			intEnc = findEncoding(mrb, parts[2])
		}
	}
	opt := mrb.KeywordArgs()
	var optHash oruby.Value
	if !opt.IsNil() && mrb.HashSize(opt) > 0 {
		optHash = opt.Value()
	} else if args.Len() > 0 && args.Item(-1).IsHash() {
		optHash = args.Item(-1)
	}
	if !optHash.IsNil() {
		encVal := mrb.HashFetch(optHash, mrb.Intern("encoding"), mrb.NilValue())
		if encVal.IsString() {
			parts := strings.Split(encVal.String(), ":")
			extEnc = findEncoding(mrb, parts[0])
			if len(parts) >= 2 {
				intEnc = findEncoding(mrb, parts[1])
			}
		} else if !encVal.IsNil() {
			extEnc = encVal
		}
		extVal := mrb.HashFetch(optHash, mrb.Intern("external_encoding"), mrb.NilValue())
		if !extVal.IsNil() {
			extEnc = extVal
		}
		intVal := mrb.HashFetch(optHash, mrb.Intern("internal_encoding"), mrb.NilValue())
		if !intVal.IsNil() {
			intEnc = intVal
		}
	}

	isBinary := false
	if strings.Contains(modeStr, "b") {
		isBinary = true
	}
	if args.Len() > 1 && args.Item(1).IsInteger() {
		if args.Item(1).Int()&0x8000 != 0 { // File::BINARY
			isBinary = true
		}
	}
	if !optHash.IsNil() {
		bm := mrb.HashFetch(optHash, mrb.Intern("binmode"), mrb.NilValue())
		if bm.Type() == oruby.MrbTTTrue {
			isBinary = true
		}
		tm := mrb.HashFetch(optHash, mrb.Intern("textmode"), mrb.NilValue())
		if tm.Type() == oruby.MrbTTTrue {
			isBinary = false
		}
		flagsVal := mrb.HashFetch(optHash, mrb.Intern("flags"), mrb.NilValue())
		if flagsVal.IsInteger() && flagsVal.Int()&0x8000 != 0 {
			isBinary = true
		}
		nl := mrb.HashFetch(optHash, mrb.Intern("newline"), mrb.NilValue())
		if !nl.IsNil() && isBinary {
			return mrb.Raise(mrb.EArgumentError(), "newline decorator with binary mode")
		}
	}

	if isBinary {
		mrb.SetIV(self, "@binmode", oruby.True)
		if extEnc.IsNil() {
			encClass := mrb.EncodingClass()
			extEnc, _ = mrb.Funcall(encClass, mrb.Intern("find"), mrb.StrNew("ASCII-8BIT"))
		}
	}

	if !extEnc.IsNil() {
		mrb.SetIV(self, "@external_encoding", extEnc)
	}
	if !intEnc.IsNil() {
		mrb.SetIV(self, "@internal_encoding", intEnc)
	}

	mrb.DataSetInterface(self, f)
	return self
}

func fileFnMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 2 || len(args) > 3 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2..3)", len(args))
	}
	pattern, err := pathString(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	name, err := pathString(mrb, args[1])
	if err != nil {
		return raisePathError(mrb, err)
	}
	flags := 0
	if len(args) == 3 {
		fInt, err := parseMode(mrb, args[2])
		if err != nil {
			return mrb.RaiseError(err)
		}
		flags = int(fInt)
	}

	ret, err := fnmatch(name, pattern, flags)
	if err != nil {
		return oruby.False
	}

	return oruby.Bool(ret)
}

func parseTimeArg(mrb *oruby.MrbState, val oruby.Value) (time.Time, error) {
	if mrb.NilP(val) {
		return time.Now(), nil
	}
	if val.IsData() {
		if t, ok := mrb.Data(val).(time.Time); ok {
			return t, nil
		}
	}
	if val.IsInteger() {
		return time.Unix(val.Int64(), 0), nil
	}
	if val.IsFloat() {
		f := val.Float64()
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	}
	if mrb.RespondTo(val, mrb.Intern("to_f")) {
		if fv, err := mrb.Funcall(val, mrb.Intern("to_f")); err == nil {
			if fl, err := mrb.Float(fv); err == nil {
				f := fl.Float64()
				sec := int64(f)
				nsec := int64((f - float64(sec)) * 1e9)
				return time.Unix(sec, nsec), nil
			}
		}
	}
	if mrb.RespondTo(val, mrb.Intern("to_i")) {
		if iv, err := mrb.Funcall(val, mrb.Intern("to_i")); err == nil && iv.IsInteger() {
			return time.Unix(iv.Int64(), 0), nil
		}
	}
	return time.Time{}, &typeError{msg: fmt.Sprintf("can't convert %s into Time", mrb.ClassOf(val).Name())}
}

func fileUtime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2+)", len(args))
	}
	atime, err := parseTimeArg(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	mtime, err := parseTimeArg(mrb, args[1])
	if err != nil {
		return raisePathError(mrb, err)
	}
	paths := args[2:]
	for _, p := range paths {
		name, err := pathString(mrb, p)
		if err != nil {
			return raisePathError(mrb, err)
		}
		if err := os.Chtimes(name, atime, mtime); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(paths))
}

// findEncoding resolves an encoding named in an open mode or encoding:
// option: "BOM|" is dropped, "-" (no conversion) and an empty name give nil,
// and an unknown name is ignored, as MRI ignores it with a warning.
func findEncoding(mrb *oruby.MrbState, name string) oruby.Value {
	if len(name) >= 4 && strings.EqualFold(name[:4], "BOM|") {
		name = name[4:]
	}
	if name == "" || name == "-" {
		return mrb.NilValue()
	}
	v, err := mrb.Funcall(mrb.EncodingClass(), mrb.Intern("find"), mrb.StrNew(name))
	if err != nil {
		mrb.ExcClear()
		return mrb.NilValue()
	}
	return v
}
