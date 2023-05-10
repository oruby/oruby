package file

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"

	"github.com/oruby/oruby"
)

func initDir(mrb *oruby.MrbState) {
	dirClass := mrb.DefineClass("Dir", mrb.ObjectClass())
	dirClass.Include(mrb.ModuleGet("Enumerable"))
	oruby.MrbSetInstanceTT(dirClass, oruby.MrbTTCData)

	dirClass.DefineClassMethod("open", dirOpen, mrb.ArgsArg(1, 1))
	dirClass.DefineClassMethod("foreach", dirForeach, mrb.ArgsArg(1, 1))
	dirClass.DefineClassMethod("entries", dirEntries, mrb.ArgsArg(1, 1))
	dirClass.DefineClassMethod("each_child", dirSEachChild, mrb.ArgsArg(1, 1))
	dirClass.DefineClassMethod("children", dirChildren, oruby.ArgsReq(1))

	dirClass.DefineMethod("initialize", dirInitialize, mrb.ArgsArg(1, 1))
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

	dirClass.DefineClassMethod("glob", dirGlob, mrb.ArgsArg(1, 2))
	dirClass.DefineClassMethod("[]", dirAref, mrb.ArgsReq(1)|mrb.ArgsRest())
	dirClass.DefineClassMethod("exist?", dirExist, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("exists?", dirExist, mrb.ArgsReq(1))
	dirClass.DefineClassMethod("empty?", dirIsEmpty, mrb.ArgsReq(1))
}

func dirOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	ret, err := mrb.ObjNew(mrb.ClassPtr(self), mrb.GetArgs().SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if block.IsNil() {
		return ret
	}
	result := mrb.Yield(block, ret)
	_ = ret.Data().(*os.File).Close()
	return result
}

func dirInitialize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	f, err := os.Open(dir)
	if err != nil {
		return mrb.RaiseError(err)
	}
	mrb.SetIV(self, "@path", dir)
	mrb.SetIV(self, "@pos", 0)
	mrb.DataSetInterface(self, f)

	return self
}

func dirEntries(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	f, err := os.Open(dir)
	if err != nil {
		return mrb.FalseValue()
	}

	files, err := f.Readdirnames(-1)
	_ = f.Close()
	ret := mrb.AryNewCapa(len(files) + 2)
	ret.PushString(".")
	ret.PushString("..")
	for _, f := range files {
		ret.PushString(f)
	}
	return ret
}

func dirDoForeach(mrb *oruby.MrbState, self oruby.Value, dir string, skipDots bool) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	ret := mrb.NilValue()
	if !block.IsNil() {
		ret = mrb.AryNew().Value()
	}
	err := eachName(dir, skipDots, func(f string) error {
		if !block.IsNil() {
			_ = mrb.Yield(block, mrb.StrNew(f))
			if mrb.Exc() != nil {
				return mrb.Err()
			}
			return nil
		}
		mrb.AryPush(ret, mrb.StrNew(f))
		return nil
	})
	if err != nil {
		return mrb.RaiseError(err)
	}

	if block.IsNil() {
		return mrb.Call(self, "to_enum", mrb.GetMID())
	}
	return ret
}

func dirForeach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	return dirDoForeach(mrb, self, dir, false)
}

func dirSEachChild(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	return dirDoForeach(mrb, self, dir, true)
}

func dirChildren(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	if dir == "" {
		return mrb.EArgumentError().Raise("ENOENT empty directory not allowed")
	}
	ret := mrb.AryNew()
	err := eachName(dir, true, func(f string) error {
		ret.PushString(f)
		return nil
	})
	if err != nil {
		return mrb.RaiseError(err)
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
			return mrb.RaiseError(err)
		}
	} else {
		dir = arg.String()
	}

	if block.IsNil() {
		err = os.Chdir(dir)
		if err != nil {
			return mrb.RaiseError(err)
		}

		return ret
	}

	wdir, err := os.Getwd()
	if err != nil {
		return mrb.RaiseError(err)
	}

	err = os.Chdir(dir)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret = mrb.YieldArgv(block, mrb.StrNew(dir))

	if err := os.Chdir(wdir); err != nil {
		return mrb.RaiseError(err)
	}
	return ret
}

func dirGetwd(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir, err := os.Getwd()
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(dir)
}

func dirMkdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir, perm := mrb.GetArgs2("", os.ModePerm)
	err := os.Mkdir(dir.String(), os.FileMode(perm.Int()))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(0)
}

func dirRmdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	err := os.Remove(dir)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(0)
}

func dirHome(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	usr := mrb.GetArgsFirst()
	if usr.IsNil() {
		home, err := os.UserHomeDir()
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.StrNew(home)
	}

	foundUser, err := user.Lookup(usr.String())
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(foundUser.HomeDir)
}

