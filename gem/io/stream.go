package io

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/oruby/oruby"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

type stream struct {
	io.Reader
	io.Writer
	io.Seeker
	f interface{}
	Lineno int64
}

func newStream(ioobject interface{}) *stream {
	reader, ok := ioobject.(io.Reader)
	if !ok {
		reader = nil
	}
	writer, ok := ioobject.(io.Writer)
	if !ok {
		writer = nil
	}
	seeker, ok := ioobject.(io.Seeker)
	if !ok {
		seeker = nil
	}
	return &stream{
		f: ioobject,
		Reader: reader,
		Writer: writer,
		Seeker: seeker,
	}
}

func (s *stream) Close() error {
	if closer, ok := s.f.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func ioInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if f, ok := mrb.Data(self).(*os.File); ok {
		f2, err := platformDup(f)
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.Value(f2)
	}

	if f, err := getFile(mrb, self); err != nil {
		f2, err := platformDup(f)
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.Value(newStream(f2))
	}

	return mrb.EArgumentError().Raise("IO stream is not file and does not support cloning")
}

func valueToS(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	switch v.Type() {
	case oruby.MrbTTString, oruby.MrbTTFixnum, oruby.MrbTTFalse, oruby.MrbTTTrue:
		return v.String(), nil
	}

	sv, err := mrb.FuncallWithBlock(v, mrb.Intern("to_s"))
	if err != nil {
		return "", err
	}
	return sv.String(), nil
}

func ioWriteString(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.GetArgsFirst()
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return raiseIOError(mrb,"IO Stream does not suport writing")
	}

	s, err := valueToS(mrb, str)
	if err !=nil {
		return mrb.RaiseError(err)
	}

	_, err = io.WriteString(w, s)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return mrb.StrNew(fmt.Sprintf("%v%v", s, ioInspect(mrb, self).Value()))
}

func  ioSetAutosclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb,self)
}

func  ioIsAutoclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

func ioBinmode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return self
}

func ioIsBinbode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

func ioClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f  := mrb.Data(self)
	if closer, ok := f.(io.Closer); ok {
		_=closer.Close()
	}
	return mrb.NilValue()
}

func  ioIsCloseOnExec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

var errClosed = errors.New("IOError: not opened for writing")

func ioCloseRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sp, ok := mrb.Data(self).(*superPipe)
	if !ok {
		return raiseIOError(mrb, "IO stream is not duplexed")
	}
	_=sp.PipeReader.CloseWithError(errClosed)
	return mrb.NilValue()
}

func ioCloseWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sp, ok := mrb.Data(self).(*superPipe)
	if !ok {
		return raiseIOError(mrb, "IO stream is not duplexed")
	}
	_=sp.PipeWriter.CloseWithError(errClosed)
	return mrb.NilValue()
}

func ioIsClosed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self)
	zeroBuf := []byte{}

	if sp, ok := f.(*superPipe); ok {
		_,err1 := sp.Read(zeroBuf)
		_,err2 := sp.Write(zeroBuf)
		return oruby.Bool(errors.Is(err1, errClosed) && errors.Is(err2, errClosed))
	}

	r, ok := f.(io.Reader)
	if !ok {
		return mrb.FalseValue()
	}
	w, ok := f.(io.Writer)
	if !ok {
		return mrb.FalseValue()
	}
	_,err1 := r.Read(zeroBuf)
	_,err2 := w.Write(zeroBuf)

	return oruby.Bool(err1 == err2) //&& (err1 == poll.ErrFileClosing || err1 == poll.ErrNetClosing))
}

func ioEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a, block := mrb.GetArgsWithBlock()
	r, sep, err := openLineReader(mrb, self, a.Item(0), a.Item(1), a.GetLastHash())
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

func ioEachByte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	r, ok := mrb.Data(self).(io.Reader)
	if !ok {
		return raiseIOError(mrb,"stream not open for reading")
	}

	if !block.IsNil() {
		b := make([]byte, 1)
		for {
			_, err := io.ReadAtLeast(r, b, 1)
			if err == io.EOF {
				break
			}
			if err != nil {
				return mrb.RaiseError(err)
			}
			_, err = mrb.Yield(block, oruby.Int(int(b[0])))
			if err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}

	ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("to_enum"), mrb.Intern("getbyte"))
	return ret
}

