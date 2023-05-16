package io

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/oruby/oruby"
)

func ioInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if f, ok := mrb.Data(self).(*os.File); ok {
		f2, err := platformDup(f)
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.Value(f2)
	}

	return mrb.EArgumentError().Raise("IO stream is not a file and does not support cloning")
}

func valueToS(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	switch v.Type() {
	case oruby.MrbTTString, oruby.MrbTTFixnum, oruby.MrbTTFalse, oruby.MrbTTTrue:
		return v, nil
	}

	sv, err := mrb.FuncallWithBlock(v, mrb.Intern("to_s"))
	if err != nil {
		return mrb.NilValue(), err
	}
	return sv, nil
}

func ioWriteString(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.GetArgsFirst()
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return RaiseIOError(mrb, "IO Stream does not suport writing")
	}

	s, err := valueToS(mrb, str)
	if err != nil {
		return mrb.RaiseError(err)
	}

	_, err = w.Write(s.Bytes())
	if err != nil {
		return mrb.RaiseError(err)
	}

	return mrb.StrNew(fmt.Sprintf("%v%v", s, ioInspect(mrb, self).Value()))
}

func ioSetAutosclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}

func ioIsAutoclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

func ioBinmode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return self
}

func ioIsBinbode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

func ioClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self)
	if closer, ok := f.(io.Closer); ok {
		_ = closer.Close()
	}
	return mrb.NilValue()
}

func ioIsCloseOnExec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

func ioCloseRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return RaiseIOError(mrb, "IO stream is not duplexed")
}

func ioCloseWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return RaiseIOError(mrb, "IO stream is not duplexed")
}

func ioIsClosed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self)
	zeroBuf := []byte{}

	//if sp, ok := f.(*superPipe); ok {
	//	_,err1 := sp.Read(zeroBuf)
	//	_,err2 := sp.Write(zeroBuf)
	//	return oruby.Bool(errors.Is(err1, os.ErrClosed) && errors.Is(err2, os.ErrClosed))
	//}

	r, ok := f.(io.Reader)
	if !ok {
		return mrb.FalseValue()
	}
	w, ok := f.(io.Writer)
	if !ok {
		return mrb.FalseValue()
	}
	_, err1 := r.Read(zeroBuf)
	_, err2 := w.Write(zeroBuf)

	return oruby.Bool(err1 == err2) //&& (err1 == poll.ErrFileClosing || err1 == poll.ErrNetClosing))
}

func ioEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a, kwargs, block := mrb.GetAllArgs()
	reader, closer, err := openLineReader(mrb, self, a, kwargs, 0)
	if err != nil {
		return mrb.RaiseError(err)
	}

	lines := mrb.AryNewCapa(15)
	for reader.Scan() {
		if !block.IsNil() {
			_ = mrb.YieldArgv(block, reader.Text())
			if mrb.Exc() != nil {
				return mrb.RaiseError(mrb.Err())
			}
			continue
		}
		lines.PushString(reader.Text())
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

func ioEachByte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	r, ok := mrb.Data(self).(io.Reader)
	if !ok {
		return RaiseIOError(mrb, "stream not open for reading")
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
			_ = mrb.Yield(block, oruby.Int(int(b[0])))
			if mrb.Exc() != nil {
				return mrb.RaiseError(mrb.Err())
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
		return RaiseIOError(mrb, "stream not open for reading")
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
			_ = mrb.Yield(block, mrb.StrNew(string(c)))
			if mrb.Exc() != nil {
				return mrb.RaiseError(mrb.Err())
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
		return RaiseIOError(mrb, "stream not open for reading")
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
			_ = mrb.Yield(block, oruby.Int(int(c)))
			if mrb.Exc() != nil {
				return mrb.RaiseError(mrb.Err())
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
		return RaiseIOError(mrb, "stream not open for reading")
	}

	_, err := r.Read([]byte{})
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

func getFile(mrb *oruby.MrbState, self oruby.Value) (*os.File, error) {
	if f, ok := mrb.Data(self).(*os.File); ok {
		return f, nil
	}
	return nil, oruby.EError("IOError", "IO stream is not a file")
}

func ioFsync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	_ = f.Sync()
	return oruby.Int(0)
}

func ioGetbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return RaiseIOError(mrb, "IO Stream does not suport reading")
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
		return RaiseIOError(mrb, "IO Stream does not suport reading")
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

func setLastLine(mrb *oruby.MrbState, self oruby.Value, line string) oruby.Value {
	v := mrb.StrNew(line)
	mrb.GVSet(mrb.Intern("$_"), v)

	lineno := mrb.Intern("@lineno")
	noV := mrb.IVGet(self, lineno)
	no := 0
	if !noV.IsNil() {
		no = noV.Int()
	}
	_ = mrb.IVSet(self, lineno, oruby.MrbFixnumValue(no+1))

	return v
}

func setLineNo(mrb *oruby.MrbState, self oruby.Value, v int) {
	_ = mrb.IVSet(self, mrb.Intern("@lineno"), mrb.FixnumValue(v))
}

func ioGets(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	reader, _, err := openLineReader(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		return mrb.RaiseError(err)
	}

	reader.Scan()
	if errors.Is(reader.Err(), io.EOF) {
		return mrb.NilValue()
	}
	if reader.Err() != nil {
		mrb.SetGV("$_", mrb.NilValue())
		return RaiseIOError(mrb, reader.Err().Error())
	}

	return setLastLine(mrb, self, reader.Text())
}

func ioInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self)

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
	return mrb.IVGet(self, mrb.Intern("@lineno"))
}

func ioSetLineno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	no := mrb.GetArgsFirst().Int()
	setLineNo(mrb, self, no)
	return oruby.Int(no)
}

