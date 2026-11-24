package file

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/oruby/oruby"
	gemIO "github.com/oruby/oruby/gem/io"
	"golang.org/x/sys/unix"
)

func initIOMethods(mrb *oruby.MrbState) oruby.RClass {
	cIO := mrb.Class("IO")
	cIO.DefineClassMethod("sysopen", ioSysopen, mrb.ArgsArg(1, 2))
	cIO.DefineClassMethod("_binread", ioBinreadInternal, mrb.ArgsArg(1, 2))
	cIO.DefineClassMethod("_read", ioReadInternal, mrb.ArgsArg(1, 3))
	cIO.DefineClassMethod("_binwrite", ioBinwriteInternal, mrb.ArgsArg(2, 2))
	cIO.DefineClassMethod("_write", ioBinwriteInternal, mrb.ArgsArg(2, 2))
	cIO.DefineMethod("__oruby_reopen_io", ioReopenIO, mrb.ArgsReq(1))
	cIO.DefineMethod("__oruby_reopen_path", ioReopenPath, mrb.ArgsReq(3))

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

	f, err := platformOpenFile(mrb.FSPath(path.Value()), flags, os.FileMode(perm))
	if err != nil {
		return mrb.SysFail(err)
	}

	return mrb.FixnumValue(int(f))
}

