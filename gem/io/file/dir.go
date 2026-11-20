package file

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"

	"github.com/oruby/oruby"
)

func initDir(mrb *oruby.MrbState) {
	dirClass := mrb.DefineClass("Dir", mrb.ObjectClass())
	dirClass.Include(mrb.ModuleGet("Enumerable"))
	oruby.MrbSetInstanceTT(dirClass, oruby.MrbTTCData)

	dirClass.DefineClassMethod("open", dirOpen, mrb.ArgsArg(1, 1)|mrb.ArgsKey(1, 0)|mrb.ArgsBlock())
	dirClass.DefineClassMethod("for_fd", dirForFd, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("fchdir", dirSFchdir, mrb.ArgsReq(1)|mrb.ArgsBlock())
	dirClass.DefineClassMethod("foreach", dirForeach, mrb.ArgsArg(1, 1)|mrb.ArgsKey(1, 0)|mrb.ArgsBlock())
	dirClass.DefineClassMethod("entries", dirEntries, mrb.ArgsArg(1, 1)|mrb.ArgsKey(1, 0))
	dirClass.DefineClassMethod("each_child", dirSEachChild, mrb.ArgsArg(1, 1)|mrb.ArgsKey(1, 0)|mrb.ArgsBlock())
	dirClass.DefineClassMethod("children", dirChildren, mrb.ArgsArg(1, 1)|mrb.ArgsKey(1, 0))

	dirClass.DefineMethod("initialize", dirInitialize, mrb.ArgsArg(1, 1)|mrb.ArgsKey(1, 0))
	dirClass.DefineMethod("chdir", dirChdirInst, mrb.ArgsNone()|mrb.ArgsBlock())
	dirClass.DefineMethod("fileno", dirFileno, mrb.ArgsNone())
	dirClass.DefineMethod("path", dirPath, mrb.ArgsNone())
	dirClass.DefineMethod("to_path", dirPath, mrb.ArgsNone())
	dirClass.DefineMethod("inspect", dirInspect, mrb.ArgsNone())
	dirClass.DefineMethod("read", dirRead, mrb.ArgsNone())
	dirClass.DefineMethod("each", dirEach, mrb.ArgsNone())
	dirClass.DefineMethod("each_child", dirEachChild, mrb.ArgsNone())
	dirClass.DefineMethod("children", dirCollectChildren, mrb.ArgsNone())
	dirClass.DefineMethod("rewind", dirRewind, mrb.ArgsNone())
	dirClass.DefineMethod("tell", dirTell, mrb.ArgsNone())
	dirClass.DefineMethod("seek", dirSeek, mrb.ArgsReq(1))
	dirClass.DefineMethod("pos", dirTell, mrb.ArgsNone())
	dirClass.DefineMethod("pos=", dirSetPos, mrb.ArgsReq(1))
	dirClass.DefineMethod("close", dirClose, mrb.ArgsNone())

	dirClass.DefineClassMethod("chdir", dirChdir, mrb.ArgsOpt(1))
	dirClass.DefineClassMethod("getwd", dirGetwd, 0)
	dirClass.DefineClassMethod("pwd", dirGetwd, 0)
	dirClass.DefineClassMethod("chroot", dirChroot, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("mkdir", dirMkdir, mrb.ArgsArg(1, 1))
	dirClass.DefineClassMethod("rmdir", dirRmdir, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("delete", dirRmdir, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("unlink", dirRmdir, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("home", dirHome, mrb.ArgsOpt(1))

	dirClass.DefineClassMethod("__oruby_glob", dirGlob, mrb.ArgsReq(4))
	dirClass.DefineClassMethod("scan", dirSScan, mrb.ArgsReq(1)|mrb.ArgsKey(1, 0)|mrb.ArgsBlock())
	dirClass.DefineMethod("scan", dirScan, mrb.ArgsNone()|mrb.ArgsBlock())
	dirClass.DefineClassMethod("exist?", dirExist, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("empty?", dirIsEmpty, mrb.ArgsReq(1))
	if _, err := mrb.Eval(dirGlobRb); err != nil {
		panic(err)
	}
}

func toInt(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	if v.IsInt() {
		return v.Int(), nil
	}
	if mrb.RespondTo(v, oruby.Sym_to_int) {
		ret, err := mrb.Funcall(v, oruby.Sym_to_int)
		if err != nil {
			return 0, err
		}
		if ret.IsInt() {
			return ret.Int(), nil
		}
		return 0, &typeError{msg: fmt.Sprintf("can't convert %s into Integer (%s#to_int gives %s)",
			mrb.ClassName(mrb.ClassOf(v)), mrb.ClassName(mrb.ClassOf(v)), mrb.ClassName(mrb.ClassOf(ret)))}
	}
	return 0, &typeError{msg: fmt.Sprintf("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ClassOf(v)))}
}

func pathValueAndString(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, string, error) {
	if mrb.NilP(v) {
		return mrb.NilValue(), "", &typeError{msg: "no implicit conversion of nil into String"}
	}
	orig := v
	if !oruby.MrbStringP(orig) {
		if mrb.RespondTo(orig, oruby.Sym_to_path) {
			ret, err := mrb.Funcall(orig, oruby.Sym_to_path)
			if err != nil {
				return mrb.NilValue(), "", err
			}
			orig = ret
		}
		if !oruby.MrbStringP(orig) && mrb.RespondTo(orig, oruby.Sym_to_str) {
			ret, err := mrb.Funcall(orig, oruby.Sym_to_str)
			if err != nil {
				return mrb.NilValue(), "", err
			}
			orig = ret
		}
		if !oruby.MrbStringP(orig) {
			return mrb.NilValue(), "", &typeError{msg: fmt.Sprintf("no implicit conversion of %s into String", mrb.ClassName(mrb.ClassOf(v)))}
		}
	}
	s := orig.String()
	if strings.ContainsRune(s, 0) {
		return mrb.NilValue(), "", &argumentError{msg: "path name contains null byte"}
	}
	return orig, mrb.FSPath(orig), nil
}

func dirEncodingOpt(mrb *oruby.MrbState) oruby.Value {
	if kw := mrb.KeywordArgs(); !kw.IsNil() {
		if v := mrb.HashFetch(kw.Value(), mrb.SymbolValue(oruby.Sym_encoding), mrb.NilValue()); !v.IsNil() {
			return v
		}
	}
	if h := mrb.GetArgs().Item(1); h.IsHash() {
		return mrb.HashFetch(h, mrb.SymbolValue(oruby.Sym_encoding), mrb.NilValue())
	}
	return mrb.NilValue()
}

func resolveDirEncoding(mrb *oruby.MrbState, enc oruby.Value) (oruby.Value, bool, error) {
	if !enc.IsNil() {
		if !enc.IsData() {
			e, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_find, enc)
			if err != nil {
				return mrb.NilValue(), false, err
			}
			return e.Value(), true, nil
		}
		return enc, true, nil
	}
	// External encoding defaults to filesystem/default_external
	if mrb.RespondTo(mrb.EncodingClass(), oruby.Sym_default_external) {
		if e, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_default_external); err == nil && !e.IsNil() {
			return e.Value(), false, nil
		}
	}
	return mrb.NilValue(), false, nil
}

func encodeDirEntry(mrb *oruby.MrbState, name string, extEnc oruby.Value, explicit bool) oruby.Value {
	s := mrb.StrNew(name).Value()
	if !extEnc.IsNil() {
		_, _ = mrb.Funcall(s, oruby.Sym_force_encoding, extEnc)
	}
	if !explicit && mrb.RespondTo(mrb.EncodingClass(), oruby.Sym_default_internal) {
		if di, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_default_internal); err == nil && !di.IsNil() {
			converted, err := mrb.Funcall(s, mrb.Intern("encode"), di.Value())
			if err == nil {
				return converted.Value()
			}
			mrb.SetExc(nil)
		}
	}
	return s
}

func dirFile(mrb *oruby.MrbState, self oruby.Value) (*os.File, error) {
	if mrb.GetIV(self, "@closed").Bool() {
		return nil, &ioClosedError{msg: "closed directory"}
	}
	f, _ := mrb.Data(self).(*os.File)
	if f == nil {
		return nil, &ioClosedError{msg: "closed directory"}
	}
	return f, nil
}

type ioClosedError struct{ msg string }

func (e *ioClosedError) Error() string { return e.msg }

func raiseDirError(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if ce, ok := err.(*ioClosedError); ok {
		return mrb.Raise(mrb.ClassGet("IOError"), ce.msg)
	}
	return raisePathError(mrb, err)
}

func dirOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	var callArgs []interface{}
	for i := 0; i < args.Len(); i++ {
		callArgs = append(callArgs, args.Item(i))
	}
	if kw := mrb.KeywordArgs(); !kw.IsNil() {
		callArgs = append(callArgs, kw.Value())
	}
	ret, err := mrb.ObjNew(mrb.ClassPtr(self), callArgs...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if block.IsNil() {
		return ret
	}
	result := mrb.Yield(block, ret)
	dirCloseRaw(mrb, ret.Value())
	return result
}

func dirInitialize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pathVal, dir, err := pathValueAndString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	f, err := os.Open(dir)
	if err != nil {
		return mrb.SysFail(err)
	}
	mrb.SetIV(self, "@path", pathVal)
	mrb.SetIV(self, "@pos", 0)
	mrb.SetIV(self, "@closed", false)
	if enc := dirEncodingOpt(mrb); !enc.IsNil() {
		mrb.SetIV(self, "@encoding", enc)
	}
	mrb.DataSetInterface(self, f)

	return self
}

func dirEntries(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	_, dir, err := pathValueAndString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	extEnc, explicit, err := resolveDirEncoding(mrb, dirEncodingOpt(mrb))
	if err != nil {
		return mrb.RaiseError(err)
	}
	f, err := os.Open(dir)
	if err != nil {
		return mrb.SysFail(err)
	}

	files, err := f.Readdirnames(-1)
	_ = f.Close()
	if err != nil {
		return mrb.SysFail(err)
	}
	ret := mrb.AryNewCapa(len(files) + 2)
	ret.Push(encodeDirEntry(mrb, ".", extEnc, explicit))
	ret.Push(encodeDirEntry(mrb, "..", extEnc, explicit))
	for _, name := range files {
		ret.Push(encodeDirEntry(mrb, name, extEnc, explicit))
	}
	return ret
}

func dirDoForeach(mrb *oruby.MrbState, self oruby.Value, dir string, enc oruby.Value, skipDots bool) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		args := mrb.GetArgs()
		if args.Len() > 0 {
			if kw := mrb.KeywordArgs(); !kw.IsNil() {
				return mrb.Call(self, "to_enum", mrb.GetMID(), args.Item(0), kw.Value())
			}
			return mrb.Call(self, "to_enum", mrb.GetMID(), args.Item(0))
		}
		return mrb.Call(self, "to_enum", mrb.GetMID())
	}
	extEnc, explicit, err := resolveDirEncoding(mrb, enc)
	if err != nil {
		return mrb.RaiseError(err)
	}
	err = eachName(dir, skipDots, func(f string) error {
		_ = mrb.Yield(block, encodeDirEntry(mrb, f, extEnc, explicit))
		if mrb.Exc() != nil {
			return mrb.Err()
		}
		return nil
	})
	if err != nil {
		return mrb.RaiseError(err)
	}
	if self.Type() == oruby.MrbTTClass {
		return mrb.NilValue()
	}
	return self
}

func dirForeachEnum(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	var callArgs []interface{}
	callArgs = append(callArgs, mrb.GetMID())
	for i := 0; i < args.Len(); i++ {
		callArgs = append(callArgs, args.Item(i))
	}
	if kw := mrb.KeywordArgs(); !kw.IsNil() {
		callArgs = append(callArgs, kw.Value())
	}
	return mrb.Call(self, "to_enum", callArgs...)
}

func dirForeach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if mrb.GetArgsBlock().IsNil() {
		return dirForeachEnum(mrb, self)
	}
	_, dir, err := pathValueAndString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	return dirDoForeach(mrb, self, dir, dirEncodingOpt(mrb), false)
}

func dirSEachChild(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if mrb.GetArgsBlock().IsNil() {
		return dirForeachEnum(mrb, self)
	}
	_, dir, err := pathValueAndString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	return dirDoForeach(mrb, self, dir, dirEncodingOpt(mrb), true)
}

func dirChildren(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	_, dir, err := pathValueAndString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	if dir == "" {
		return mrb.EArgumentError().Raise("ENOENT empty directory not allowed")
	}
	extEnc, explicit, err := resolveDirEncoding(mrb, dirEncodingOpt(mrb))
	if err != nil {
		return mrb.RaiseError(err)
	}
	ret := mrb.AryNew()
	err = eachName(dir, true, func(f string) error {
		ret.Push(encodeDirEntry(mrb, f, extEnc, explicit))
		return nil
	})
	if err != nil {
		return mrb.SysFail(err)
	}
	return ret
}

func dirChdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgs().Item(0)
	block := mrb.GetArgsBlock()
	ret := oruby.Int(0)
	var err error
	var dir string

	if arg.IsNil() {
		dir, err = os.UserHomeDir()
		if err != nil {
			if u, uerr := user.Current(); uerr == nil && u.HomeDir != "" {
				dir = u.HomeDir
			} else {
				return mrb.RaiseError(err)
			}
		}
	} else {
		dir, err = pathString(mrb, arg)
		if err != nil {
			return raisePathError(mrb, err)
		}
	}

	if block.IsNil() {
		err = os.Chdir(dir)
		if err != nil {
			return mrb.SysFail(err)
		}

		return ret
	}

	wdir, err := os.Getwd()
	if err != nil {
		return mrb.SysFail(err)
	}

	err = os.Chdir(dir)
	if err != nil {
		return mrb.SysFail(err)
	}

	ret = mrb.YieldArgv(block, mrb.StrNew(dir))

	if err := os.Chdir(wdir); err != nil {
		return mrb.SysFail(err)
	}
	return ret
}

