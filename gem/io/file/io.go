package file

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/oruby/oruby"
	gemIO "github.com/oruby/oruby/gem/io"
)

func initIOMethods(mrb *oruby.MrbState) oruby.RClass {
	cIO := mrb.Class("IO")
	cIO.DefineClassMethod("sysopen", ioSysopen, mrb.ArgsArg(1, 2))
	cIO.DefineClassMethod("_binread", ioBinreadInternal, mrb.ArgsArg(1, 2))
	cIO.DefineClassMethod("_read", ioReadInternal, mrb.ArgsArg(1, 3))
	cIO.DefineClassMethod("_binwrite", ioBinwriteInternal, mrb.ArgsArg(2, 2))
	cIO.DefineClassMethod("_write", ioBinwriteInternal, mrb.ArgsArg(2, 2))
	cIO.DefineMethod("reopen", ioReopen, mrb.ArgsArg(1, 2))

	_, err := mrb.Eval(`
class IO
  def self.binread(*args)
    if args.length < 1 || args.length > 3
      raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 1..3)"
    end
    name, length, offset = args
    offset ||= 0
    raise ArgumentError, "negative length #{length} given" if length && length < 0
    raise SystemCallError.new(22) if offset && offset < 0
    _binread(name, length, offset)
  end

  def self.binwrite(*args, **opts)
    if args.length < 2 || args.length > 3
      raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 2..3)"
    end
    name, string, offset = args
    _binwrite(name, string, offset, opts)
  end

  def self.read(*args, **opts)
    if args.length < 1 || args.length > 3
      raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 1..3)"
    end
    name, length, offset = args
    offset ||= 0
    raise ArgumentError, "negative offset #{offset} given" if offset && offset < 0
    raise ArgumentError, "negative length #{length} given" if length && length < 0
    if opts.key?(:open_args)
      oargs = opts[:open_args]
      oargs = [oargs] unless oargs.is_a?(Array)
      return File.open(name, *oargs) do |f|
        f.seek(offset) if offset && offset > 0
        length ? f.read(length) : f.read
      end
    end
    _read(name, length, offset, opts)
  end

  def self.write(*args, **opts)
    if args.length < 2 || args.length > 3
      raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 2..3)"
    end
    name, string, offset = args
    _write(name, string, offset, opts)
  end
end
`)
	if err != nil {
		panic(err)
	}

	ioData := mrb.GemData("io").(*gemIO.IoData)
	ioData.GetStream = fileGetStream
	ioData.OpenIO = fileOpenIO

	return cIO
}

// TODO: this will leak file descriptors, if not properly closed
//
//	this method returns int; MRI closes fd when returned variablee is GC-ed
//	mruby Int/Fixnum is not in GC arena.
//	mruby DATA structure does get free
func ioSysopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	path := args.Item(0)
	mode := args.Item(1)
	perm := args.ItemDefInt(2, 0666)

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

	return mrb.FixnumValue(int(f))
}

func binaryStringValue(mrb *oruby.MrbState, buf []byte) oruby.Value {
	v := mrb.BytesValue(buf)
	if mrb.RespondTo(v, mrb.Intern("force_encoding")) {
		modEnc := mrb.ModuleGet("Encoding")
		if !modEnc.IsNil() && mrb.ConstDefined(modEnc, mrb.Intern("BINARY")) {
			binEnc := mrb.ConstGet(modEnc, mrb.Intern("BINARY"))
			mrb.Funcall(v, mrb.Intern("force_encoding"), binEnc)
		} else {
			mrb.Funcall(v, mrb.Intern("force_encoding"), mrb.StringValue("ASCII-8BIT"))
		}
	}
	return v
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

func ioBinreadInternal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) < 1 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1..3)")
	}

	nameVal := rawArgs[0]
	name, err := pathString(mrb, nameVal)
	if err != nil {
		return raisePathError(mrb, err)
	}

	var hasLength bool
	var length int64
	if len(rawArgs) >= 2 && !rawArgs[1].IsNil() {
		hasLength = true
		length = rawArgs[1].Int64()
		if length < 0 {
			return mrb.Raise(mrb.EArgumentError(), "negative length given")
		}
		if length == 0 {
			return binaryStringValue(mrb, []byte(""))
		}
	}

	var offset int64
	if len(rawArgs) >= 3 && !rawArgs[2].IsNil() {
		offset = rawArgs[2].Int64()
		if offset < 0 {
			return mrb.SysFail(syscall.EINVAL)
		}
	}

	f, err := os.Open(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return mrb.SysFail(err)
	}
	fileSize := stat.Size()

	if offset >= fileSize {
		if hasLength {
			return mrb.NilValue()
		}
		return binaryStringValue(mrb, []byte(""))
	}

	rem := fileSize - offset
	toRead := rem
	if hasLength && length < rem {
		toRead = length
	}

	buf := make([]byte, toRead)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return mrb.SysFail(err)
	}

	return binaryStringValue(mrb, buf[:n])
}