func binaryStringValue(mrb *oruby.MrbState, buf []byte) oruby.Value {
	v := mrb.BytesValue(buf)
	if mrb.RespondTo(v, oruby.Sym_force_encoding) {
		modEnc := mrb.EncodingClass()
		if !modEnc.IsNil() && mrb.ConstDefined(modEnc, oruby.Sym_BINARY) {
			binEnc := mrb.ConstGet(modEnc, oruby.Sym_BINARY)
			mrb.Funcall(v, oruby.Sym_force_encoding, binEnc)
		} else {
			mrb.Funcall(v, oruby.Sym_force_encoding, mrb.StringValue("ASCII-8BIT"))
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

	if v, ok := mrb.OrbRelativePath(name); ok {
		name = v
	}
	if oruby.IsOrbVirtualPath(name) {
		data, ok := mrb.OrbReadAsset(name)
		if !ok {
			return mrb.SysFail(&os.PathError{Op: "open", Path: name, Err: syscall.ENOENT})
		}
		fileSize := int64(len(data))
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
		return binaryStringValue(mrb, data[offset:offset+toRead])
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
			modeVal := mrb.HashGet(opts, mrb.SymbolValue(oruby.Sym_mode))
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

	var buf []byte
	var n int
	if v, ok := mrb.OrbRelativePath(name); ok {
		name = v
	}
	if oruby.IsOrbVirtualPath(name) {
		data, ok := mrb.OrbReadAsset(name)
		if !ok {
			return mrb.SysFail(&os.PathError{Op: "open", Path: name, Err: syscall.ENOENT})
		}
		fileSize := int64(len(data))
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
		buf = data[offset : offset+toRead]
		n = len(buf)
	} else {
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

		buf = make([]byte, toRead)
		n, err = f.ReadAt(buf, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return mrb.SysFail(err)
		}
	}

	ret := mrb.StrNew(string(buf[:n]))
	if hasLength {
		if mrb.RespondTo(ret, oruby.Sym_force_encoding) {
			modEnc := mrb.EncodingClass()
			if !modEnc.IsNil() && mrb.ConstDefined(modEnc, oruby.Sym_BINARY) {
				binEnc := mrb.ConstGet(modEnc, oruby.Sym_BINARY)
				mrb.Funcall(ret, oruby.Sym_force_encoding, binEnc)
			}
		}
	} else if !opts.IsNil() && opts.Type() == oruby.MrbTTHash {
		encVal := mrb.HashGet(opts, mrb.SymbolValue(oruby.Sym_encoding))
		if encVal.IsNil() {
			encVal = mrb.HashGet(opts, mrb.StringValue("encoding"))
		}
		if encVal.IsNil() {
			encVal = mrb.HashGet(opts, mrb.SymbolValue(oruby.Sym_external_encoding))
			if encVal.IsNil() {
				encVal = mrb.HashGet(opts, mrb.StringValue("external_encoding"))
			}
		}
		if !encVal.IsNil() && mrb.RespondTo(ret, oruby.Sym_force_encoding) {
			mrb.Funcall(ret, oruby.Sym_force_encoding, encVal)
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
		s, err := mrb.Funcall(strVal, oruby.Sym_to_s)
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
			modeVal := mrb.HashGet(opts, mrb.SymbolValue(oruby.Sym_mode))
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

			flagsVal := mrb.HashGet(opts, mrb.SymbolValue(oruby.Sym_flags))
			if flagsVal.IsNil() {
				flagsVal = mrb.HashGet(opts, mrb.StringValue("flags"))
			}
			if !flagsVal.IsNil() && flagsVal.Type() == oruby.MrbTTFixnum {
				flags |= flagsVal.Int()
			}

			permVal := mrb.HashGet(opts, mrb.SymbolValue(oruby.Sym_perm))
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
		ret, err := os.OpenFile(mrb.FSPath(item), mode, 0666)
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

		ioObject, err = os.OpenFile(mrb.FSPath(fd), flags, 0666)
		if err != nil {
			return nil, err
		}
		return ioObject, nil
	case oruby.MrbTTFixnum:
		if f := gemIO.ClaimPipeFD(fd.Int()); f != nil {
			return f, nil // an end of IO.pipe
		}
		f := os.NewFile(uintptr(fd.Int()), "fd")
		if f == nil {
			return nil, oruby.EArgumentError("invalid file descriptor")
		}
		return keepFD(f), nil
	case oruby.MrbTTCptr:
		f := os.NewFile(fd.Uintptr(), "fd")
		if f == nil {
			return nil, oruby.EArgumentError("invalid file descriptor")
		}
		return keepFD(f), nil
	case oruby.MrbTTCData:
		ioObject = mrb.Data(fd)
		switch ioObject.(type) {
		case io.Reader, io.Writer, *os.File, *io.PipeReader, *io.PipeWriter:
			return ioObject, nil
		default:
			ret, err := mrb.FuncallWithBlock(fd, oruby.Sym_to_io)
			if err == nil {
				return mrb.Data(ret), nil
			}
		}
	case oruby.MrbTTObject:
		ret, err := mrb.FuncallWithBlock(fd, oruby.Sym_to_io)
		if err == nil {
			return mrb.Data(ret), nil
		}
	}

	return nil, oruby.EArgumentError("First argument must be fd or IO object")
}

// io.__oruby_reopen_io(other): self takes other's stream (MRI's io_reopen):
// other's descriptor is duplicated onto self's, at other's position
func ioReopenIO(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	other := mrb.GetArgsFirst()
	f, ok := mrb.Data(self).(*os.File)
	of, ok2 := mrb.Data(other).(*os.File)
	if !ok || !ok2 {
		return mrb.EArgumentError().Raise("reopen supported only for files")
	}
	gemIO.ResetReadBuffer(mrb, self)
	gemIO.DropReadBuffer(mrb, other)
	fd, ofd := gemIO.RawFd(f), gemIO.RawFd(of)
	if fd != ofd {
		if err := unix.Dup2(ofd, fd); err != nil {
			return mrb.SysFail(err)
		}
	}
	if fd > 2 {
		_ = gemIO.SetCloseOnExec(f, true)
	}
	mrb.SetIV(self, "@lineno", 0)
	mrb.ObjSetClass(self, mrb.ObjClass(other))
	return self
}

// io.__oruby_reopen_path(path, mode, opt): self reads/writes the file at
// path (MRI's rb_io_reopen). An open IO keeps its descriptor number (the
// file is duplicated onto it, so exec'd children see it); a closed one gets
// the new descriptor. mode nil: the IO's own open mode.
func ioReopenPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pathV, modeV, optV := mrb.GetArgs3()
	newPath, err := pathString(mrb, pathV)
	if err != nil {
		return raisePathError(mrb, err)
	}
	if modeV.IsNil() && (optV.IsNil() || mrb.HashFetch(optV, oruby.Sym_mode, mrb.NilValue()).IsNil()) {
		modeV, optV = mrb.GetIV(self, "@mode"), mrb.GetIV(self, "@opt")
	} else {
		mrb.SetIV(self, "@mode", modeV)
		mrb.SetIV(self, "@opt", optV)
	}
	flags, err := parseFlags(mrb, modeV, optV)
	if err != nil {
		return mrb.RaiseError(err)
	}
	nf, err := os.OpenFile(newPath, flags, 0666)
	if err != nil {
		return mrb.SysFail(err)
	}
	gemIO.ResetReadBuffer(mrb, self)
	mrb.SetIV(self, "@lineno", 0)
	mrb.SetIV(self, "@path", pathV)

	f, ok := mrb.Data(self).(*os.File)
	if ok && f != nil && !mrb.GetIV(self, "@closed").Bool() && gemIO.RawFd(f) >= 0 {
		fd := gemIO.RawFd(f)
		err := unix.Dup2(gemIO.RawFd(nf), fd)
		_ = nf.Close()
		if err != nil {
			return mrb.SysFail(err)
		}
		if fd > 2 {
			_ = gemIO.SetCloseOnExec(f, true)
		}
		return self
	}
	mrb.DataSetInterface(self, nf)
	mrb.SetIV(self, "@closed", mrb.FalseValue())
	return self
}

// externalFiles keeps the *os.File values made for descriptor numbers
// passed in from Ruby (IO.new(fd), IO.for_fd) reachable: Go's finalizer would
// otherwise close the number when the value is collected, even after Ruby
// closed it or the number was reused (the Go runtime's own kqueue/epoll
// descriptor included). Ruby code closes them explicitly, as in MRI.
var (
	externalMu    sync.Mutex
	externalFiles []*os.File
)

func keepFD(f *os.File) *os.File {
	externalMu.Lock()
	externalFiles = append(externalFiles, f)
	externalMu.Unlock()
	return f
}