func dirGetwd(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir, err := os.Getwd()
	if err != nil {
		return mrb.SysFail(err)
	}
	return mrb.StrNew(dir)
}

func dirMkdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	dir, err := pathString(mrb, args.Item(0))
	if err != nil {
		return raisePathError(mrb, err)
	}
	perm := int(os.ModePerm)
	if args.Len() > 1 {
		perm, err = toInt(mrb, args.Item(1))
		if err != nil {
			return raisePathError(mrb, err)
		}
	}
	err = os.Mkdir(dir, os.FileMode(perm))
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int(0)
}

func dirRmdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir, err := pathString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	err = dirSysRmdir(dir)
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int(0)
}

func dirHome(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	usr := mrb.GetArgsFirst()
	if usr.IsNil() {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			u, uerr := user.Current()
			if uerr != nil {
				return mrb.EArgumentError().Raise("couldn't find HOME environment -- expanding `~'")
			}
			home = u.HomeDir
		}
		return mrb.StrNew(home)
	}

	name, err := pathString(mrb, usr)
	if err != nil {
		return raisePathError(mrb, err)
	}
	if name == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			if u, uerr := user.Current(); uerr == nil {
				home = u.HomeDir
			}
		}
		return mrb.StrNew(home)
	}
	foundUser, err := user.Lookup(name)
	if err != nil {
		return mrb.EArgumentError().Raisef("user %s doesn't exist", name)
	}
	return mrb.StrNew(foundUser.HomeDir)
}

