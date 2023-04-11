package io

import (
	"bufio"
	"io"
	"os"

	"github.com/oruby/oruby"
)

type IoData struct {
	GetStream func(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, error)
	OpenIO    func(mrb *oruby.MrbState, fd, mode, opt oruby.Value) (interface{}, error)
}

func init() {
	oruby.Gem("io", func(mrb *oruby.MrbState) interface{} {
		cIO := mrb.DefineClass("IO", mrb.ObjectClass())
		cIO.AttachType((*bufio.Writer)(nil))
		cIO.AttachType((*bufio.Reader)(nil))
		cIO.AttachType((*io.PipeWriter)(nil))
		cIO.AttachType((*io.PipeReader)(nil))

		cIO.Include(mrb.ModuleGet("Enumerable"))
		initConsts(mrb, cIO)

		// Not implemented: select and popen with fork command '-')
		cIO.DefineClassMethod("copy_stream", ioCopyStream, mrb.ArgsArg(2, 2))
		cIO.DefineClassMethod("foreach", ioForeach, mrb.ArgsArg(2, 3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("pipe", ioPipe, mrb.ArgsOpt(3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("readlines", ioSReadlines, mrb.ArgsArg(2, 3))
		cIO.DefineClassMethod("select", mrb.NotImplemented, mrb.ArgsArg(1, 3))
		cIO.DefineClassMethod("try_convert", ioTryConvert, mrb.ArgsReq(1))
		cIO.DefineClassMethod("open", ioOpen, mrb.ArgsArg(1, 2)|mrb.ArgsBlock())
		cIO.DefineClassMethod("for_fd", ioNew, mrb.ArgsArg(1, 2))

		cIO.DefineMethod("initialize", ioInit, mrb.ArgsArg(1, 2))
		cIO.DefineMethod("initialize_copy", ioInitCopy, mrb.ArgsReq(1))
		cIO.DefineMethod("<<", ioWriteString, mrb.ArgsReq(1))
		cIO.DefineMethod("advise", mrb.NotImplemented, mrb.ArgsArg(1, 2))
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
		cIO.DefineMethod("pread", ioPread, mrb.ArgsArg(2, 1))
		cIO.DefineMethod("print", ioPrint, mrb.ArgsAny())
		cIO.DefineMethod("printf", ioPrintf, mrb.ArgsReq(1)|mrb.ArgsAny())
		cIO.DefineMethod("putc", ioPutc, mrb.ArgsReq(1))
		cIO.DefineMethod("puts", ioPuts, mrb.ArgsAny())
		cIO.DefineMethod("pwrite", ioPwrite, mrb.ArgsReq(2))
		cIO.DefineMethod("read", ioRead, mrb.ArgsOpt(2))
		cIO.DefineMethod("readbyte", ioReadbyte, mrb.ArgsNone())
		cIO.DefineMethod("readchar", ioReadchar, mrb.ArgsNone())
		cIO.DefineMethod("readline", ioReadline, mrb.ArgsOpt(3))
		cIO.DefineMethod("readlines", ioReadlines, mrb.ArgsArg(1, 2))
		cIO.DefineMethod("readpartial", ioReadpartial, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("rewind", ioRewind, mrb.ArgsNone())
		cIO.DefineMethod("seek", ioSeek, mrb.ArgsArg(1, 1))
		//cIO.DefineMethod("set_encoding", ioSet_encoding, mrb.ArgsArg(1,2))
		//cIO.DefineMethod("set_encoding_by_bom", ioSetEncodingByBom, mrb.ArgsNone())
		cIO.DefineMethod("stat", ioStat, mrb.ArgsNone())
		cIO.DefineMethod("sync", ioSync, mrb.ArgsNone())
		cIO.DefineMethod("sync=", ioSetSync, mrb.ArgsReq(1))
		cIO.DefineMethod("sysread", ioReadpartial, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("sysseek", ioSeek, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("syswrite", ioWrite, mrb.ArgsReq(1))
		cIO.DefineMethod("tell", ioTell, mrb.ArgsNone())
		cIO.DefineMethod("to_i", ioFileno, mrb.ArgsNone())
		cIO.DefineMethod("to_io", ioToIo, mrb.ArgsNone())
		cIO.DefineMethod("tty?", mrb.NotImplemented, mrb.ArgsNone())
		cIO.DefineMethod("ungetbyte", ioUngetbyte, mrb.ArgsReq(1))
		cIO.DefineMethod("ungetc", ioUngetc, mrb.ArgsReq(1))
		cIO.DefineMethod("write", ioWrite, mrb.ArgsAny())

		cIO.DefineMethod("readNonblock", mrb.NotImplemented, mrb.ArgsArg(1, 2))
		cIO.DefineMethod("writeNonblock", mrb.NotImplemented, mrb.ArgsArg(1, 1))

		ioError := mrb.DefineClass("IOError", mrb.EStandardErrorClass())
		mrb.DefineClass("EOFError", ioError)

		initStringIO(mrb, cIO)

		return &IoData{getStream, openIO}
	})
}

func RaiseEOF(mrb *oruby.MrbState) oruby.Value {
	return mrb.Raise(mrb.ExcGet("EOFError"), "")
}

func RaiseIOError(mrb *oruby.MrbState, msg string) oruby.Value {
	return mrb.Raise(mrb.ExcGet("IOError"), msg)
}

func RaiseIOErrorf(mrb *oruby.MrbState, format string, args ...interface{}) oruby.Value {
	return mrb.Raisef(mrb.ExcGet("IOError"), format, args...)
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
	to := args.Item(1)

	src, err := getStream(mrb, os.O_RDONLY, from)
	if err != nil {
		return mrb.RaiseError(err)
	}
	defer closeStream(src, from.IsString())

	dest, err := getStream(mrb, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, to)
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
					_ = closeStream(dest, to.IsString())
					return mrb.RaiseError(err)
				}
				defer func() { _, _ = seeker.Seek(oldPos, io.SeekStart) }()
			}

			if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
				_ = closeStream(dest, to.IsString())
				return mrb.RaiseError(err)
			}
		} else {
			mrb.EArgumentError().Raise("source does not support offset seek")
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
		_ = closeStream(dest, to.IsString())
		return mrb.RaiseError(err)
	}

	// Destination could error on close, discarding writes
	if err = closeStream(dest, to.IsString()); err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int64(ret)
}

func getIOData(mrb *oruby.MrbState, ioClass oruby.RClass) *IoData {
	return mrb.GemData("io").(*IoData)
}

func ioInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, mode, opt := mrb.GetArgs3()
	if fd.Type() == oruby.MrbTTString {
		return mrb.EArgumentError().Raise("IO stream expected")
	}

	ioData := getIOData(mrb, mrb.ClassOf(self))
	ioObject, err := ioData.OpenIO(mrb, fd, mode, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}
	mrb.SetIV(self, "@mode", mode)
	mrb.SetIV(self, "@opt", opt)

	mrb.DataSetInterface(self, ioObject)
	return self
}

func ioNew(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, mode, opt := mrb.GetArgs3()
	if fd.Type() == oruby.MrbTTString {
		return mrb.EArgumentError().Raise("IO stream expected")
	}

	ioData := getIOData(mrb, mrb.ClassPtr(self))
	ioObject, err := ioData.OpenIO(mrb, fd, mode, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret, err := mrb.ObjNew(mrb.ClassPtr(self), ioObject)
	if err != nil {
		return mrb.RaiseError(err)
	}

	mrb.SetIV(self, "@mode", mode)
	mrb.SetIV(self, "@opt", opt)

	return ret
}

func ioOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, mode, opt := mrb.GetArgs3()
	block := mrb.GetArgsBlock()

	ioData := getIOData(mrb, mrb.ClassPtr(self))
	ioObject, err := ioData.OpenIO(mrb, fd, mode, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	ret, err := mrb.ObjNew(mrb.ClassPtr(self), ioObject)
	if err != nil {
		return mrb.RaiseError(err)
	}

	mrb.SetIV(self, "@mode", mode)
	mrb.SetIV(self, "@opt", opt)

	if block.IsNil() {
		return ret
	}

	obj, _ := mrb.Yield(block, ret)
	if closer, ok := ioObject.(io.Closer); ok {
		_ = closer.Close()
	}

	return obj
}

// ioPipe internally opens io.Pipe, which return io.PipeReader and io.PipeWriter
func ioPipe(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()

	r, w := io.Pipe()

	if !block.IsNil() {
		return mrb.YieldCont(block, self, r, w)
	}

	return mrb.AryNewFromValues(mrb.DataValue(r), mrb.DataValue(w))
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
		_ = closer.Close()
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
	if f.Type() == oruby.MrbTTString {
		return mrb.NilValue()
	}

	ioData := getIOData(mrb, mrb.ClassOf(self))
	ioObject, err := ioData.OpenIO(mrb, f, mrb.NilValue(), mrb.NilValue())
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.DataValue(ioObject)
}

func getStream(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, error) {
	switch item.Type() {
	case oruby.MrbTTCData:
		return mrb.Data(item), nil
	}
	return nil, oruby.EArgumentError("IO Stream or name expected")
}

func openIO(mrb *oruby.MrbState, fd, mode, opt oruby.Value) (interface{}, error) {
	var ioObject interface{}

	switch fd.Type() {
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
