package io

import (
	"bufio"
	"errors"
	"github.com/oruby/oruby"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

func init() {
	oruby.Gem("io", func(mrb *oruby.MrbState) interface{} {
		cIO := mrb.DefineClass("IO", mrb.ObjectClass())
		cIO.AttachType((*io.PipeWriter)(nil))
		cIO.AttachType((*io.PipeReader)(nil))
		cIO.AttachType((*superPipe)(nil))

		cIO.Include(mrb.ModuleGet("Enumerable")) /* 15.2.20.3 */

		// Not implemented: select and popen fork (command '-')
		cIO.DefineClassMethod("binread", ioBinread, mrb.ArgsArg(1,2))
		cIO.DefineClassMethod("binwrite", ioBinwrite, mrb.ArgsArg(2,2))
		cIO.DefineClassMethod("copy_stream", ioCopyStream, mrb.ArgsArg(2,2))
		cIO.DefineAlias("for_fd", "new")
		cIO.DefineClassMethod("foreach", ioForeach, mrb.ArgsArg(2,3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("pipe", ioPipe, mrb.ArgsOpt(3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("popen", ioPopen, mrb.ArgsReq(1)+mrb.ArgsRest())
		cIO.DefineClassMethod("read", ioBinread, mrb.ArgsArg(1, 3))
		cIO.DefineClassMethod("readlines", ioSReadlines, mrb.ArgsArg(2, 3))
 		cIO.DefineClassMethod("select", mrb.NotImplemented, mrb.ArgsArg(1, 3))
		cIO.DefineClassMethod("sysopen", ioSSysopen, mrb.ArgsArg(1, 2))
		cIO.DefineClassMethod("try_convert", ioTryConvert, mrb.ArgsReq(1))
		cIO.DefineClassMethod("write", ioBinwrite, mrb.ArgsArg(2, 2))

		cIO.DefineMethod("open", ioOpen, mrb.ArgsArg(1, 2)|mrb.ArgsBlock()) /* 15.2.20.5.21 (x)*/
		cIO.DefineMethod("initialize", ioInit, mrb.ArgsArg(1, 2)) /* 15.2.20.5.21 (x)*/
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

		cIO.DefineMethod("readNonblock", ioReadNonblock, mrb.ArgsArg(1,2))
		cIO.DefineMethod("writeNonblock", ioWriteNonblock, mrb.ArgsArg(1,1))

		ioError := mrb.DefineClass("IOError", mrb.EStandardErrorClass())
		mrb.DefineClass("EOFError", ioError)

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

func fooClose()error{ return nil }

func getStream(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, func()error, error) {
	switch item.Type() {
	case oruby.MrbTTString:
		ret, err := os.OpenFile(item.String(), mode, 0755)
		return ret, func()error{ return ret.Close() }, err
	case oruby.MrbTTData:
		return mrb.Data(item), fooClose, nil
	}
	return nil, fooClose, oruby.EArgumentError("IO Stream or name expected")
}

func ioCopyStream(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()

	src, srcCloser, err := getStream(mrb, os.O_RDONLY, args.Item(0))
	if err != nil {
		return mrb.RaiseError(err)
	}
	defer srcCloser()

	dest, destCloser, err := getStream(mrb, os.O_WRONLY, args.Item(1))
	if err != nil {
		return mrb.RaiseError(err)
	}

	offset := int64(args.ItemDefInt(3, 0))
	if offset > 0 {
		if seaker, ok := src.(io.Seeker); ok {
			if _, err := seaker.Seek(offset, io.SeekStart); err != nil {
				_= destCloser()
				return mrb.RaiseError(err)
			}
		}
	}

	var ret int64

	length := int64(args.ItemDefInt(2, -1))
	if length < 0 {
		ret, err = io.Copy(src.(io.Writer), dest.(io.Reader))
	} else {
		ret, err = io.CopyN(src.(io.Writer), dest.(io.Reader), length)
	}
	if err != nil {
		_= destCloser()
		return mrb.RaiseError(err)
	}

	// Destination could error on close, discarding writes
	if err = destCloser(); err != nil {
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
	length :=  lengthV.Int64()

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

	f, err := os.OpenFile(name.String(), os.O_CREATE|os.O_RDWR,0755)
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
	flags := 0

	if mode == "" {
		return 0, nil
	}

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

	for _, m := range mode {
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

	f, err := os.OpenFile(mrb.String(path), flags, os.FileMode(perm))
	if err != nil {
		return mrb.SysFail(err.Error())
	}

	return mrb.Value(f.Fd())
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

	mrb.DataSetInterface(self, newStream(ioObject))
	return self
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

	if !block.IsNil() {
		return ret
	}

	return mrb.YieldCont(block, self, ret)
}

func ioPipe(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()

	reader, writer := io.Pipe()

	if !block.IsNil() {
		return mrb.YieldCont(block, self, reader, writer)
	}

	return mrb.AryNewFromValues(mrb.Value(reader), mrb.Value(writer))
}

func openLineReader(mrb *oruby.MrbState, fd, arg1, arg2, opt oruby.Value) (io.Reader, *string, error) {
	globalSeparator := mrb.GetGV("$/").String()
	sep := &globalSeparator
	limit := int64(-1)

	switch arg1.Type() {
	case oruby.MrbTTString:
		*sep = arg1.String()
	case oruby.MrbTTFalse:
		sep = nil
	case oruby.MrbTTFixnum:
		limit = arg1.Int64()
	}

	if arg2.IsFixnum() {
		limit = arg2.Int64()
	}

	f, err := openIO(mrb, fd, mrb.NilValue(), opt)
	if err != nil {
		return nil, nil, err
	}

	reader, ok := f.(io.Reader)
	if !ok {
		return nil, nil, oruby.EError("IOError", "file does not support reading")
	}

	if limit >= 0 {
		reader = io.LimitReader(reader, limit)
	}
	return reader, sep, nil
}

func ioSReadlines(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	f := a.Item(0)
	r, sep, err := openLineReader(mrb, f, a.Item(1), a.Item(2), a.GetLastHash() )
	if err != nil {
		return mrb.RaiseError(err)
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return mrb.RaiseError(err)
	}

	defer func(){
		if closer, ok := r.(io.Closer); ok {
			_= closer.Close()
		}
	}()

	if sep == nil {
		return mrb.BytesValue(data)
	}

	lines := strings.Split(string(data), *sep)
	return mrb.Value(lines)
}

func ioForeach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a, block := mrb.GetArgsWithBlock()
	r, sep, err := openLineReader(mrb, a.Item(0), a.Item(1), a.Item(2), a.GetLastHash())
	if err != nil {
		return mrb.RaiseError(err)
	}

	rs := bufio.NewScanner(r)
	rs.Split(getSpliter(sep))

	lines := mrb.AryNewCapa(15)
	for rs.Scan() {
		if !block.IsNil() {
			_, err := mrb.YieldArgv(block, rs.Text())
			if err != nil {
				return mrb.RaiseError(err)
			}
			continue
		}
		lines.PushString(rs.Text())
	}
	if closer, ok := r.(io.Closer); ok {
		_ = closer.Close()
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
		case io.Reader, io.Writer, *os.File, *io.PipeReader, *io.PipeWriter:
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
	cmd  := args.Item(1)
	modeV := args.Item(2)
	opt  := args.GetLastHash()

	if !env.IsHash() {
		modeV = cmd
		cmd = env
		env = mrb.NilValue()
	}

	if cmd.IsString() && cmd.String() == "-" {
		return mrb.NotImplemented(mrb,self)

	} else if cmd.IsArray() {
		arg := mrb.AryEntry(cmd, 0)
		if arg.IsHash() {
			if env.IsNil() {
				env = arg
			}
			mrb.AryShift(cmd)
		}

		arg = mrb.AryEntry(cmd, -1)
		if arg.IsHash() {
			mrb.HashMerge(opt, arg)
			mrb.AryPop(cmd)
		}
	}

	// Pipe connects subprocess with mrb state
	r,w := io.Pipe()

	if !opt.IsHash() {
		opt = mrb.HashNew().Value()
	}

	mode, err := parseFlags(mrb, modeV, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	mrb.HashDeleteKey(opt, mrb.Intern("in"))
	mrb.HashDeleteKey(opt, mrb.Intern("out"))
	mrb.HashSet(opt, mrb.FixnumValue(0), mrb.Value(w))
	mrb.HashSet(opt, mrb.FixnumValue(1), mrb.Value(r))

	// Process.spawn(env, cmd, opt)
	pid, err := mrb.FuncallWithBlock(proc, mrb.Intern("spawn"), env, cmd, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret := mrb.Value(&superPipe{r,w, pid.Int(), mode})
	if block.IsNil() {
		return ret
	}

	result, err := mrb.Yield(block, ret)
	if err != nil {
		return mrb.RaiseError(err)
	}
	_,_= mrb.FuncallWithBlock(proc, mrb.Intern("wait"), pid)
	return result
}