func ioEachChar(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	r, ok := mrb.Data(self).(io.Reader)
	if !ok {
		return raiseIOError(mrb,"stream not open for reading")
	}

	rb := bufio.NewReader(r)

	if !block.IsNil() {
		for {
			c, _, err := rb.ReadRune()
			if err == io.EOF {
				break
			}
			if err != nil {
				return mrb.RaiseError(err)
			}
			_, err = mrb.Yield(block, mrb.StrNew(string(c)))
			if err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}

	ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("to_enum"), mrb.Intern("getc"))
	return ret
}

func ioEachCodepoint(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	r, ok := mrb.Data(self).(io.Reader)
	if !ok {
		return raiseIOError(mrb,"stream not open for reading")
	}

	rb := bufio.NewReader(r)

	if !block.IsNil() {
		for {
			c, _, err := rb.ReadRune()
			if err == io.EOF {
				break
			}
			if err != nil {
				return mrb.RaiseError(err)
			}
			_, err = mrb.Yield(block, oruby.Int(int(c)))
			if err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}

	ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("to_enum"), mrb.Intern("getc"))
	return ret
}

func ioIsEof(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok {
		return raiseIOError(mrb,"stream not open for reading")
	}

	_,err := r.Read([]byte{})
	return oruby.Bool(err == io.EOF)
}

func ioFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int(int(f.Fd()))
}

func ioFlush(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if r, ok := mrb.Data(self).(bufio.Writer); ok {
		if err := r.Flush(); err != nil {
			return mrb.RaiseError(err)
		}
	}
	return self
}

func getFile(mrb *oruby.MrbState, self oruby.Value) (*os.File,error) {
	if f, ok := mrb.Data(self).(*os.File); ok {
		return f, nil
	}
	if stm, ok := mrb.Data(self).(*stream); ok {
		if f, ok := stm.f.(*os.File); ok {
			return f, nil
		}
	}
	return nil, oruby.EError("IOError", "IO stream is not a file")
}

func ioFsync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	_=f.Sync()
	return oruby.Int(0)
}

func ioGetbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return raiseIOError(mrb,"IO Stream does not suport reading")
	}

	rb := bufio.NewReader(r)
	b, err := rb.Peek(1)
	if errors.Is(err, io.EOF) {
		return mrb.NilValue()
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(int(b[0]))
}

func ioGetc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return raiseIOError(mrb,"IO Stream does not suport reading")
	}
	rb := bufio.NewReader(r)
	b, _, err := rb.ReadRune()
	if errors.Is(err, io.EOF) {
		return mrb.NilValue()
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(string(b))
}

func setLastLine(mrb *oruby.MrbState, r io.Reader, line string)  oruby.Value {
	v := mrb.StrNew(line)
	mrb.GVSet(mrb.Intern("$_"), v)
	if stm, ok := r.(*stream); ok {
		stm.Lineno++
	}
	return v
}

func ioGets(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	r, sep, err := openLineReader(mrb, self, a.Item(0), a.Item(1), a.GetLastHash())
	if err != nil {
		return mrb.RaiseError(err)
	}

	line, err := getLine(r, sep)
	if errors.Is(err, io.EOF) {
		return mrb.NilValue()
	}
	if err != nil {
		mrb.SetGV("$_", mrb.NilValue())
		return raiseIOError(mrb, err.Error())
	}

	return setLastLine(mrb, r, line)
}

func ioInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self)
	if stm, ok := f.(*stream); ok {
		f = stm.f
	}

	if f == os.Stdout {
		return mrb.StrNew("<IO:STDOUT>")
	} else if f == os.Stdin {
		return mrb.StrNew(fmt.Sprintf("<IO:STDIN>"))
	} else if f == os.Stderr {
		return mrb.StrNew(fmt.Sprintf("<IO:STERR>"))
	} else if f, ok := f.(*os.File); ok {
		return mrb.StrNew(fmt.Sprintf("<File:%v>", f.Name()))
	}

	return mrb.StrNew(fmt.Sprintf("<%v:%v>", mrb.ClassOf(self), f))
}

