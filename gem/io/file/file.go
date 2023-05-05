package file

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oruby/oruby"
)

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

		mrb.DefineClassMethod(fileClass, "absolute_path", fileAbsolutePath, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "absolute_path?", fileIsAbsolutePath, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "atime", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "basename", fileBasename, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "birthtime", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "blockdev?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "chardev?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "chmod", fileChmod, mrb.ArgsReq(1)|mrb.ArgsRest())
		mrb.DefineClassMethod(fileClass, "chown", fileChown, mrb.ArgsReq(2)|mrb.ArgsRest())
		proxyClassMethodToStat(fileClass, "ctime", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "delete", fileUnlink, mrb.ArgsAny())
		proxyClassMethodToStat(fileClass, "directory?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "dirname", fileDirname, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "empty?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "executable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "executable_real?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "exist?", fileExist, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "exists?", fileExist, mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "expand_path", fileExpandPath, mrb.ArgsArg(1, 1))
		mrb.DefineClassMethod(fileClass, "extname", fileExtname, mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "file?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "match?", fileMatch, mrb.ArgsArg(2, 1))
		mrb.DefineClassMethod(fileClass, "fnmatch", fileFnMatch, mrb.ArgsArg(2, 1))
		mrb.DefineClassMethod(fileClass, "fnmatch?", fileFnMatch, mrb.ArgsArg(2, 1))
		proxyClassMethodToStat(fileClass, "ftype", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "grpowned", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "identical?", fileIdentical, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "join", fileJoin, mrb.ArgsAny())
		mrb.DefineClassMethod(fileClass, "lchown", fileLchown, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "link", fileLink, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "lstat", fileLStat, mrb.ArgsReq(1))
		//lchmod
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
		proxyClassMethodToStat(fileClass, "symlink?", mrb.ArgsReq(1))
		mrb.DefineClassMethod(fileClass, "truncate", fileTruncate, mrb.ArgsReq(2))
		mrb.DefineClassMethod(fileClass, "umask", fileUmask, mrb.ArgsOpt(1))
		mrb.DefineClassMethod(fileClass, "unlink", fileUnlink, mrb.ArgsAny())
		proxyClassMethodToStat(fileClass, "utime", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "world_readable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "world_writable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "writable?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "writable_real?", mrb.ArgsReq(1))
		proxyClassMethodToStat(fileClass, "zero?", mrb.ArgsReq(1))

		mrb.DefineClassMethod(fileClass, "open", fileOpen, mrb.ArgsReq(1))
		mrb.DefineMethod(fileClass, "initialize", fileInit, mrb.ArgsReq(1))

		proxyMethodToStat(fileClass, "atime", mrb.ArgsNone())
		mrb.DefineMethod(fileClass, "chmod", fileFChmod, mrb.ArgsReq(1))
		mrb.DefineMethod(fileClass, "chown", fileFChown, mrb.ArgsReq(2))
		proxyMethodToStat(fileClass, "birthtime", mrb.ArgsNone())
		proxyMethodToStat(fileClass, "ctime", mrb.ArgsNone())
		mrb.DefineMethod(fileClass, "flock", fileFlock, mrb.ArgsReq(1))
		mrb.DefineMethod(fileClass, "lstat", fileLStat, mrb.ArgsReq(1))
		proxyMethodToStat(fileClass, "mtime", mrb.ArgsNone())
		proxyMethodToStat(fileClass, "size", mrb.ArgsNone())
		mrb.DefineMethod(fileClass, "to_path", fileToPath, mrb.ArgsReq(1))
		mrb.DefineMethod(fileClass, "truncate", fileFTruncate, mrb.ArgsReq(1))
		return nil
	})
}

func proxyClassMethodToStat(fileClass oruby.RClass, name string, args oruby.MrbAspec) {
	fileClass.DefineClassMethod(name, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ai := mrb.GCArenaSave()
		defer mrb.GCArenaRestore(ai)

		stat, err := statFirst(mrb)
		if err != nil {
			return mrb.SysFail(err)
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

		f, ok := mrb.Data(self).(*os.File)
		if !ok {
			mrb.EArgumentError().Raise("file object expected")
		}

		stat, err := f.Stat()
		if err != nil {
			return mrb.SysFail(err)
		}

		ret, err := mrb.FuncallWithBlock(mrb.DataValue(stat), mrb.GetMID())
		if err != nil {
			return mrb.RaiseError(err)
		}

		return ret
	}, args)
}

func fileUnlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := mrb.GetArgs()
	for i := 0; i < names.Len(); i++ {
		if err := os.Remove(names.Item(i).String()); err != nil {
			return mrb.RaiseError(err)
		}
	}
	return oruby.Integer(names.Len())
}

func fileRename(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := mrb.GetArgs()
	err := os.Rename(names.Item(0).String(), names.Item(1).String())
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileSymlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := mrb.GetArgs()
	err := os.Symlink(names.Item(0).String(), names.Item(1).String())
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileLink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := mrb.GetArgs()
	err := os.Link(names.Item(0).String(), names.Item(1).String())
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileChmod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	mode := os.FileMode(args.Item(0).Int())
	for i := 1; i < args.Len(); i++ {
		if err := os.Chmod(args.Item(i).String(), mode); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(args.Len() - 1)
}

func fileChown(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	uid := -1
	uidV := args.Item(0)
	gid := args.Item(1).Int()
	if !uidV.IsNil() {
		uid = uidV.Int()
	}

	for i := 2; i < args.Len(); i++ {
		if err := os.Chown(args.Item(i).String(), uid, gid); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(args.Len() - 2)
}

func fileLchown(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	uid := -1
	uidV := args.Item(0)
	gid := args.Item(1).Int()
	if !uidV.IsNil() {
		uid = uidV.Int()
	}

	for i := 2; i < args.Len(); i++ {
		if err := os.Lchown(args.Item(i).String(), uid, gid); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(args.Len() - 2)
}

func fileReadlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	ret, err := os.Readlink(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	return mrb.StrNew(ret)
}

func fileJoin(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := mrb.GetArgs()
	elements := make([]string, names.Len())
	for i := 0; i < names.Len(); i++ {
		elements[i] = names.Item(i).String()
	}
	return mrb.StrNew(filepath.Join(elements...))
}

func fileExpandPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, dir := mrb.GetArgs2("", "")
	pth := filepath.Join(dir.String(), name.String())
	idx := strings.Index(pth, "~")
	if idx >= 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			mrb.RaiseError(err)
		}
		pth = filepath.Join(pth[:idx], home, pth[idx+1:])
	}
	pth = filepath.Clean(pth)
	return mrb.StrNew(pth)
}

func fileExtname(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	if len(name) > 0 && name[0] == '.' {
		return mrb.StrNew(filepath.Ext(name[1:]))
	}
	return mrb.StrNew(filepath.Ext(name))
}

func filePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgs().Item(0)
	if arg.IsString() {
		return arg
	}
	ret, err := mrb.FuncallWithBlock(arg, mrb.Intern("to_path"))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return ret
}

func getStat(mrb *oruby.MrbState, f oruby.Value) (os.FileInfo, error) {
	if f.IsString() {
		return os.Stat(f.String())
	}
	if !f.IsData() {
		return nil, oruby.EArgumentError("argument error, expected file name or IO object")
	}
	if s, ok := mrb.Data(f).(os.FileInfo); ok {
		return s, nil
	}
	file, ok := mrb.Data(f).(*os.File)
	if !ok {
		return nil, oruby.EArgumentError("argument error, IO object does not support stat")
	}
	return file.Stat()
}

func fileIdentical(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f1, f2 := mrb.GetArgs2()

	stat1, err1 := getStat(mrb, f1)
	stat2, err2 := getStat(mrb, f2)

	if err1 != nil || err2 != nil {
		if f1.IsData() && f2.IsData() {
			return oruby.Bool(mrb.Data(f1) == mrb.Data(f2))
		} else {
			return mrb.SysFail(errors.New("argument error, expected file name or IO object"))
		}
	}

	return oruby.Bool(os.SameFile(stat1, stat2))
}

func fileSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	dir, file := filepath.Split(name)
	return mrb.AryNewFromValues(mrb.StrNew(filepath.Clean(dir)), mrb.StrNew(file))
}

func fileDirname(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	f := mrb.ClassGet("File")
	sep := f.ConstGet("SEPARATOR").String() + mrb.String(f.ConstGet("ALT_SEPARATOR"))

	idx := strings.LastIndexAny(name, sep)
	if idx < 0 {
		idx = 0
	}

	ok := false
	for i := idx; i >= 0; i-- {
		if !ok && !strings.Contains(sep, string(name[i])) {
			ok = true
			continue
		}
		if ok && strings.Contains(sep, string(name[i])) {
			continue
		}
		name = name[:i]
		break
	}

	return mrb.StrNew(name)
}

