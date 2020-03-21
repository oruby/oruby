package io

import (
	"bufio"
	"errors"
	"github.com/oruby/oruby"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
)

func init() {
	oruby.Gem("io", func(mrb *oruby.MrbState) interface{} {
		cIO := mrb.DefineClass("IO", mrb.ObjectClass())
		cIO.AttachType((*io.PipeWriter)(nil))
		cIO.AttachType((*io.PipeReader)(nil))
		cIO.AttachType((*bufio.Writer)(nil))
		cIO.AttachType((*bufio.Reader)(nil))
		cIO.AttachType((*superPipe)(nil))

		cIO.Include(mrb.ModuleGet("Enumerable"))

		// Not implemented: select and popen with fork command '-')
		cIO.DefineClassMethod("binread", ioBinread, mrb.ArgsArg(1,2))
		cIO.DefineClassMethod("binwrite", ioBinwrite, mrb.ArgsArg(2,2))
		cIO.DefineClassMethod("copy_stream", ioCopyStream, mrb.ArgsArg(2,2))
		cIO.DefineClassMethod("foreach", ioForeach, mrb.ArgsArg(2,3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("pipe", ioPipe, mrb.ArgsOpt(3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("popen", ioPopen, mrb.ArgsReq(1)+mrb.ArgsRest())
		cIO.DefineClassMethod("read", ioBinread, mrb.ArgsArg(1, 3))
		cIO.DefineClassMethod("readlines", ioSReadlines, mrb.ArgsArg(2, 3))
 		cIO.DefineClassMethod("select", mrb.NotImplemented, mrb.ArgsArg(1, 3))
		cIO.DefineClassMethod("sysopen", ioSSysopen, mrb.ArgsArg(1, 2))
		cIO.DefineClassMethod("try_convert", ioTryConvert, mrb.ArgsReq(1))
		cIO.DefineClassMethod("write", ioBinwrite, mrb.ArgsArg(2, 2))
		cIO.DefineClassMethod("open", ioOpen, mrb.ArgsArg(1, 2)|mrb.ArgsBlock())
		cIO.DefineClassMethod("for_fd", ioNew, mrb.ArgsArg(1, 2))

		cIO.DefineMethod("initialize", ioInit, mrb.ArgsArg(1, 2))
		cIO.DefineMethod("initialize_copy", ioInitCopy, mrb.ArgsReq(1))
		cIO.DefineMethod("<<", ioWriteString, mrb.ArgsReq(1))
		cIO.DefineMethod("advise", mrb.NotImplemented, mrb.ArgsArg(1,2))
		cIO.DefineMethod("autoclose=", ioSetAutosclose, mrb.ArgsReq(1))
		cIO.DefineMethod("autoclose?", ioIsAutoclose, mrb.ArgsNone())
		cIO.DefineMethod("binmode", ioBinmode, mrb.ArgsNone())
		cIO.DefineMethod("binmode?", ioIsBinbode, mrb.ArgsNone())
		//cIO.DefineMethod("bytes", ioBytes, mrb.ArgsArg()) // Deprecated
		//cIO.DefineMethod("chars", ioChars, mrb.ArgsArg()) // Deprecated
		cIO.DefineMethod("close", ioClose, mrb.ArgsNone())
		cIO.DefineMethod("close_on_exec=", mrb.NotImplemented, mrb.ArgsReq(1))
		cIO.DefineMethod("close_on_exec?", ioIsCloseOnExec, mrb.ArgsNone())
		cIO.DefineMethod("close_read", ioCloseRead, mrb.ArgsNone())
		cIO.DefineMethod("close_write", ioCloseWrite, mrb.ArgsNone())
		cIO.DefineMethod("closed?", ioIsClosed, mrb.ArgsNone())
		cIO.DefineMethod("each", ioEach, mrb.ArgsOpt(3)|mrb.ArgsBlock())
		cIO.DefineAlias("each_line", "each")
		cIO.DefineMethod("each_byte", ioEachByte, mrb.ArgsBlock())
		cIO.DefineMethod("each_char", ioEachChar, mrb.ArgsBlock())
		cIO.DefineMethod("each_codepoint", ioEachCodepoint, mrb.ArgsBlock())
		cIO.DefineAlias("codepoints", "each_codepoint")
		cIO.DefineMethod("eof?", ioIsEof, mrb.ArgsNone())
		cIO.DefineAlias("eof", "eof?")
		//cIO.DefineMethod("external_encoding", ioExternalEncoding, mrb.ArgsNone())
		cIO.DefineMethod("fcntl", mrb.NotImplemented, mrb.ArgsReq(2))
		cIO.DefineMethod("fdatasync", ioFsync, mrb.ArgsNone())
		cIO.DefineMethod("fileno", ioFileno, mrb.ArgsNone())
		cIO.DefineMethod("flush", ioFlush, mrb.ArgsNone())
		cIO.DefineMethod("fsync", ioFsync, mrb.ArgsNone())
		cIO.DefineMethod("getbyte", ioGetbyte, mrb.ArgsNone())
		cIO.DefineMethod("getc", ioGetc, mrb.ArgsNone())
		cIO.DefineMethod("gets", ioGets, mrb.ArgsOpt(3))
		cIO.DefineMethod("inspect", ioInspect, mrb.ArgsNone())
		//cIO.DefineMethod("internal_encoding", ioInternalEncoding, mrb.ArgsNone())
		cIO.DefineMethod("ioctl", mrb.NotImplemented, mrb.ArgsReq(2))
		cIO.DefineMethod("isatty", mrb.NotImplemented, mrb.ArgsNone())
		cIO.DefineMethod("lineno", ioLineno, mrb.ArgsNone())
		cIO.DefineMethod("lineno=", ioSetLineno, mrb.ArgsReq(1))
		cIO.DefineAlias("lines", "each_line")
		cIO.DefineMethod("pid", ioPid, mrb.ArgsNone())
		cIO.DefineMethod("pos", ioPos, mrb.ArgsNone())
		cIO.DefineMethod("pos=", ioSetPos, mrb.ArgsReq(1))
		cIO.DefineMethod("pread", ioPread, mrb.ArgsArg(2,1))
		cIO.DefineMethod("print", ioPrint, mrb.ArgsAny())
		cIO.DefineMethod("printf", ioPrintf, mrb.ArgsReq(1)|mrb.ArgsAny())
		cIO.DefineMethod("putc", ioPutc, mrb.ArgsReq(1))
		cIO.DefineMethod("puts", ioPuts, mrb.ArgsAny())
		cIO.DefineMethod("pwrite", ioPwrite, mrb.ArgsReq(2))
		cIO.DefineMethod("read", ioRead, mrb.ArgsOpt(2))
		cIO.DefineMethod("readbyte", ioReadbyte, mrb.ArgsNone())
		cIO.DefineMethod("readchar", ioReadchar, mrb.ArgsNone())
		cIO.DefineMethod("readline", ioReadline, mrb.ArgsOpt(3))
		cIO.DefineMethod("readlines", ioReadlines, mrb.ArgsArg(1,2))
		cIO.DefineMethod("readpartial", ioReadpartial, mrb.ArgsArg(1,1))
		cIO.DefineMethod("reopen", ioReopen, mrb.ArgsArg(1,2))
		cIO.DefineMethod("rewind", ioRewind, mrb.ArgsNone())
		cIO.DefineMethod("seek", ioSeek, mrb.ArgsArg(1,1))
		//cIO.DefineMethod("set_encoding", ioSet_encoding, mrb.ArgsArg(1,2))
		//cIO.DefineMethod("set_encoding_by_bom", ioSetEncodingByBom, mrb.ArgsNone())
		cIO.DefineMethod("stat", ioStat, mrb.ArgsNone())
		cIO.DefineMethod("sync", ioSync, mrb.ArgsNone())
		cIO.DefineMethod("sync=", ioSetSync, mrb.ArgsReq(1))
		cIO.DefineMethod("sysread", ioReadpartial, mrb.ArgsArg(1,1))
		cIO.DefineMethod("sysseek", ioSeek, mrb.ArgsArg(1,1))
		cIO.DefineMethod("syswrite", ioWrite, mrb.ArgsReq(1))
		cIO.DefineMethod("tell", ioTell, mrb.ArgsNone())
		cIO.DefineMethod("to_i", ioFileno, mrb.ArgsNone())
		cIO.DefineMethod("to_io", ioToIo, mrb.ArgsNone())
		cIO.DefineMethod("tty?", mrb.NotImplemented, mrb.ArgsNone())
		cIO.DefineMethod("ungetbyte", ioUngetbyte, mrb.ArgsReq(1))
		cIO.DefineMethod("ungetc", ioUngetc, mrb.ArgsReq(1))
		cIO.DefineMethod("write", ioWrite, mrb.ArgsAny())

		cIO.DefineMethod("readNonblock",  mrb.NotImplemented, mrb.ArgsArg(1,2))
		cIO.DefineMethod("writeNonblock", mrb.NotImplemented, mrb.ArgsArg(1,1))

		ioError := mrb.DefineClass("IOError", mrb.EStandardErrorClass())
		mrb.DefineClass("EOFError", ioError)

		initFile(mrb, cIO)

		return nil
	})
}

func raiseEOF(mrb *oruby.MrbState) oruby.Value {
	return mrb.Raise(mrb.ExcGet("EOFError"), "")
}

func raiseIOError(mrb *oruby.MrbState, msg string) oruby.Value {
	return mrb.Raise(mrb.ExcGet("IOError"), msg)
}

func raiseIOErrorf(mrb *oruby.MrbState, format string, args... interface{}) oruby.Value {
	return mrb.Raisef(mrb.ExcGet("IOError"), format, args...)
}

func getStream(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, error) {
	switch item.Type() {
	case oruby.MrbTTString:
		ret, err := os.OpenFile(item.String(), mode, 0755)
		return ret, err
	case oruby.MrbTTData:
		return mrb.Data(item), nil
	}
	return nil, oruby.EArgumentError("IO Stream or name expected")
}

func closeStream(s interface{}, isFilePath bool) error {
	if !isFilePath {
		return nil
	}
	if closer, ok := s.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func ioCopyStream(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	from := args.Item(0)
	to   := args.Item(1)

	src, err := getStream(mrb, os.O_RDONLY, from)
	if err != nil {
		return mrb.RaiseError(err)
	}
	defer closeStream(src, from.IsString())

	dest, err := getStream(mrb, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, to)
	if err != nil {
		return mrb.RaiseError(err)
	}

	offset := int64(args.ItemDefInt(3, 0))
	if offset > 0 {
		if seeker, ok := src.(io.Seeker); ok {
			// If IO object, and offset is set copy_stream doesn’t move the current file offset
			if !from.IsString() {
				oldPos, err := seeker.Seek(0, io.SeekCurrent)
				if err != nil {
					_= closeStream(dest, to.IsString())
					return mrb.RaiseError(err)
				}
				defer func(){ _,_= seeker.Seek(oldPos, io.SeekStart) }()
			}

			if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
				_= closeStream(dest, to.IsString())
				return mrb.RaiseError(err)
			}
		}
	}

	var ret int64

	length := int64(args.ItemDefInt(2, -1))
	if length < 0 {
		ret, err = io.Copy(dest.(io.Writer), src.(io.Reader))
	} else {
		ret, err = io.CopyN(dest.(io.Writer), src.(io.Reader), length)
	}
	if err != nil {
		_= closeStream(dest, to.IsString())
		return mrb.RaiseError(err)
	}

	// Destination could error on close, discarding writes
	if err = closeStream(dest, to.IsString()); err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int64(ret)
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

func modestrToFlags(mode string) (int, error) {
	if mode == "" {
		return 0, nil
	}

	flags := 0
	switch mode[0] {
	case 'r':
		flags = os.O_RDONLY
	case 'w':
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case 'a':
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	default:
		return 0, oruby.EArgumentError("illegal access mode %v", mode)
	}

	if len(mode) == 1 {
		return flags, nil
	}

	for _, m := range mode[1:] {
		switch m {
		case 'b':
			//flags |= os.O_BINARY
		case 't':
			//flags |= os.O_TEXT
		case '+':
			flags = (flags & ^(os.O_RDONLY | os.O_WRONLY | os.O_RDWR)) | os.O_RDWR
		case 'x':
			if mode[0] != 'w' {
				return 0, oruby.EArgumentError("illegal access mode %v", mode)
			}
			flags |= os.O_EXCL
		case ':':
			// ignore BOM
			goto end
		default:
			return 0, oruby.EArgumentError("illegal access mode %v", mode)
		}
	}

end:
	return flags, nil
}

func modeToFlags(mrb *oruby.MrbState, mode oruby.Value) (int, error) {
	if mode.IsNil() {
		// Default is RDONLY
		return 0, nil

	} else if mode.IsString() {
		// mode string: 'rw+'
		return modestrToFlags(mode.String())

	} else if mode.IsFixnum() {
		// mode integer: File::RDONLY|File::EXCL
		return mode.Int(), nil

	} else if mode.IsHash() {
		// mode: 'rw+', flags: File::EXCL
		optMode := mrb.HashFetch(mode, mrb.Intern("mode"), mrb.NilValue())
		mFlags, err := modestrToFlags(optMode.String())
		if err != nil {
			return 0, err
		}

		fFlags := mrb.HashFetch(mode, mrb.Intern("flags"), oruby.Int(0)).Int()
		return mFlags|fFlags, nil

	} else  {
		return 0, oruby.EArgumentError("illegal access mode %v", mrb.Inspect(mode))
	}
}

func parseFlags(mrb *oruby.MrbState, mode, optHash oruby.Value) (int, error) {
	flags,err := modeToFlags(mrb, mode)
	if err != nil {
		return 0, err
	}

	flagsOpt,err := modeToFlags(mrb, optHash)
	if err != nil {
		return 0, err
	}

	return flags|flagsOpt, nil
}

// TODO: this will leak file descriptors, if not properly closed
//       this method returns int; MRI closes fd when returned variablee is GC-ed
//       mruby Int/Fixnum is not in GC arena.
//       mruby DATA structure does get free
func ioSSysopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
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

func openIO(mrb *oruby.MrbState, fd, mode, opt oruby.Value) (interface{}, error) {
	var ioObject interface{}

	switch fd.Type() {
	case oruby.MrbTTFixnum:
		ioObject = os.NewFile(uintptr(fd.Int()), "fd")
	case oruby.MrbTTCptr:
		ioObject = os.NewFile(fd.Uintptr(), "fd")
	case oruby.MrbTTString:
		flags, err := parseFlags(mrb, mode, opt)
		if err != nil {
			return nil, err
		}

		if flags == 0 {
			flags = os.O_RDONLY
		}

		ioObject, err = os.OpenFile(fd.String(), flags, 0)
		if err != nil {
			return nil, err
		}
	case oruby.MrbTTData:
		ioObject = mrb.Data(fd)
		switch ioObject.(type) {
		case io.Reader, io.Writer, *os.File, *io.PipeReader, *io.PipeWriter:
			// These are OK as IO object data
		default:
			return nil, oruby.EArgumentError("First argument must be fd or IO object")
		}

	default:
		return nil, oruby.EArgumentError("First argument must be fd or IO object")
	}

	return ioObject, nil
}

func ioInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, mode, opt := mrb.GetArgs3()
	ioObject, err := openIO(mrb, fd, mode, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	mrb.DataSetInterface(self, ioObject)
	return self
}

func ioNew(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, mode, opt := mrb.GetArgs3()

	ioObject, err := openIO(mrb, fd, mode, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret, err := mrb.ObjNew(mrb.ClassPtr(self), ioObject)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return ret
}

func ioOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, mode, opt := mrb.GetArgs3()
	block := mrb.GetArgsBlock()

	ioObject, err := openIO(mrb, fd, mode, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret, err := mrb.ObjNew(mrb.ClassPtr(self), ioObject)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if block.IsNil() {
		return ret
	}
	defer func(){
		//if closer, ok := mrb.Data(ret).(io.Closer); ok {
		//	_=closer.Close()
		//}
	}()

	return mrb.YieldCont(block, self, ret)
}

// ioPipe internally opens io.Pipe, which return io.PipeReader and io.PipeWriter
func ioPipe(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()

	r, w := io.Pipe()

	if !block.IsNil() {
		return mrb.YieldCont(block, self, r, w)
	}

	return mrb.AryNewFromValues(mrb.Value(r), mrb.Value(w))
}


func ioSReadlines(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	reader, closer, err := openLineReader(mrb, a.Item(0), a, 1)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret := mrb.AryNewCapa(15)
	for reader.Scan() {
		ret.PushString(reader.Text())
	}
	if closer != nil {
		_= closer.Close()
	}

	return ret
}

func ioForeach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a, block := mrb.GetArgsWithBlock()
	reader, closer, err := openLineReader(mrb, a.Item(0), a, 1)
	if err != nil {
		return mrb.RaiseError(err)
	}

	lines := mrb.AryNewCapa(15)
	for reader.Scan() {
		if block.IsNil() {
			lines.PushString(reader.Text())
			continue
		}
		_, err := mrb.YieldArgv(block, reader.Text())
		if err != nil {
				return mrb.RaiseError(err)
			}
	}
	if closer != nil {
		_= closer.Close()
	}

	if !block.IsNil() {
		return mrb.NilValue()
	}

	ret, err := mrb.FuncallWithBlock(lines, mrb.Intern("to_enum"))
	if err != nil {
		return mrb.RaiseError(err)
	}

	return ret
}

func ioTryConvert(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.GetArgsFirst()

	var ioObject interface{}

	switch f.Type() {
	case oruby.MrbTTFixnum:
		ioObject = os.NewFile(uintptr(f.Int()), "fd")
		return mrb.Value(ioObject)
	case oruby.MrbTTCptr:
		ioObject = os.NewFile(f.Uintptr(), "fd")
		return mrb.Value(ioObject)
	case oruby.MrbTTData:
		ioObject = mrb.Data(f)

		switch ioObject.(type) {
		case *os.File:
			return mrb.Value(ioObject)
		case io.Reader, io.Writer, *superPipe, *io.PipeReader, *io.PipeWriter:
			return mrb.Value(ioObject)
		default:
			ret, err := mrb.FuncallWithBlock(f, mrb.Intern("to_io"))
			if err == nil {
				return ret
			}
		}
	case oruby.MrbTTObject:
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("to_io"))
		if err == nil {
			return ret
		}
	}

	return mrb.NilValue()
}

func ioPopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	proc := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Process"))
	if proc.Type() != oruby.MrbTTModule {
		panic("gem 'process' must be required for IO::popen to work")
	}

	args, block := mrb.GetArgsWithBlock()
	env  := args.Item(0)
	command := args.Item(1)
	modeV := args.Item(2)
	opt  := args.GetLastHash()

	if !env.IsHash() {
		modeV = command
		command = env
		env = mrb.NilValue()
	}

	if command.IsString() && command.String() == "-" {
		return mrb.EArgumentError().Raise("fork param '-' is not supported")
	}

	if command.IsArray() {
		arg := mrb.AryEntry(command, 0)
		if arg.IsHash() {
			if env.IsNil() {
				env = arg
			}
			mrb.AryShift(command)
		}

		arg = mrb.AryEntry(command, -1)
		if arg.IsHash() {
			mrb.HashMerge(opt, arg)
			mrb.AryPop(command)
		}
	}

	mode, err := parseFlags(mrb, modeV, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	// Process.spawn(env, command, opt)
	var cmdV oruby.Value
	if env.IsNil() {
		cmdV, err = mrb.FuncallWithBlock(proc, mrb.Intern("_get_cmd"), command, opt)
	} else {
		cmdV, err = mrb.FuncallWithBlock(proc, mrb.Intern("_get_cmd"), env, command, opt)
	}
	if err != nil {
		return mrb.RaiseError(err)
	}

	cmd, ok := mrb.Data(cmdV).(*exec.Cmd)
	if !ok {
		return mrb.ERuntimeError().Raise("Process::_get_cmd does not return command")
	}

	cmd.Stdin = nil
	cmd.Stdout = nil

	r, err := cmd.StdoutPipe()
	if err != nil {
		return mrb.RaiseError(err)
	}

	w, err := cmd.StdinPipe()
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err = cmd.Start(); err != nil {
		return mrb.RaiseError(err)
	}

	ret := mrb.Value(&superPipe{r, w, cmd.Process.Pid, mode})
	if block.IsNil() {
		return ret
	}

	result, err := mrb.Yield(block, ret)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err = cmd.Wait(); err != nil {
		return mrb.RaiseError(err)
	}
	return result
}