func ioReadInternal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) < 1 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1..4)")
	}

	nameVal := rawArgs[0]
	name, err := pathString(mrb, nameVal)
	if err != nil {
		return raisePathError(mrb, err)
	}

	var hasLength bool
	var length int64
	if len(rawArgs) >= 2 && !rawArgs[1].IsNil() {
		hasLength = true
		length = rawArgs[1].Int64()
		if length < 0 {
			return mrb.Raise(mrb.EArgumentError(), "negative length given")
		}
		if length == 0 {
			return binaryStringValue(mrb, []byte(""))
		}
	}

	var offset int64
	if len(rawArgs) >= 3 && !rawArgs[2].IsNil() {
		offset = rawArgs[2].Int64()
		if offset < 0 {
			return mrb.SysFail(syscall.EINVAL)
		}
	}

	var opts oruby.Value
	if len(rawArgs) >= 4 && !rawArgs[3].IsNil() {
		opts = rawArgs[3]
		if opts.Type() == oruby.MrbTTHash {
			modeVal := mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("mode")))
			if modeVal.IsNil() {
				modeVal = mrb.HashGet(opts, mrb.StringValue("mode"))
			}
			if !modeVal.IsNil() {
				m := mrb.String(modeVal)
				if m == "w" || m == "w+" || (!strings.Contains(m, "r") && !strings.Contains(m, "+")) {
					return gemIO.RaiseIOError(mrb, "not opened for reading")
				}
			}
		}
	}

	f, err := os.Open(name)
	if err != nil {
		return mrb.SysFail(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return mrb.SysFail(err)
	}
	fileSize := stat.Size()

	if offset >= fileSize {
		if hasLength {
			return mrb.NilValue()
		}
		return mrb.StrNew("")
	}

	rem := fileSize - offset
	toRead := rem
	if hasLength && length < rem {
		toRead = length
	}

	buf := make([]byte, toRead)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return mrb.SysFail(err)
	}

	ret := mrb.StrNew(string(buf[:n]))
	if hasLength {
		if mrb.RespondTo(ret, mrb.Intern("force_encoding")) {
			modEnc := mrb.ModuleGet("Encoding")
			if !modEnc.IsNil() && mrb.ConstDefined(modEnc, mrb.Intern("BINARY")) {
				binEnc := mrb.ConstGet(modEnc, mrb.Intern("BINARY"))
				mrb.Funcall(ret, mrb.Intern("force_encoding"), binEnc)
			}
		}
	} else if !opts.IsNil() && opts.Type() == oruby.MrbTTHash {
		encVal := mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("encoding")))
		if encVal.IsNil() {
			encVal = mrb.HashGet(opts, mrb.StringValue("encoding"))
		}
		if encVal.IsNil() {
			encVal = mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("external_encoding")))
			if encVal.IsNil() {
				encVal = mrb.HashGet(opts, mrb.StringValue("external_encoding"))
			}
		}
		if !encVal.IsNil() && mrb.RespondTo(ret, mrb.Intern("force_encoding")) {
			mrb.Funcall(ret, mrb.Intern("force_encoding"), encVal)
		}
	}
	return ret
}

