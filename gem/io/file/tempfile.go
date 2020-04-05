package file

import (
	"github.com/oruby/oruby"
	"io/ioutil"
	"os"
)

type tmpfile struct {
	*os.File
}

func (f tmpfile) Free() {
	_= f.Close()
	_= os.Remove(f.Name())
}

func initTmpFile(mrb *oruby.MrbState, fileClass oruby.RClass) {
	tmp := mrb.DefineClass("Tempfile", fileClass)
	tmp.DefineClassMethod("create", tmpCreate, mrb.ArgsOpt(4))
	tmp.DefineClassMethod("open",   tmpOpen, mrb.ArgsOpt(4))
	tmp.DefineMethod("initialize",  tmpInit, mrb.ArgsOpt(4))
	tmp.DefineMethod("close", tmpClose, mrb.ArgsOpt(1))
	tmp.DefineMethod("close!", tmpCloseBang, mrb.ArgsNone())
	tmp.DefineMethod("delete", tmpUnlink, mrb.ArgsNone())
	tmp.DefineMethod("length", tmpSize, mrb.ArgsNone())
	tmp.DefineMethod("open", tmpReopen, mrb.ArgsNone())
	tmp.DefineMethod("path", tmpPath, mrb.ArgsNone())
	tmp.DefineMethod("size", tmpSize, mrb.ArgsNone())
	tmp.DefineMethod("unlink", tmpUnlink, mrb.ArgsNone())
}

func openTmpFile(mrb *oruby.MrbState, args oruby.RArgs) (string, string, int, int, error) {
	var err error
	var name string
	nameV := args.Item(0)
	dir := ""
	if args.Item(1).IsString() {
		dir = args.Item(1).String()
	}

	flags := os.O_RDWR|os.O_CREATE|os.O_EXCL
	perm := 0600
	opt := args.GetLastHash()

	if nameV.IsArray() {
		name = mrb.String(mrb.AryEntry(nameV, 0)) + "*" +
			mrb.String(mrb.AryEntry(nameV, 1))
	} else {
		name = nameV.String()
	}

	if opt.IsHash() {
		flags = mrb.HashFetch(opt, mrb.Intern("mode"), oruby.Int(flags)).Int()
	}

	return name, dir, flags, perm, err
}

func tmpCreate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	ret, err := mrb.ObjNew(mrb.ClassPtr(self), mrb.GetArgs().SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if block.IsNil() {
		return ret
	}
	result, _ := mrb.Yield(block, ret)
	tmpCloseBang(mrb, ret.Value())

	return result
}

func tmpOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
  	block := mrb.GetArgsBlock()
  	ret, err := mrb.ObjNew(mrb.ClassPtr(self), mrb.GetArgs().SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}
  	if block.IsNil() {
  		return ret
	}
	result, _ := mrb.Yield(block, ret)
	tmpCloseBang(mrb, ret.Value())

	return result
}

// TODO set unlink finalizer on temp files
func tmpInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	name, dir, flags, perm, err := openTmpFile(mrb, args)
	if err != nil {
		return mrb.RaiseError(err)
	}

	f, err := ioutil.TempFile(dir, name)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if perm != 0600 {
		if err := f.Chmod(os.FileMode(perm)); err != nil {
			return mrb.RaiseError(err)
		}
	}
	mrb.SetIV(self, "@mode", flags)
	mrb.SetIV(self, "@perm", perm)
	mrb.DataSetInterface(self, f)

	if flags != os.O_RDWR|os.O_CREATE|os.O_EXCL {
		return tmpReopen(mrb, self)
	}
	return self
}

func tmpClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	unlink := mrb.GetArgsFirst().Bool()
	f := mrb.Data(self).(*os.File)
	_= f.Close()
	mrb.SetIV(self, "@closed", true)
	if unlink {
		return tmpUnlink(mrb, self)
	}
	return mrb.NilValue()
}

func tmpCloseBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	_= f.Close()
	mrb.SetIV(self, "@closed", true)
	return tmpUnlink(mrb, self)
}

func tmpReopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	mode := mrb.GetIV(self, "@mode").Int()
	perm := mrb.GetIV(self, "@perm").Int()

	f := mrb.Data(self).(*os.File)
	err := f.Close()
	if err != nil {
		return mrb.RaiseError(err)
	}
	f, err = os.OpenFile(f.Name(), mode, os.FileMode(perm))
	if err != nil {
		return mrb.RaiseError(err)
	}

	mrb.DataSetInterface(self, f)
	return self
}

func tmpPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if mrb.GetIV(self, "@closed").Bool() {
		return mrb.NilValue()
	}
	f := mrb.Data(self).(*os.File)
	return mrb.StrNew(f.Name())
}

func tmpSize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	stat, err := f.Stat()
	if err != nil {
		mrb.RaiseError(err)
	}
	return oruby.Int64(stat.Size())
}

func tmpUnlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	err := os.Remove(f.Name())
	if err != nil {
		return mrb.NilValue()
	}
	return mrb.TrueValue()
}