// Dir.__oruby_glob(pattern, flags, base, sort): the paths pattern matches
// (glob.go); Dir.glob and Dir.[] (dirGlobRb) sort out the arguments
func dirGlob(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	pat, flags, base, sorted := a.Item(0), a.Item(1), a.Item(2), a.Item(3)
	b := ""
	if base.IsString() {
		b = mrb.String(base)
	}
	paths := globPatterns([]string{mrb.String(pat)}, flags.Int(), b, b != "", sorted.Bool())
	ret := mrb.AryNewCapa(len(paths))
	for _, p := range paths {
		ret.PushString(p)
	}
	return ret
}

const dirGlobRb = `
class Dir
  # a NUL byte: a lone String pattern is MRI's old NUL-separated list, an
  # element of several a path name with a NUL in it
  def self.__oruby_glob_pattern(pat, single)
    pat = pat.to_path if !(String === pat) && pat.respond_to?(:to_path)
    pat = pat.to_str if !(String === pat) && pat.respond_to?(:to_str)
    raise TypeError, "no implicit conversion of #{pat.class} into String" unless String === pat
    if pat.include?("\0")
      raise ArgumentError, single ? "nul-separated glob pattern is deprecated" : "path name contains null byte"
    end
    pat
  end

  def self.__oruby_glob_run(pats, flags, base, sort, single)
    raise ArgumentError, "expected true or false as sort: #{sort.inspect}" unless true.equal?(sort) || false.equal?(sort)
    unless base.nil?
      base = base.path if Dir === base
      base = base.to_path if !(String === base) && base.respond_to?(:to_path)
      raise TypeError, "no implicit conversion of #{base.class} into String" unless String === base
      base = nil if base.empty?
    end
    out = []
    pats.each do |pat|
      pat = __oruby_glob_pattern(pat, single)
      enc = pat.encoding
      unless enc.respond_to?(:ascii_compatible?)
        # no Encoding support loaded: bytes in, bytes out
        out.concat(__oruby_glob(pat, flags, base, sort))
        next
      end
      raise Encoding::CompatibilityError, "incompatible character encodings: #{enc} and US-ASCII" unless enc.ascii_compatible?
      # the file system's names are UTF-8: a pattern in another encoding is
      # matched as UTF-8, the paths come back in its encoding
      conv = !pat.ascii_only? && enc != Encoding::UTF_8 && enc != Encoding::BINARY
      __oruby_glob(conv ? pat.encode(Encoding::UTF_8) : pat, flags, base, sort).each do |r|
        out << (conv ? r.force_encoding(Encoding::UTF_8).encode(enc) : r.force_encoding(enc))
      end
    end
    out
  end

  # MRI's dir_s_glob: flags positionally or as flags: (which wins)
  def self.glob(pattern, pflags = 0, flags: nil, base: nil, sort: true, &block)
    flags = pflags if flags.nil?
    unless Integer === flags
      raise TypeError, "no implicit conversion of #{flags.class} into Integer" unless flags.respond_to?(:to_int)
      flags = flags.to_int
    end
    r = __oruby_glob_run(Array === pattern ? pattern : [pattern], flags, base, sort, !(Array === pattern))
    return r unless block
    r.each(&block)
    nil
  end

  def self.[](*patterns, base: nil, sort: true)
    __oruby_glob_run(patterns, 0, base, sort, patterns.size == 1)
  end
end
`