func ioBinwriteInternal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) < 2 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 2..4)")
	}

	nameVal := rawArgs[0]
	name, err := pathString(mrb, nameVal)
	if err != nil {
		return raisePathError(mrb, err)
	}

	strVal := rawArgs[1]
	var strBytes []byte
	if oruby.MrbStringP(strVal) {
		strBytes = []byte(mrb.String(strVal))
	} else {
		s, err := mrb.Funcall(strVal, mrb.Intern("to_s"))
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), "cannot convert to string")
		}
		strBytes = []byte(mrb.String(s))
	}

	hasOffset := len(rawArgs) >= 3 && !rawArgs[2].IsNil()
	var offset int64
	if hasOffset {
		offset = rawArgs[2].Int64()
	}

	flags := os.O_CREATE | os.O_WRONLY
	defaultTrunc := !hasOffset
	perm := 0666

	if len(rawArgs) >= 4 && !rawArgs[3].IsNil() {
		opts := rawArgs[3]
		if opts.Type() == oruby.MrbTTHash {
			modeVal := mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("mode")))
			if modeVal.IsNil() {
				modeVal = mrb.HashGet(opts, mrb.StringValue("mode"))
			}
			if !modeVal.IsNil() {
				m := mrb.String(modeVal)
				if m == "r" || m == "rb" {
					return gemIO.RaiseIOError(mrb, "not opened for writing")
				}
				if strings.Contains(m, "w") {
					defaultTrunc = true
				} else if strings.Contains(m, "a") {
					defaultTrunc = false
					flags |= os.O_APPEND
				}
			}

			flagsVal := mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("flags")))
			if flagsVal.IsNil() {
				flagsVal = mrb.HashGet(opts, mrb.StringValue("flags"))
			}
			if !flagsVal.IsNil() && flagsVal.Type() == oruby.MrbTTFixnum {
				flags |= flagsVal.Int()
			}

			permVal := mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("perm")))
			if permVal.IsNil() {
				permVal = mrb.HashGet(opts, mrb.StringValue("perm"))
			}
			if !permVal.IsNil() && permVal.Type() == oruby.MrbTTFixnum {
				perm = permVal.Int()
			}
		}
	}

	if defaultTrunc {
		flags |= os.O_TRUNC
	}

	fd, err := platformOpenFile(name, flags, os.FileMode(perm))
	if err != nil {
		return mrb.SysFail(err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()

	var n int
	if hasOffset {
		n, err = f.WriteAt(strBytes, offset)
	} else {
		n, err = f.Write(strBytes)
	}
	if err != nil {
		return mrb.SysFail(err)
	}

	return mrb.FixnumValue(n)
}

func fileGetStream(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, error) {
	switch item.Type() {
	case oruby.MrbTTString:
		ret, err := os.OpenFile(item.String(), mode, 0666)
		return ret, err
	case oruby.MrbTTCData:
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

		ioObject, err = os.OpenFile(fd.String(), flags, 0666)
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
	case oruby.MrbTTCData:
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
		return mrb.EArgumentError().Raise("reopen supported only for files")
	}

	rawArgs := mrb.GetArgs().Slice()
	if len(rawArgs) == 0 {
		stat, err := f.Stat()
		if err != nil {
			return mrb.RaiseError(err)
		}
		if err := f.Close(); err != nil {
			return mrb.RaiseError(err)
		}
		flags, _ := parseFlags(mrb, mrb.GetIV(self, "@mode"), mrb.GetIV(self, "@opt"))
		reopened, err := os.OpenFile(f.Name(), flags, stat.Mode().Perm())
		if err != nil {
			return mrb.RaiseError(err)
		}
		mrb.SetIV(self, "@lineno", 0)
		mrb.DataSetInterface(self, reopened)
		return self
	}

	target := rawArgs[0]
	if mrb.RespondTo(target, mrb.Intern("to_io")) {
		otherVal, err := mrb.Funcall(target, mrb.Intern("to_io"))
		if err == nil {
			if otherFile, ok := mrb.Data(otherVal).(*os.File); ok {
				_ = syscall.Dup2(int(otherFile.Fd()), int(f.Fd()))
				mrb.SetIV(self, "@path", mrb.GetIV(otherVal, "@path"))
				return self
			}
		}
	}

	newPath, err := pathString(mrb, target)
	if err != nil {
		return raisePathError(mrb, err)
	}

	flags := os.O_RDONLY
	if len(rawArgs) >= 2 && !rawArgs[1].IsNil() {
		flags, err = modeToFlags(mrb, rawArgs[1])
		if err != nil {
			return mrb.RaiseError(err)
		}
	}

	_ = f.Close()

	reopened, err := os.OpenFile(newPath, flags, 0666)
	if err != nil {
		return mrb.SysFail(err)
	}

	mrb.SetIV(self, "@path", target)
	mrb.SetIV(self, "@lineno", 0)
	mrb.DataSetInterface(self, reopened)
	return self
}