func dirGlob(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var err error
	args, block := mrb.GetArgsWithBlock()
	patt := args.Item(0)
	basePath := ""
	ret := mrb.NilValue()
	var patterns []string

	switch patt.Type() {
	case oruby.MrbTTString:
		patterns = []string{patt.String()}
	case oruby.MrbTTArray:
		patterns = make([]string, patt.Len())
		for i := 0; i < patt.Len(); i++ {
			patterns[i] = mrb.AryEntry(patt, i).String()
		}
	default:
		return mrb.EArgumentError().Raise("pattern must be string or array of strings")
	}

	flags := 0
	if args.Item(1).IsInt() {
		flags = args.ItemDefInt(1, flags)
	}
	flags |= fnmExtglob | fnmPathname

	opt := args.KeywordArgs()
	if opt.IsHash() {
		if base := mrb.HashFetch(opt, mrb.Intern("base"), oruby.Nil); base.IsString() {
			basePath = base.String()
		}
	}

	if block.IsNil() {
		ret = mrb.AryNew().Value()
	}

	if basePath != "" {
		wd, err := os.Getwd()
		if err != nil {
			return mrb.RaiseError(err)
		}
		err = os.Chdir(basePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ret
			}
			return mrb.RaiseError(err)
		}
		defer os.Chdir(wd)
		basePath = ""
	}

	err = glob(patterns, flags, basePath, func(f string) error {
		if block.IsNil() {
			mrb.AryPush(ret, mrb.StrNew(f))
		} else {
			_ = mrb.Yield(block, mrb.StrNew(f))
			if mrb.Exc() != nil {
				return mrb.Err()
			}
		}
		return nil
	})
	if err != nil {
		return mrb.RaiseError(err)
	}
	return ret
}

func dirAref(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	basePath := ""
	opt := args.KeywordArgs()
	if opt.IsHash() {
		if base := mrb.HashFetch(opt, mrb.Intern("base"), oruby.Nil); base.IsString() {
			basePath = base.String()
		}
	}

	patterns := make([]string, 0, args.Len())
	for i := 0; i < args.Len(); i++ {
		if arg := args.Item(i); arg.IsString() {
			patterns = append(patterns, arg.String())
		}
	}

	ret := mrb.AryNew()
	if basePath != "" {
		wd, err := os.Getwd()
		if err != nil {
			return mrb.RaiseError(err)
		}
		err = os.Chdir(basePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ret
			}
			return mrb.RaiseError(err)
		}
		defer os.Chdir(wd)
		basePath = ""
	}

	err := glob(patterns, fnmExtglob|fnmPathname, basePath, func(f string) error {
		ret.PushString(f)
		return nil
	})
	if err != nil {
		return mrb.RaiseError(err)
	}
	return ret
}

func dirExist(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	stat, err := os.Stat(dir)
	if err != nil {
		return oruby.False
	}
	return oruby.Bool(stat.IsDir())
}

func dirIsEmpty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()

	f, err := os.Open(dir)
	if err != nil {
		return mrb.FalseValue()
	}

	files, err := f.Readdirnames(1)
	_ = f.Close()

	return oruby.Bool(err == io.EOF && len(files) == 0)
}

func dirClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	_ = f.Close()
	return mrb.NilValue()
}

func dirCollectChildren(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ret, _ := mrb.FuncallWithBlock(mrb.ClassOf(self), mrb.Intern("children"), mrb.GetIV(self, "@path"))
	return ret
}

func dirPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetIV(self, "@path")
}

func dirInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := fmt.Sprintf("#<Dir:%v>", mrb.GetIV(self, "@path"))
	return mrb.StrNew(s)
}

func dirSetPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
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
		delta = newPos.Int()
	}

	if delta > 0 {
		f := mrb.Data(self).(*os.File)
		_, err := f.Readdirnames(delta)
		if err != nil {
			return mrb.RaiseError(err)
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
	return mrb.GetIV(self, "@pos")
}

func dirRewind(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	_ = f.Close()
	f, err := os.Open(mrb.GetIV(self, "@path").String())
	if err != nil {
		return mrb.RaiseError(err)
	}
	mrb.DataSetInterface(self, f)

	mrb.SetIV(self, "@pos", 0)
	return self
}

func dirRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
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
			return mrb.NilValue()
		} else if err != nil {
			return mrb.RaiseError(err)
		}
		ret = files[len(files)-1]
	}

	mrb.SetIV(self, "@pos", pos+1)
	return mrb.StrNew(ret)
}

func dirEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetIV(self, "@path").String()
	return dirDoForeach(mrb, self, dir, false)
}

func dirEachChild(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetIV(self, "@path").String()
	return dirDoForeach(mrb, self, dir, true)
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