// Dir.scan(path, encoding: nil) / Dir#scan (Ruby 4.1): [name, type] of
// each entry but "." and "..", type from the directory listing (:file,
// :directory, :link, :fifo, :socket, :characterSpecial, :blockSpecial,
// :unknown); names in the given encoding, Encoding.default_internal, or the
// filesystem one. With a block each pair is yielded and nil returned.
func dirSScan(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	path, err := pathString(mrb, args.Item(0))
	if err != nil {
		return raisePathError(mrb, err)
	}
	enc := mrb.NilValue()
	if kw := mrb.KeywordArgs(); !kw.IsNil() {
		enc = mrb.HashFetch(kw.Value(), mrb.SymbolValue(oruby.Sym_encoding), mrb.NilValue())
	}
	return scanDir(mrb, path, enc, block)
}

func dirScan(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	return scanDir(mrb, mrb.String(mrb.GetIV(self, "@path")), mrb.GetIV(self, "@encoding"), block)
}

func scanType(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m&os.ModeSymlink != 0:
		return "link"
	case m&os.ModeNamedPipe != 0:
		return "fifo"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeCharDevice != 0:
		return "characterSpecial"
	case m&os.ModeDevice != 0:
		return "blockSpecial"
	case m.IsRegular():
		return "file"
	}
	return "unknown"
}