func ioPid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NilValue()
}

func ioPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, ok := mrb.Data(self).(io.Seeker)
	if !ok {
		return RaiseIOError(mrb, "stream cannot tell position")
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
		return RaiseIOError(mrb, "stream cannot tell position")
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

	defer func() {
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

	if seeker, ok := f.(io.Seeker); ok {
		if _, err = seeker.Seek(offset, io.SeekStart); err != nil {
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

	return RaiseIOError(mrb, "IO Stream does not suport reading")
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
		return RaiseIOError(mrb, "IO Stream does not suport writing")
	}

	for i := 0; i < argc; i++ {
		s, err := valueToS(mrb, args.Item(i))
		if err != nil {
			return mrb.RaiseError(err)
		}
		_, err = w.Write(s.Bytes())
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
		return RaiseIOError(mrb, "IO Stream does not suport writing")
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
		return RaiseIOError(mrb, "IO Stream does not suport writing")
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
		_, err := io.WriteString(w, "\n")
		return err
	}

	for i := 0; i < ary.Len(); i++ {
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

		_, err = w.Write(s.Bytes())
		if err != nil {
			return err
		}

		_, err = w.Write([]byte{'\n'})
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
		return RaiseIOError(mrb, "IO Stream does not suport writing")
	}
	if argc == 0 {
		_, _ = io.WriteString(w, "\n")
		return mrb.NilValue()
	}

	for i := 0; i < argc; i++ {
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

		_, err = w.Write(s.Bytes())
		if err != nil {
			return mrb.RaiseError(err)
		}

		_, err = w.Write([]byte{'\n'})
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
		n, err = w.WriteAt(s.Bytes(), offset.Int64())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return oruby.Int(n)
	}

	if seeker, ok := f.(io.Seeker); ok {
		if _, err := seeker.Seek(offset.Int64(), io.SeekStart); err != nil {
			return mrb.RaiseError(err)
		}

		if w2, ok := f.(io.Writer); ok {
			n, err = w2.Write(s.Bytes())
			if err != nil {
				return mrb.RaiseError(err)
			}
			return oruby.Int(n)
		}
	}

	return RaiseIOError(mrb, "IO Stream does not suport writing")
}

func ioRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var length int64
	var buf []byte
	var err error
	l, outbuf := mrb.GetArgs2()

	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return RaiseIOError(mrb, "IO Stream does not suport reading")
	}

	// length: 0, nil, omitted or positive integer
	if l.IsNil() {
		//
	} else if l.IsInteger() {
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
		return RaiseIOError(mrb, "IO Stream does not suport reading")
	}
	var b byte
	_, err := r.Read([]byte{b})
	if errors.Is(err, io.EOF) {
		return RaiseEOF(mrb)
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(int(b))
}

func ioReadchar(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return RaiseIOError(mrb, "IO Stream does not suport reading")
	}
	rs := bufio.NewReader(r)
	chr, _, err := rs.ReadRune()
	if errors.Is(err, io.EOF) {
		return RaiseEOF(mrb)
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(string(chr))
}

func ioReadline(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	reader, _, err := openLineReader(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		return mrb.RaiseError(err)
	}

	reader.Scan()
	if reader.Err() != nil {
		mrb.SetGV("$_", mrb.NilValue())
		if errors.Is(reader.Err(), io.EOF) {
			RaiseEOF(mrb)
		}
		return RaiseIOError(mrb, reader.Err().Error())
	}

	return setLastLine(mrb, self, reader.Text())
}

func ioReadlines(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	reader, _, err := openLineReader(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		return mrb.RaiseError(err)
	}

	lines := mrb.AryNewCapa(15)
	for reader.Scan() {
		lines.PushString(reader.Text())
	}
	if reader.Err() != nil && reader.Err() != io.EOF {
		return mrb.RaiseError(reader.Err())
	}
	return lines
}

func ioReadpartial(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	length, outbuf := mrb.GetArgs2()
	r, ok := mrb.Data(self).(io.Reader)
	if !ok || r == nil {
		return RaiseIOError(mrb, "IO Stream does not suport reading")
	}
	if !length.IsInteger() {
		mrb.EArgumentError().Raise("maxlength must be integer")
	}

	buf := make([]byte, length.Int64())

	r = bufio.NewReader(r)
	n, err := r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return RaiseEOF(mrb)
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

func ioRewind(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	setLineNo(mrb, self, 0)

	if seeker, ok := mrb.Data(self).(io.Seeker); ok {
		_, err := seeker.Seek(0, io.SeekStart)
		if err != nil {
			return mrb.RaiseError(err)
		}
	}

	return oruby.Int(0)
}

func ioSeek(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	offset, whence := mrb.GetArgs2(0, io.SeekStart)

	seeker, ok := mrb.Data(self).(io.Seeker)
	if !ok || seeker == nil {
		return RaiseIOError(mrb, "IO object does not support seek")
	}

	pos, err := seeker.Seek(offset.Int64(), whence.Int())
	if err != nil {
		return mrb.RaiseError(err)
	}

	if pos == 0 {
		setLineNo(mrb, self, 0)
	}

	return oruby.Int(0)
}

func ioStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, ok := mrb.Data(self).(*os.File)
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
		return RaiseIOError(mrb, "IO object does not support position")
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
		return RaiseIOError(mrb, "IO object does not support writing")
	}

	cnt := int64(0)
	for i := 0; i < args.Len(); i++ {
		s, err := valueToS(mrb, args.Item(i))
		if err != nil {
			return mrb.RaiseError(err)
		}
		n, err := w.Write(s.Bytes())
		if err != nil {
			return mrb.RaiseError(err)
		}
		cnt += int64(n)
	}

	return oruby.Int64(cnt)
}
