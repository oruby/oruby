package io

import "C"
import (
	"bufio"
	_ "embed"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

type IoData struct {
	GetStream func(mrb *oruby.MrbState, mode int, item oruby.Value) (interface{}, error)
	OpenIO    func(mrb *oruby.MrbState, fd, mode, opt oruby.Value) (interface{}, error)
}

//go:embed io_nonblock.rb
var ioNonblockRb []byte

//go:embed io_methods.rb
var ioMethodsRb []byte

// platformInit adds IO.select and non-blocking IO where supported
var platformInit func(mrb *oruby.MrbState, cIO oruby.RClass)

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
		cIO.DefineClassMethod("__oruby_copy_stream", ioCopyStream, mrb.ArgsArg(2, 2))
		cIO.DefineClassMethod("foreach", ioForeach, mrb.ArgsArg(2, 3)|mrb.ArgsBlock())
		cIO.DefineClassMethod("_pipe", ioPipe, mrb.ArgsOpt(3))
		mrb.Eval(`
class IO
  def self.pipe(*args, &block)
    r, w = _pipe(*args)
    if block
      begin
        yield r, w
      ensure
        r.close rescue nil
        w.close rescue nil
      end
    else
      [r, w]
    end
  end
end
`)
		cIO.DefineClassMethod("readlines", ioSReadlines, mrb.ArgsArg(1, 3)|mrb.ArgsKey(0, 1))
		cIO.DefineClassMethod("open", ioOpen, mrb.ArgsArg(1, 2)|mrb.ArgsBlock())
		cIO.DefineClassMethod("for_fd", ioNew, mrb.ArgsArg(1, 2))

		cIO.DefineMethod("initialize", ioInit, mrb.ArgsArg(1, 2))
		cIO.DefineMethod("initialize_copy", ioInitCopy, mrb.ArgsReq(1))
		cIO.DefineMethod("autoclose=", ioSetAutosclose, mrb.ArgsReq(1))
		cIO.DefineMethod("autoclose?", ioIsAutoclose, mrb.ArgsNone())
		cIO.DefineMethod("binmode", ioBinmode, mrb.ArgsNone())
		cIO.DefineMethod("binmode?", ioIsBinbode, mrb.ArgsNone())
		//cIO.DefineMethod("bytes", ioBytes, mrb.ArgsArg()) // Deprecated
		//cIO.DefineMethod("chars", ioChars, mrb.ArgsArg()) // Deprecated
		cIO.DefineMethod("close", ioClose, mrb.ArgsNone())
		cIO.DefineMethod("close_on_exec=", ioSetCloseOnExec, mrb.ArgsReq(1))
		cIO.DefineMethod("close_on_exec?", ioIsCloseOnExec, mrb.ArgsNone())
		cIO.DefineMethod("closed?", ioIsClosed, mrb.ArgsNone())
		cIO.DefineMethod("each_byte", ioEachByte, mrb.ArgsBlock())
		cIO.DefineMethod("each_char", ioEachChar, mrb.ArgsBlock())
		cIO.DefineMethod("each_codepoint", ioEachCodepoint, mrb.ArgsBlock())
		cIO.DefineAlias("codepoints", "each_codepoint")
		cIO.DefineMethod("eof?", ioIsEof, mrb.ArgsNone())
		cIO.DefineAlias("eof", "eof?")
		cIO.DefineMethod("external_encoding", ioExternalEncoding, mrb.ArgsNone())
		cIO.DefineMethod("internal_encoding", ioInternalEncoding, mrb.ArgsNone())
		cIO.DefineMethod("__oruby_fcntl", ioFcntl, mrb.ArgsReq(2))
		cIO.DefineMethod("__oruby_buffered?", ioBuffered, mrb.ArgsNone())
		cIO.DefineClassMethod("__oruby_fd_flags", ioFdFlags, mrb.ArgsReq(1))
		cIO.DefineClassMethod("__oruby_pipe_fds", ioPipeFds, mrb.ArgsNone())
		cIO.DefineMethod("fdatasync", ioFsync, mrb.ArgsNone())
		cIO.DefineMethod("fileno", ioFileno, mrb.ArgsNone())
		cIO.DefineMethod("flush", ioFlush, mrb.ArgsNone())
		cIO.DefineMethod("fsync", ioFsync, mrb.ArgsNone())
		cIO.DefineMethod("getbyte", ioGetbyte, mrb.ArgsNone())
		cIO.DefineMethod("getc", ioGetc, mrb.ArgsNone())
		cIO.DefineMethod("gets", ioGets, mrb.ArgsOpt(3))
		cIO.DefineMethod("inspect", ioInspect, mrb.ArgsNone())
		//cIO.DefineMethod("internal_encoding", ioInternalEncoding, mrb.ArgsNone())
		cIO.DefineMethod("isatty", ioIsTty, mrb.ArgsNone())
		cIO.DefineMethod("lineno", ioLineno, mrb.ArgsNone())
		cIO.DefineMethod("lineno=", ioSetLineno, mrb.ArgsReq(1))
		cIO.DefineMethod("pid", ioPid, mrb.ArgsNone())
		cIO.DefineMethod("pos", ioPos, mrb.ArgsNone())
		cIO.DefineMethod("pos=", ioSetPos, mrb.ArgsReq(1))
		cIO.DefineMethod("pread", ioPread, mrb.ArgsArg(2, 1))
		cIO.DefineMethod("pwrite", ioPwrite, mrb.ArgsReq(2))
		cIO.DefineMethod("read", ioRead, mrb.ArgsOpt(2))
		cIO.DefineMethod("readbyte", ioReadbyte, mrb.ArgsNone())
		cIO.DefineMethod("readchar", ioReadchar, mrb.ArgsNone())
		cIO.DefineMethod("readline", ioReadline, mrb.ArgsOpt(3))
		cIO.DefineMethod("readlines", ioReadlines, mrb.ArgsOpt(2))
		cIO.DefineMethod("readpartial", ioReadpartial, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("rewind", ioRewind, mrb.ArgsNone())
		cIO.DefineMethod("seek", ioSeek, mrb.ArgsArg(1, 1))
		//cIO.DefineMethod("set_encoding", ioSet_encoding, mrb.ArgsArg(1,2))
		//cIO.DefineMethod("set_encoding_by_bom", ioSetEncodingByBom, mrb.ArgsNone())
		cIO.DefineMethod("stat", ioStat, mrb.ArgsNone())
		cIO.DefineMethod("sysread", ioSysread, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("sysseek", ioSysseek, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("syswrite", ioSyswrite, mrb.ArgsReq(1))
		cIO.DefineMethod("tell", ioPos, mrb.ArgsNone())
		cIO.DefineMethod("to_i", ioFileno, mrb.ArgsNone())
		cIO.DefineMethod("to_io", ioToIo, mrb.ArgsNone())
		cIO.DefineMethod("tty?", ioIsTty, mrb.ArgsNone())
		cIO.DefineMethod("ungetbyte", ioUngetbyte, mrb.ArgsReq(1))
		cIO.DefineMethod("ungetc", ioUngetc, mrb.ArgsReq(1))
		cIO.DefineMethod("write", ioWrite, mrb.ArgsAny())

		ioError := mrb.DefineClass("IOError", mrb.EStandardErrorClass())
		mrb.DefineClass("EOFError", ioError)
		if platformInit != nil {
			platformInit(mrb, cIO)
			rb.LoadRuby(mrb, "io_nonblock.rb", ioNonblockRb)
		}
		rb.LoadRuby(mrb, "<internal:io>", ioMethodsRb)
		// StringIO is provided by stdlib gem (require "stringio")

		return &IoData{getStream, openIO}
	})
}

func RaiseEOF(mrb *oruby.MrbState) oruby.Value {
	return mrb.Raise(mrb.ExcGet("EOFError"), "end of file reached")
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

func parseCopyStreamInt(mrb *oruby.MrbState, v oruby.Value) (int64, error) {
	if v.IsInteger() {
		return int64(v.Int()), nil
	}
	if mrb.RespondTo(v, oruby.Sym_to_int) {
		res, err := mrb.Funcall(v, oruby.Sym_to_int)
		if err != nil {
			return 0, err
		}
		if res.IsInteger() {
			return int64(res.Int()), nil
		}
		return 0, oruby.ETypeError("can't convert %v into Integer", mrb.TypeName(res))
	}
	return 0, oruby.ETypeError("can't convert %v into Integer", mrb.TypeName(v))
}

func resolveStreamArg(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if v.IsString() || v.IsData() {
		return v, nil
	}
	if mrb.RespondTo(v, oruby.Sym_to_path) {
		res, err := mrb.Funcall(v, oruby.Sym_to_path)
		if err != nil {
			return v, err
		}
		if !res.IsString() {
			return v, oruby.ETypeError("can't convert %v into String", mrb.TypeName(res))
		}
		return res, nil
	}
	if mrb.RespondTo(v, oruby.Sym_to_io) {
		res, err := mrb.Funcall(v, oruby.Sym_to_io)
		if err != nil {
			return v, err
		}
		return res, nil
	}
	return v, nil
}

func ioCopyStream(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	from := args.Item(0)
	to := args.Item(1)

	fromResolved, err := resolveStreamArg(mrb, from)
	if err != nil {
		return mrb.RaiseError(err)
	}

	streamFn := getStream
	if data := mrb.GemData("io"); data != nil {
		if ioData, ok := data.(*IoData); ok && ioData.GetStream != nil {
			streamFn = ioData.GetStream
		}
	}

	src, err := streamFn(mrb, os.O_RDONLY, fromResolved)
	if err != nil {
		return mrb.RaiseError(err)
	}
	defer closeStream(src, fromResolved.IsString())

	toResolved, err := resolveStreamArg(mrb, to)
	if err != nil {
		return mrb.RaiseError(err)
	}

	dest, err := streamFn(mrb, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, toResolved)
	if err != nil {
		return mrb.RaiseError(err)
	}

	var offset int64
	offsetSpecified := false
	if args.Len() > 3 && !args.Item(3).IsNil() {
		offset, err = parseCopyStreamInt(mrb, args.Item(3))
		if err != nil {
			_ = closeStream(dest, toResolved.IsString())
			return mrb.RaiseError(err)
		}
		offsetSpecified = true
	}

	if offsetSpecified {
		if seeker, ok := src.(io.Seeker); ok {
			// If IO object, and offset is set copy_stream doesn’t move the current file offset
			if !fromResolved.IsString() {
				oldPos, err := seeker.Seek(0, io.SeekCurrent)
				if err != nil {
					_ = closeStream(dest, toResolved.IsString())
					return mrb.RaiseError(err)
				}
				defer func() { _, _ = seeker.Seek(oldPos, io.SeekStart) }()
			}

			if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
				_ = closeStream(dest, toResolved.IsString())
				return mrb.RaiseError(err)
			}
		} else {
			_ = closeStream(dest, toResolved.IsString())
			return mrb.EArgumentError().Raise("source does not support offset seek")
		}
	}

	var length int64 = -1
	if args.Len() > 2 && !args.Item(2).IsNil() {
		length, err = parseCopyStreamInt(mrb, args.Item(2))
		if err != nil {
			_ = closeStream(dest, toResolved.IsString())
			return mrb.RaiseError(err)
		}
	}

	writer, ok := dest.(io.Writer)
	if !ok || writer == nil {
		_ = closeStream(dest, toResolved.IsString())
		return RaiseIOError(mrb, "destination is not writable")
	}

	reader, ok := src.(io.Reader)
	if !ok || reader == nil {
		_ = closeStream(dest, toResolved.IsString())
		return RaiseIOError(mrb, "source is not readable")
	}

	var ret int64
	if length < 0 {
		ret, err = io.Copy(writer, reader)
	} else {
		ret, err = io.Copy(writer, io.LimitReader(reader, length))
	}
	if err != nil {
		_ = closeStream(dest, toResolved.IsString())
		if errors.Is(err, syscall.EBADF) || errors.Is(err, os.ErrClosed) {
			return RaiseIOError(mrb, err.Error())
		}
		return mrb.RaiseError(err)
	}

	// Destination could error on close, discarding writes
	if err = closeStream(dest, toResolved.IsString()); err != nil {
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

	obj := mrb.Yield(block, ret)
	if closer, ok := ioObject.(io.Closer); ok {
		_ = closer.Close()
	}

	return obj
}

// ioPipe internally opens os.Pipe, returning os.File for reader and writer
// IO.__oruby_pipe_fds: a pipe's [read fd, write fd]. Their *os.File (Go's
// pollable pipe ends) wait in pendingPipes until IO#initialize(fd) claims
// them, so IO.pipe can make instances the way MRI does: allocate and
// initialize, not new.
func ioPipeFds(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, w, err := os.Pipe()
	if err != nil {
		return mrb.SysFail(err)
	}
	rfd, wfd := RawFd(r), RawFd(w)
	pendingMu.Lock()
	pendingPipes[rfd] = r
	pendingPipes[wfd] = w
	pendingMu.Unlock()
	return mrb.AryNewFromValues(oruby.Int(rfd).Value(), oruby.Int(wfd).Value())
}

var (
	pendingMu    sync.Mutex
	pendingPipes = map[int]*os.File{}
)

// ClaimPipeFD is the *os.File of a descriptor IO.__oruby_pipe_fds made, nil
// for any other
func ClaimPipeFD(fd int) *os.File {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	f := pendingPipes[fd]
	delete(pendingPipes, fd)
	return f
}

func ioPipe(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, w, err := os.Pipe()
	if err != nil {
		return mrb.SysFail(err)
	}

	cIO := mrb.ClassGet("IO")
	rVal := mrb.DataWrapInterface(cIO, r).Value()
	wVal := mrb.DataWrapInterface(cIO, w).Value()

	return mrb.AryNewFromValues(rVal, wVal)
}

func ioSReadlines(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()

	reader, closer, err := openLineReader(mrb, a.Item(0), a, mrb.KeywordArgs(), 1)
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
	a, kwargs, block := mrb.GetAllArgs()
	reader, closer, err := openLineReader(mrb, a.Item(0), a, kwargs, 1)
	if err != nil {
		return mrb.RaiseError(err)
	}

	lines := mrb.AryNewCapa(15)
	for reader.Scan() {
		if block.IsNil() {
			lines.PushString(reader.Text())
			continue
		}
		_ = mrb.YieldArgv(block, reader.Text())
		if mrb.Exc() != nil {
			return mrb.RaiseError(mrb.Err())
		}
	}
	if closer != nil {
		_ = closer.Close()
	}

	if !block.IsNil() {
		return mrb.NilValue()
	}

	ret, err := mrb.FuncallWithBlock(lines, oruby.Sym_to_enum)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return ret
}

// IO#__oruby_fcntl(cmd, arg): fcntl with an integer argument (IO#fcntl,
// io_methods.rb, checks the arguments)
func ioFcntl(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	cmd, arg := mrb.GetArgs2()
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	r, err := platformFcntl(f, cmd.Int(), arg.Int())
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int(r)
}

// IO.__oruby_fd_flags(fd): the status flags of descriptor fd (F_GETFL), or
// nil where they are not known
func ioFdFlags(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	fl, ok := fdFlags(mrb.GetArgsFirst().Int())
	if !ok {
		return mrb.NilValue()
	}
	return oruby.Int(fl)
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
	case oruby.MrbTTString:
		return os.Open(mrb.FSPath(fd))
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
		if path, err := mrb.Funcall(fd, oruby.Sym_to_path); err == nil && path.IsString() {
			return os.Open(mrb.FSPath(path))
		}
		if path, err := mrb.Funcall(fd, oruby.Sym_to_str); err == nil && path.IsString() {
			return os.Open(mrb.FSPath(path))
		}
		ret, err := mrb.FuncallWithBlock(fd, oruby.Sym_to_io)
		if err == nil {
			return mrb.Data(ret), nil
		}
	}

	return nil, oruby.EArgumentError("First argument must be fd or IO object")
}