func ioLineno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stm := mrb.Data(self)
	if stm, ok := stm.(*stream); ok {
		return oruby.Int64(stm.Lineno)
	}
	return oruby.Int(0)
}

func ioSetLineno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	no := mrb.GetArgsFirst().Int64()
	stm := mrb.Data(self)
	if stm, ok := stm.(*stream); ok {
		stm.Lineno = no
	}
	return oruby.Int64(no)
}

func ioPid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if sp, ok := mrb.Data(self).(*superPipe); ok {
		return oruby.Int(sp.pid)
	}
	return mrb.NilValue()
}

func ioPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, ok := mrb.Data(self).(io.Seeker)
	if !ok {
		return raiseIOError(mrb,"stream cannot tell position")
	}

	pos, err := s.Seek(0, io.SeekCurrent)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int64(pos)
}

func ioSetPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pos := mrb.GetArgsFirst().Int64()
	s, ok := mrb.Data(self).(io.Seeker)
	if !ok {
		return raiseIOError(mrb,"stream cannot tell position")
	}

	pos, err := s.Seek(pos, io.SeekStart)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int64(pos)
}

func ioPread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var err error
	f := mrb.Data(self)
	maxlen, off, outbuff := mrb.GetArgs3()
	buf := make([]byte, maxlen.Int64())
	offset := off.Int64()

	defer func(){
		if err == nil && outbuff.IsString() {
			mrb.StrResize(outbuff, 0)
			mrb.StrCat(outbuff, string(buf))
		}
	}()

	if r, ok := f.(io.ReaderAt); ok {
		_, err = r.ReadAt(buf, offset)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.BytesValue(buf)
	}

	stm, ok := mrb.Data(self).(*stream)
	if ok {
		if r, ok := stm.f.(io.ReaderAt); ok {
			_, err = r.ReadAt(buf, offset)
			if err != nil {
				return mrb.RaiseError(err)
			}
			return mrb.BytesValue(buf)
		}
		f = stm.f
		if stm.Seeker != nil {
			if _,err = stm.Seek(offset, io.SeekStart); err != nil {
				return mrb.RaiseError(err)
			}
			if stm.Reader != nil {
				_, err = stm.Reader.Read(buf)
				if err != nil {
					return mrb.RaiseError(err)
				}
				return mrb.BytesValue(buf)
			}
		}
	}

	if seeker, ok := f.(io.Seeker); ok {
		if _,err = seeker.Seek(offset, io.SeekStart); err != nil {
			return mrb.RaiseError(err)
		}

		if r, ok := f.(io.Reader); ok {
			_, err = r.Read(buf)
			if err != nil {
				return mrb.RaiseError(err)
			}
			return mrb.BytesValue(buf)
		}
	}

	return raiseIOError(mrb, "IO Stream does not suport reading")
}

func ioPrint(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	argc := args.Len()

	sepV := mrb.GetGV("$,") // if field separator is not nil, it is inserted between objects.
	sep := ""
	if !sepV.IsNil() {
		sep = sepV.String()
	}

	outsep := mrb.GetGV("$\\") // If the output record separator os not nil, it is appended to the output.
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return raiseIOError(mrb,"IO Stream does not suport writing")
	}

	for i := 0; i < argc; i++ {
		s, err := valueToS(mrb, args.Item(i))
		if err != nil {
			return mrb.RaiseError(err)
		}
		_, err = io.WriteString(w, s)
		if err != nil {
			return mrb.RaiseError(err)
		}

		if (i != argc-1) && (sep != "") {
			_, err := io.WriteString(w, sep)
			if err != nil {
				return mrb.RaiseError(err)
			}
		}
	}

	if !outsep.IsNil() {
		_, err := io.WriteString(w, outsep.String())
		if err != nil {
			return mrb.RaiseError(err)
		}
	}

	return mrb.NilValue()
}