func scanDir(mrb *oruby.MrbState, path string, enc oruby.Value, block oruby.RProc) oruby.MrbValue {
	entries, err := os.ReadDir(path)
	if err != nil {
		return mrb.SysFail(err)
	}
	// the names' encoding: the option, the default internal one, or none
	// to set (UTF-8: the filesystem encoding)
	if enc.IsNil() {
		if mrb.RespondTo(mrb.EncodingClass(), oruby.Sym_default_internal) {
			if e, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_default_internal); err == nil {
				enc = e.Value()
			}
		}
	} else if !enc.IsData() {
		e, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_find, enc)
		if err != nil {
			return mrb.RaiseError(err) // an unknown encoding name, as MRI
		}
		enc = e.Value()
	}
	ret := mrb.AryNew()
	for _, e := range entries {
		name := mrb.StrNew(e.Name())
		if !enc.IsNil() {
			_, _ = mrb.Funcall(name, oruby.Sym_force_encoding, enc)
		}
		pair := mrb.AryNewFromValues(name.Value(), mrb.SymbolValue(mrb.Intern(scanType(e.Type()))))
		if block.IsNil() {
			ret.Push(pair.Value())
		} else {
			mrb.Yield(block, pair.Value())
		}
	}
	if !block.IsNil() {
		return mrb.NilValue()
	}
	return ret
}