func fileTruncate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, size := mrb.GetArgs2()
	if err := os.Truncate(name.String(), size.Int64()); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	stat, err := os.Stat(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	return mrb.DataValue(stat)
}

func fileLStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	stat, err := os.Lstat(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	return mrb.DataValue(stat)
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

func fileBasename(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	n, s := mrb.GetArgs2("", "")
	name := filepath.Base(n.String())
	suffix := s.String()

	if suffix != "" {
		ext := filepath.Ext(name)
		if (suffix == ext) || (suffix == ".*") {
			name = strings.TrimSuffix(name, ext)
		}
	}

	return mrb.StrNew(name)
}

func fileRealpath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, dir := mrb.GetArgs2("", "")
	pth := filepath.Join(filepath.Clean(dir.String()), name.String())
	_, err := os.Stat(pth)
	if err != nil {
		return mrb.SysFail(err)
	}
	abspath, err := filepath.Abs(name.String())
	return mrb.StrNew(abspath)
}

func fileRealdirpath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, dir := mrb.GetArgs2("", "")
	pth := filepath.Join(filepath.Clean(dir.String()), name.String())
	fileName := filepath.Base(pth)
	stat, err := os.Stat(filepath.Dir(pth))
	if err != nil {
		return mrb.SysFail(err)
	}
	absdir, err := filepath.Abs(stat.Name())
	return mrb.StrNew(filepath.Join(absdir, fileName))
}

func fileExist(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	if _, err := os.Stat(name); err != nil {
		return oruby.Bool(!errors.Is(err, os.ErrNotExist))
	}
	return oruby.True
}

func fileAbsolutePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, dir := mrb.GetArgs2("")
	var pth string
	if dir.IsNil() {
		pth = name.String()
	} else {
		pth = filepath.Join(dir.String(), name.String())
	}
	ret, err := filepath.Abs(pth)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return mrb.StrNew(ret)
}

func fileIsAbsolutePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, dir := mrb.GetArgs2("", "")
	pth := filepath.Join(dir.String(), name.String())
	return oruby.Bool(filepath.IsAbs(pth))
}

func fileFTruncate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	size := mrb.GetArgsFirst().Int64()

	if err := f.Truncate(size); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileFChmod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	mode := mrb.GetArgsFirst().Int()

	if err := f.Chmod(os.FileMode(mode)); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileFChown(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	args := mrb.GetArgs()
	uid := -1
	uidV := args.Item(0)
	gid := args.Item(1).Int()
	if !uidV.IsNil() {
		uid = uidV.Int()
	}

	if err := f.Chown(uid, gid); err != nil {
		return mrb.SysFail(err)
	}

	return oruby.Integer(0)
}

func fileToPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	return mrb.StrNew(f.Name())
}

func openFile(mrb *oruby.MrbState, args oruby.RArgs) (*os.File, error) {
	var err error
	name := args.Item(0).String()
	flags := 0
	optFlags := 0
	perm := 0755
	opt := args.Item(-1)

	if opt.IsHash() {
		optFlags, err = modeToFlags(mrb, opt)
		if err != nil {
			return nil, err
		}
	} else {
		opt = mrb.NilValue()
	}

	switch args.Len() {
	case 2:
		if opt.IsNil() {
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
		if opt.IsNil() {
			perm = args.Item(2).Int()
		}
	case 4:
		flags, err = modeToFlags(mrb, args.Item(1))
		if err != nil {
			return nil, err
		}
		perm = args.Item(2).Int()
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
	ai := mrb.GCArenaSave()
	defer mrb.GCArenaRestore(ai)
	args, block := mrb.GetArgsWithBlock()

	f, err := openFile(mrb, args)
	if err != nil {
		mrb.SysFail(err)
	}
	file := mrb.DataValue(f)
	if block.IsNil() {
		return file
	}
	ret := mrb.YieldArgv(block, file)
	if mrb.Exc() != nil {
		return mrb.Exc()
	}
	return ret
}

func fileInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := openFile(mrb, mrb.GetArgs())
	if err != nil {
		mrb.SysFail(err)
	}

	mrb.DataSetInterface(self, f)
	return self
}

func fileFnMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	p, n, f := mrb.GetArgs3("", "", 0)
	name := n.String()
	pattern := p.String()
	flags := f.Int()

	ret, err := fnmatch(name, pattern, flags)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Bool(ret)
}