func ioPrintf(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return raiseIOError(mrb,"IO Stream does not suport writing")
	}

	s, err := mrb.FuncallWithBlock(mrb.KernelModule(), mrb.Intern("sprintf"), args.SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if !s.IsNil() {
		_, err := io.WriteString(w, s.String())
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
	return mrb.NilValue()
}

func ioPutc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c := mrb.GetArgsFirst()
	ch := byte(0)
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return raiseIOError(mrb,"IO Stream does not suport writing")
	}
	switch c.Type() {
	case oruby.MrbTTFixnum:
		ch = byte(c.Int())
	case oruby.MrbTTString:
		if c.Len() == 0 {
			return mrb.ETypeError().Raise("argument should be int or string")
		}
		ch = c.String()[0]
	default:
		return mrb.ETypeError().Raise("argument should be int or string")
	}

	_, err := w.Write([]byte{ch})
	if err != nil {
		return mrb.RaiseError(err)
	}

	return mrb.NilValue()
}

func pArray(mrb *oruby.MrbState, ary oruby.Value, w io.Writer) error {
	if ary.Len() == 0 {
		_,err := io.WriteString(w, "\n")
		return err
	}

	for i:= 0; i < ary.Len(); i++ {
		v := mrb.AryEntry(ary, i)
		if v.IsArray() {
			err := pArray(mrb, v, w)
			if err != nil {
				return err
			}
			continue
		}

		s, err := valueToS(mrb, v)
		if err != nil {
			return err
		}

		_, err = io.WriteString(w, s+"\n")
		if err != nil {
			return err
		}
	}
	return nil
}

func ioPuts(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	argc := args.Len()
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return raiseIOError(mrb,"IO Stream does not suport writing")
	}
	if argc == 0 {
		_,_ = io.WriteString(w, "\n")
		return mrb.NilValue()
	}

	for i:= 0; i < argc; i++ {
		v := args.Item(i)
		if v.IsArray() {
			err := pArray(mrb, v, w)
			if err != nil {
				return mrb.RaiseError(err)
			}
		}

		s, err := valueToS(mrb, v)
		if err != nil {
			return mrb.RaiseError(err)
		}

		_, err = io.WriteString(w, s+"\n")
		if err != nil {
			return mrb.RaiseError(err)
		}
	}

	return mrb.NilValue()
}

func ioPwrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var n int
	f := mrb.Data(self)
	str, offset := mrb.GetArgs2()
	s, err := valueToS(mrb, str)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if w, ok := f.(io.WriterAt); ok {
		n, err = w.WriteAt([]byte(s), offset.Int64())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return oruby.Int(n)
	}

	stm, ok := mrb.Data(self).(*stream)
	if ok {
		if w, ok := stm.f.(io.WriterAt); ok {
			n, err = w.WriteAt([]byte(s), offset.Int64())
			if err != nil {
				return mrb.RaiseError(err)
			}
			return oruby.Int(n)
		}
		f = stm.f
		if stm.Seeker != nil {
			if _,err := stm.Seek(offset.Int64(), io.SeekStart); err != nil {
				return mrb.RaiseError(err)
			}
			if stm.Writer != nil {
				n, err = io.WriteString(stm.Writer, s)
				if err != nil {
					return mrb.RaiseError(err)
				}
				return oruby.Int(n)
			}
		}
	}

	if seeker, ok := f.(io.Seeker); ok {
		if _,err := seeker.Seek(offset.Int64(), io.SeekStart); err != nil {
			return mrb.RaiseError(err)
		}

		if w2, ok := f.(io.Writer); ok {
			n, err = io.WriteString(w2, s)
			if err != nil {
				return mrb.RaiseError(err)
			}
			return oruby.Int(n)
		}
	}

	return raiseIOError(mrb, "IO Stream does not suport writing")
}

func ioRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var length int64
	var buf []byte
	var err error
	l, outbuf := mrb.GetArgs2()

	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return raiseIOError(mrb,"IO Stream does not suport reading")
	}

	// length: 0, nil, omitted or positive integer
	if l.IsNil() {
		//
	} else if l.IsFixnum() {
		length = l.Int64()
		if length == 0 {
			return mrb.StrNew("")
		} else if length < 0 {
			return mrb.EArgumentError().Raise("length must be nil or positive integer")
		}
		r = io.LimitReader(r, length)
	} else {
		return mrb.EArgumentError().Raise("length must be nil or positive integer")
	}

	buf, err = ioutil.ReadAll(r)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, buf)
		return outbuf
	}

	if len(buf) == 0 && !l.IsNil() {
		return mrb.NilValue()
	}

	return mrb.BytesValue(buf)
}

func ioReadbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return raiseIOError(mrb,"IO Stream does not suport reading")
	}
	var b byte
	_, err := r.Read([]byte{b})
	if errors.Is(err, io.EOF) {
		return raiseEOF(mrb)
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(int(b))
}

func ioReadchar(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return raiseIOError(mrb,"IO Stream does not suport reading")
	}
	rs := bufio.NewReader(r)
	chr,_, err := rs.ReadRune()
	if errors.Is(err, io.EOF) {
		return raiseEOF(mrb)
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(string(chr))
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}

func getSpliter(sep *string) bufio.SplitFunc {
	if sep == nil {
		return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			// Return nothing if at end of file and no data passed
			if atEOF && len(data) == 0 {
				return 0, nil, io.EOF
			}

			// If at end of file with data return the data
			if atEOF {
				return len(data), data, nil
			}

			return
		}
	}

	if *sep == "\n" {
		return bufio.ScanLines
	}

	// paragraphs
	if *sep == "" {
		return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			// Return nothing if at end of file and no data passed
			if atEOF && len(data) == 0 {
				return 0, nil, io.EOF
			}

			// Find the index of the input of a separator
			if i := strings.Index(string(data), "\n\n"); i >= 0 {
				return i + 1, dropCR(data[0:i]), nil
			}
			// Find the index of the input of a separator
			if i := strings.Index(string(data), "\n\r\n\r"); i >= 0 {
				return i + 1, dropCR(data[0:i]), nil
			}

			// If at end of file with data return the data
			if atEOF {
				return len(data), dropCR(data), nil
			}

			return
		}
	}

	// Custom separator
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		// Return nothing if at end of file and no data passed
		if atEOF && len(data) == 0 {
			return 0, nil, io.EOF
		}

		// Find the index of the input of a separator
		if i := strings.Index(string(data), *sep); i >= 0 {
			return i + 1, dropCR(data[0:i]), nil
		}

		// If at end of file with data return the data
		if atEOF {
			return len(data), dropCR(data), nil
		}

		return
	}
}

func getLine(r io.Reader, sep *string) (string, error) {
	if sep == nil {
		b, err := ioutil.ReadAll(r)
		return string(b), err
	}

	rs := bufio.NewScanner(r)
	rs.Split(getSpliter(sep))
	rs.Scan()

	return rs.Text(), rs.Err()
}

func ioReadline(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	r, sep, err := openLineReader(mrb, self, a.Item(0), a.Item(1), a.GetLastHash())
	if err != nil {
		return mrb.RaiseError(err)
	}

	line, err := getLine(r, sep)
	if err != nil {
		mrb.SetGV("$_", mrb.NilValue())
		if errors.Is(err, io.EOF){
			raiseEOF(mrb)
		}
		return raiseIOError(mrb, err.Error())
	}

	return setLastLine(mrb, r, line)
}

func ioReadlines(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a  := mrb.GetArgs()
	r, sep, err := openLineReader(mrb, self, a.Item(0), a.Item(1), a.GetLastHash())
	if err != nil {
		return mrb.RaiseError(err)
	}

	if sep == nil {
		data, err := ioutil.ReadAll(r)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.BytesValue(data)
	}

	rs := bufio.NewScanner(r)
	rs.Split(getSpliter(sep))

	lines := mrb.AryNewCapa(15)
	for rs.Scan() {
		lines.PushString(rs.Text())
	}
	if rs.Err() != io.EOF {
		return mrb.RaiseError(rs.Err())
	}
	return lines
}

