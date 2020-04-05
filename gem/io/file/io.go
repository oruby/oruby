package file

import (
	"errors"
	"github.com/oruby/oruby"
	gemIO "github.com/oruby/oruby/gem/io"
	"io"
	"io/ioutil"
	"os"
)

func initIOMethods(mrb *oruby.MrbState) oruby.RClass {
	cIO := mrb.Class("IO")
	cIO.DefineClassMethod("sysopen", ioSysopen, mrb.ArgsArg(1, 2))
	cIO.DefineClassMethod("binread", ioBinread, mrb.ArgsArg(1,2))
	cIO.DefineClassMethod("binwrite", ioBinwrite, mrb.ArgsArg(2,2))
	cIO.DefineClassMethod("read", ioBinread, mrb.ArgsArg(1, 3))
	cIO.DefineClassMethod("write", ioBinwrite, mrb.ArgsArg(2, 2))
	cIO.DefineMethod("reopen", ioReopen, mrb.ArgsArg(1,2))

	ioData := mrb.GemData("io").(*gemIO.IoData)
	ioData.GetStream = fileGetStream
	ioData.OpenIO = fileOpenIO

	return cIO
}

// TODO: this will leak file descriptors, if not properly closed
//       this method returns int; MRI closes fd when returned variablee is GC-ed
//       mruby Int/Fixnum is not in GC arena.
//       mruby DATA structure does get free
func ioSysopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	path := args.Item(0)
	mode := args.Item(1)
	perm := args.ItemDefInt(2, 0)

	flags, err := modeToFlags(mrb, mode)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if flags == 0 {
		flags = os.O_RDONLY
	}

	f, err := platformOpenFile(mrb.String(path), flags, os.FileMode(perm))
	if err != nil {
		return mrb.SysFail(err)
	}

	return mrb.Value(uintptr(f))
}

func rwNameLenOffset(name string, offset int64, length *int64) (*os.File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}

	if *length == -1 {
		stat, err := f.Stat()
		if err != nil {
			return f, err
		}

		*length = stat.Size() - offset
	}

	return f, nil
}

func ioBinread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, lengthV, offsetV := mrb.GetArgs3("", -1,0)
	offset := offsetV.Int64()
	length := lengthV.Int64()

	if (length == -1) && (offset == 0) {
		b, err := ioutil.ReadFile(name.String())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.BytesValue(b)
	}

	f, err := rwNameLenOffset(name.String(), offset, &length)
	if err != nil {
		return mrb.RaiseError(err)
	}
	defer f.Close()

	buf := make([]byte, length)
	_,err = f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return mrb.RaiseError(err)
	}

	return mrb.BytesValue(buf)
}

func ioBinwrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, str, offset := mrb.GetArgs3("", "", 0)

	f, err := os.OpenFile(name.String(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0755)
	if err != nil {
		return mrb.RaiseError(err)
	}

	n, err := f.WriteAt(str.Bytes(), offset.Int64())
	if err != nil  {
		return mrb.RaiseError(err)
	}

	if err := f.Close(); err != nil {
		return mrb.RaiseError(err)
	}

	return mrb.FixnumValue(n)
}

func fileGetStream(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, error) {
	switch item.Type() {
	case oruby.MrbTTString:
		ret, err := os.OpenFile(item.String(), mode, 0755)
		return ret, err
	case oruby.MrbTTData:
		return mrb.Data(item), nil
	}
	return nil, oruby.EArgumentError("IO Stream or name expected")
}

func fileOpenIO(mrb *oruby.MrbState, fd, mode, opt oruby.Value) (interface{}, error) {
	var ioObject interface{}

	switch fd.Type() {
	case oruby.MrbTTString:
		flags, err := parseFlags(mrb, mode, opt)
		if err != nil {
			return nil, err
		}

		if flags == 0 {
			flags = os.O_RDONLY
		}

		ioObject, err = os.OpenFile(fd.String(), flags, 0777)
		if err != nil {
			return nil, err
		}
		return ioObject, nil
	case oruby.MrbTTFixnum:
		ioObject = os.NewFile(uintptr(fd.Int()), "fd")
		if ioObject == nil {
			return nil, oruby.EArgumentError("invalid file descriptor")
		}
		return ioObject, nil
	case oruby.MrbTTCptr:
		ioObject = os.NewFile(fd.Uintptr(), "fd")
		if ioObject == nil {
			return nil, oruby.EArgumentError("invalid file descriptor")
		}
		return ioObject, nil
	case oruby.MrbTTData:
		ioObject = mrb.Data(fd)
		switch ioObject.(type) {
		case io.Reader, io.Writer, *os.File, *io.PipeReader, *io.PipeWriter:
			return ioObject, nil
		default:
			ret, err := mrb.FuncallWithBlock(fd, mrb.Intern("to_io"))
			if err == nil {
				return mrb.Data(ret), nil
			}
		}
	case oruby.MrbTTObject:
		ret, err := mrb.FuncallWithBlock(fd, mrb.Intern("to_io"))
		if err == nil {
			return mrb.Data(ret), nil
		}
	}

	return nil, oruby.EArgumentError("First argument must be fd or IO object")
}

func ioReopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, ok := mrb.Data(self).(*os.File)
	if !ok {
		return mrb.EArgumentError().Raise("reopen suported only for files")
	}

	stat, err := f.Stat()
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err := f.Close(); err != nil {
		return mrb.RaiseError(err)
	}

	flags, err := parseFlags(mrb, mrb.GetIV(self, "@mode"), mrb.GetIV(self, "@opt"))

	reopened, err := os.OpenFile(f.Name(), flags, stat.Mode().Perm())
	if err != nil {
		return mrb.RaiseError(err)
	}

	mrb.SetIV(self, "@lineno", 0)
	mrb.DataSetInterface(self, reopened)
	return self
}