func dirExist(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.FSPath(mrb.GetArgsFirst())
	stat, err := os.Stat(dir)
	if err != nil {
		return oruby.False
	}
	return oruby.Bool(stat.IsDir())
}

func dirIsEmpty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if strings.ContainsRune(arg.String(), 0) {
		return mrb.EArgumentError().Raise("string contains null byte")
	}
	dir := mrb.FSPath(arg)

	f, err := os.Open(dir)
	if err != nil {
		return mrb.RaiseError(err)
	}

	files, err := f.Readdirnames(1)
	_ = f.Close()

	return oruby.Bool(err == io.EOF && len(files) == 0)
}

func dirCloseRaw(mrb *oruby.MrbState, self oruby.Value) error {
	if mrb.GetIV(self, "@closed").Bool() {
		return nil
	}
	mrb.SetIV(self, "@closed", true)
	f, _ := mrb.Data(self).(*os.File)
	if f != nil {
		return f.Close()
	}
	return nil
}

func dirClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := dirCloseRaw(mrb, self); err != nil {
		return raiseDirErrno(mrb, err, "closedir")
	}
	return mrb.NilValue()
}

func dirCollectChildren(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if _, err := dirFile(mrb, self); err != nil {
		return raiseDirError(mrb, err)
	}
	ret, _ := mrb.FuncallWithBlock(mrb.ClassOf(self), oruby.Sym_children, mrb.GetIV(self, "@path"))
	if enc := mrb.GetIV(self, "@encoding"); !enc.IsNil() && ret.Value().IsArray() {
		entryEncoding(mrb, ret.Value(), enc)
	}
	return ret
}

// entryEncoding gives the names in ary encoding enc (an Encoding or a
// name), or Encoding.default_internal when enc is nil
func entryEncoding(mrb *oruby.MrbState, ary, enc oruby.Value) {
	if enc.IsNil() {
		if mrb.RespondTo(mrb.EncodingClass(), oruby.Sym_default_internal) {
			if e, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_default_internal); err == nil {
				enc = e.Value()
			}
		}
		if enc.IsNil() {
			return
		}
	} else if !enc.IsData() {
		e, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_find, enc)
		if err != nil {
			return
		}
		enc = e.Value()
	}
	for i := 0; i < oruby.RArrayLen(ary); i++ {
		_, _ = mrb.Funcall(mrb.AryEntry(ary, i), oruby.Sym_force_encoding, enc)
	}
}

func dirPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetIV(self, "@path")
}

func dirInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := fmt.Sprintf("#<Dir:%v>", mrb.GetIV(self, "@path"))
	return mrb.StrNew(s)
}

func dirSetPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := dirFile(mrb, self)
	if err != nil {
		return raiseDirError(mrb, err)
	}
	newPos := mrb.GetArgs().Item(0)
	if !newPos.IsInt() {
		return mrb.EArgumentError().Raise("integer position expected")
	}

	pos := mrb.GetIV(self, "@pos").Int()

	delta := newPos.Int() - pos

	if delta == 0 {
		return newPos
	} else if delta < 0 {
		dirRewind(mrb, self)
		if mrb.Exc() != nil {
			return mrb.NilValue()
		}
		f, _ = mrb.Data(self).(*os.File)
		delta = newPos.Int()
	}

	if delta > 0 {
		// first two entries are "." and ".."
		skip := delta
		if pos < 2 {
			avail := 2 - pos
			if skip <= avail {
				skip = 0
			} else {
				skip -= avail
			}
		}
		if skip > 0 {
			_, err := f.Readdirnames(skip)
			if err != nil && err != io.EOF {
				return mrb.SysFail(err)
			}
		}
	}

	mrb.SetIV(self, "@pos", newPos)
	return newPos
}