func ioReadpartial(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	length, outbuf := mrb.GetArgs2()
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return raiseIOError(mrb,"IO Stream does not suport reading")
	}
	if !length.IsFixnum() {
		mrb.EArgumentError().Raise("maxlength must be integer")
	}

	buf := make([]byte, length.Int64())

	r = bufio.NewReader(r)
	n, err := r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return raiseEOF(mrb)
		}
		return mrb.SysFail(err)
	}

	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, buf[:n])
		return outbuf
	}

	return mrb.BytesValue(buf[:n])
}

func ioReopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()

	if f, err := getFile(mrb, arg); err == nil {
		f2, err := platformDup(f)
		if err != nil {
			return mrb.SysFail(err)
		}
		_= f.Close()

		mrb.DataSetInterface(self, f2)
		return self
	}

	return mrb.EArgumentError().Raise("reopen suported only for files")
}

func ioRewind(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var seeker io.Seeker
	stm, ok := mrb.Data(self).(*stream)
	if !ok {
		stm = nil
		if seeker, ok = mrb.Data(self).(io.Seeker); !ok {
			return raiseIOError(mrb,"IO object does not support seek")
		}
	} else {
		stm.Lineno = 0
		seeker = stm.Seeker
	}

	if seeker != nil {
		_, err := seeker.Seek(0, io.SeekStart)
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
	return oruby.Int(0)
}

func ioSeek(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	offset, whence := mrb.GetArgs2(0, io.SeekStart)
	stm, ok := mrb.Data(self).(*stream)
	if !ok {
		stm = nil
	}

	seeker, ok := mrb.Data(self).(io.Seeker)
	if !ok || seeker == nil {
		return raiseIOError(mrb,"IO object does not support seek")
	}

	pos, err := seeker.Seek(offset.Int64(), whence.Int())
	if err != nil {
		return mrb.RaiseError(err)
	}

	if stm != nil {
		if pos == 0 {
			stm.Lineno = 0
		}
	}

	return oruby.Int(0)
}

func ioStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stm := mrb.Data(self).(*stream)
	f, ok := stm.f.(*os.File)
	if ok {
		stat, err := f.Stat()
		if err != nil {
			mrb.RaiseError(err)
		}
		return mrb.Value(stat)
	}
	return mrb.EArgumentError().Raise("IO object does not support stat")
}

func ioSync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.TrueValue()
}

func ioSetSync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetArgs().Item(0)
}

func ioTell(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, ok := mrb.Data(self).(io.Seeker)
	if !ok || s == nil {
		return raiseIOError(mrb,"IO object does not support position")
	}

	ret, err := s.Seek(0, io.SeekCurrent)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int64(ret)
}

func ioToIo(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return self
}

// TODO: not same as MRI, which modifies internal buffer with byte
func ioUngetbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if r, ok := mrb.Data(self).(*bufio.Reader); ok {
		if err := r.UnreadByte(); err != nil {
			return mrb.RaiseError(err)
		}
	}
	return mrb.NilValue()
}

// TODO: not same as MRI, which modifies internal buffer with chr
func ioUngetc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if r, ok := mrb.Data(self).(*bufio.Reader); ok {
		if err := r.UnreadRune(); err != nil {
			return mrb.RaiseError(err)
		}
	}
	return mrb.NilValue()
}

func ioWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()

	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return 	raiseIOError(mrb,"IO object does not support writing")
	}

	cnt := int64(0)
	for i := 0; i < args.Len(); i++ {
		s, err := valueToS(mrb, args.Item(i))
		if err != nil {
			return mrb.RaiseError(err)
		}
		n, err := io.WriteString(w, s)
		if err != nil {
			return mrb.RaiseError(err)
		}
		cnt += int64(n)
	}

	return oruby.Int64(cnt)
}