func dirSeek(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dirSetPos(mrb, self)
	return self
}

func dirTell(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if _, err := dirFile(mrb, self); err != nil {
		return raiseDirError(mrb, err)
	}
	return mrb.GetIV(self, "@pos")
}

func dirRewind(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := dirFile(mrb, self)
	if err != nil {
		return raiseDirError(mrb, err)
	}
	pathVal := mrb.GetIV(self, "@path")
	if !pathVal.IsNil() {
		_ = f.Close()
		f, err = os.Open(mrb.FSPath(pathVal))
		if err != nil {
			return mrb.SysFail(err)
		}
		mrb.DataSetInterface(self, f)
	}

	mrb.SetIV(self, "@pos", 0)
	return self
}

func dirReadOne(mrb *oruby.MrbState, self oruby.Value) (oruby.Value, bool, error) {
	f, err := dirFile(mrb, self)
	if err != nil {
		return mrb.NilValue(), false, err
	}
	pos := mrb.GetIV(self, "@pos").Int()
	var ret string

	switch pos {
	case 0:
		ret = "."
	case 1:
		ret = ".."
	default:
		files, err := f.Readdirnames(1)
		if err == io.EOF {
			return mrb.NilValue(), false, nil
		} else if err != nil {
			return mrb.NilValue(), false, err
		}
		ret = files[len(files)-1]
	}

	mrb.SetIV(self, "@pos", pos+1)
	extEnc, explicit, err := resolveDirEncoding(mrb, mrb.GetIV(self, "@encoding"))
	if err != nil {
		return mrb.NilValue(), false, err
	}
	return encodeDirEntry(mrb, ret, extEnc, explicit), true, nil
}

func dirRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v, ok, err := dirReadOne(mrb, self)
	if err != nil {
		return raiseDirError(mrb, err)
	}
	if !ok {
		return mrb.NilValue()
	}
	return v
}

func dirEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return mrb.Call(self, "to_enum", mrb.GetMID())
	}
	if _, err := dirFile(mrb, self); err != nil {
		return raiseDirError(mrb, err)
	}
	if mrb.GetIV(self, "@pos").Int() > 0 {
		dirRewind(mrb, self)
		if mrb.Exc() != nil {
			return mrb.NilValue()
		}
	}
	for {
		v, ok, err := dirReadOne(mrb, self)
		if err != nil {
			return raiseDirError(mrb, err)
		}
		if !ok {
			break
		}
		_ = mrb.Yield(block, v)
		if mrb.Exc() != nil {
			return mrb.NilValue()
		}
	}
	return self
}

func dirEachChild(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return mrb.Call(self, "to_enum", mrb.GetMID())
	}
	if _, err := dirFile(mrb, self); err != nil {
		return raiseDirError(mrb, err)
	}
	if mrb.GetIV(self, "@pos").Int() > 0 {
		dirRewind(mrb, self)
		if mrb.Exc() != nil {
			return mrb.NilValue()
		}
	}
	for {
		v, ok, err := dirReadOne(mrb, self)
		if err != nil {
			return raiseDirError(mrb, err)
		}
		if !ok {
			break
		}
		s := v.String()
		if s == "." || s == ".." {
			continue
		}
		_ = mrb.Yield(block, v)
		if mrb.Exc() != nil {
			return mrb.NilValue()
		}
	}
	return self
}

func eachName(dir string, skipDots bool, f func(s string) error) error {
	fd, err := os.Open(dir)
	if err != nil {
		return err
	}
	names, err := fd.Readdirnames(-1)
	if err != nil {
		return err
	}
	defer fd.Close()

	if !skipDots {
		if err := f("."); err != nil {
			return err
		}
		if err := f(".."); err != nil {
			return err
		}
	}

	for _, name := range names {
		if err := f(name); err != nil {
			return err
		}
	}
	return nil
}
