package io

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"syscall"

	"github.com/oruby/oruby"
)

// ioInitCopy is IO#initialize_copy(orig): self is the new copy, so the
// descriptor to duplicate is orig's
func ioInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src := self
	if mrb.GetArgsCount() > 0 {
		src = mrb.GetArgsFirst()
	}
	if f, ok := mrb.Data(src).(*os.File); ok {
		f2, err := platformDup(f)
		if err != nil {
			return mrb.SysFail(err)
		}
		mrb.DataSetInterface(self, f2)
		return self
	}

	return mrb.EArgumentError().Raise("IO stream is not a file and does not support cloning")
}

func valueToS(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if v.Type() == oruby.MrbTTString {
		return v, nil
	}

	sv, err := mrb.FuncallWithBlock(v, mrb.Intern("to_s"))
	if err != nil {
		return mrb.NilValue(), err
	}
	if sv.Type() == oruby.MrbTTString {
		return sv, nil
	}
	return mrb.StrNew(mrb.String(sv)), nil
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
		if errors.Is(err, syscall.EBADF) {
			return RaiseIOError(mrb, "not opened for writing")
		}
		return mrb.RaiseError(err)
	}

	return mrb.StrNew(fmt.Sprintf("%v%v", s, ioInspect(mrb, self).Value()))
}

func ioSetAutosclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	mrb.SetIV(self, "@autoclose", arg)
	return arg
}

func ioIsAutoclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetIV(self, "@autoclose")
	if v.IsNil() {
		return oruby.True
	}
	return v
}

func ioBinmode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return self
}

func ioIsBinbode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.True
}

func ioExternalEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetIV(self, "@external_encoding")
	if !v.IsNil() {
		return v
	}
	encMod := mrb.EncodingClass()
	if !encMod.IsNil() && mrb.RespondTo(encMod, mrb.Intern("default_external")) {
		defExt, err := mrb.Funcall(encMod, mrb.Intern("default_external"))
		if err == nil && !defExt.IsNil() {
			return defExt
		}
	}
	return mrb.NilValue()
}

func ioInternalEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetIV(self, "@internal_encoding")
}

func isClosed(mrb *oruby.MrbState, self oruby.Value) bool {
	v := mrb.GetIV(self, "@closed")
	if !v.IsNil() && v.Bool() {
		return true
	}
	f := mrb.Data(self)
	if f == nil {
		return true
	}
	if file, ok := f.(*os.File); ok {
		if RawFd(file) < 0 {
			return true
		}
	}
	return false
}

func ioClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if isClosed(mrb, self) {
		return mrb.NilValue()
	}
	mrb.SetIV(self, "@closed", mrb.TrueValue())
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
	return oruby.Bool(isClosed(mrb, self))
}

func ioEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a, kwargs, block := mrb.GetAllArgs()
	if block.IsNil() {
		return mrb.Call(self, "to_enum", mrb.Intern("each_line"))
	}

	for {
		line, err := readLine(mrb, self, a, kwargs, 0)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, syscall.EBADF) {
				return RaiseIOError(mrb, "not opened for reading")
			}
			return RaiseIOError(mrb, err.Error())
		}
		v := setLastLine(mrb, self, string(line))
		_ = mrb.YieldArgv(block, v)
		if mrb.Exc() != nil {
			return mrb.RaiseError(mrb.Err())
		}
	}
	return self
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
	data := mrb.Data(self)
	if data == nil {
		return RaiseIOError(mrb, "closed stream")
	}
	if f, ok := data.(*os.File); ok {
		pos, err := f.Seek(0, io.SeekCurrent)
		if err == nil {
			stat, err := f.Stat()
			if err == nil {
				return oruby.Bool(pos >= stat.Size())
			}
		}
	}
	r, ok := data.(io.Reader)
	if !ok {
		return RaiseIOError(mrb, "stream not open for reading")
	}

	buf := make([]byte, 1)
	n, err := r.Read(buf)
	if n > 0 {
		if seeker, ok := data.(io.Seeker); ok {
			_, _ = seeker.Seek(-int64(n), io.SeekCurrent)
		}
		return oruby.False
	}
	return oruby.Bool(err == io.EOF)
}

func ioFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int(RawFd(f))
}

func ioIsTty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.FalseValue()
	}
	return oruby.Bool(platformIsatty(f))
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

	if br, ok := r.(io.ByteReader); ok {
		b, err := br.ReadByte()
		if errors.Is(err, io.EOF) {
			return mrb.NilValue()
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
		return oruby.Int(int(b))
	}

	var b [1]byte
	n, err := r.Read(b[:])
	if n == 0 || errors.Is(err, io.EOF) {
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

	if rr, ok := r.(io.RuneReader); ok {
		rn, _, err := rr.ReadRune()
		if errors.Is(err, io.EOF) {
			return mrb.NilValue()
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.StrNew(string(rn))
	}

	var b [4]byte
	n, err := r.Read(b[:1])
	if n == 0 || errors.Is(err, io.EOF) {
		return mrb.NilValue()
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	need := 1
	if b[0] >= 0xC0 && b[0] < 0xE0 {
		need = 2
	} else if b[0] >= 0xE0 && b[0] < 0xF0 {
		need = 3
	} else if b[0] >= 0xF0 {
		need = 4
	}
	if need > 1 {
		_, _ = io.ReadFull(r, b[1:need])
	}
	return mrb.StrNew(string(b[:need]))
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
	line, err := readLine(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		mrb.SetGV("$_", mrb.NilValue())
		if errors.Is(err, io.EOF) {
			return mrb.NilValue()
		}
		if errors.Is(err, syscall.EBADF) {
			return RaiseIOError(mrb, "not opened for reading")
		}
		return RaiseIOError(mrb, err.Error())
	}

	return setLastLine(mrb, self, string(line))
}

func ioInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self)

	if f == os.Stdout {
		return mrb.StrNew("<IO:STDOUT>")
	} else if f == os.Stdin {
		return mrb.StrNew(fmt.Sprintf("<IO:STDIN>"))
	} else if f == os.Stderr {
		return mrb.StrNew(fmt.Sprintf("<IO:STERR>"))
	} else if f, ok := f.(*os.File); ok && f != nil {
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

	items := make([]oruby.Value, argc)
	for i := range items {
		items[i] = args.Item(i)
	}
	if argc == 0 {
		// print with no arguments prints $_
		if last := mrb.GetGV("$_"); !last.IsNil() {
			items, argc = []oruby.Value{last}, 1
		}
	}
	for i := 0; i < argc; i++ {
		s, err := valueToS(mrb, items[i])
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

// putsValue writes v as IO#puts does: an Array element by element (a
// recursive one as "[...]"), anything else as to_s followed by a newline
// unless it already ends with one.
func putsValue(mrb *oruby.MrbState, v oruby.Value, w io.Writer, seen map[oruby.Value]bool) error {
	if v.IsArray() {
		if seen[v] {
			_, err := io.WriteString(w, "[...]\n")
			return err
		}
		seen[v] = true
		defer delete(seen, v)
		for i := 0; i < v.Len(); i++ {
			if err := putsValue(mrb, mrb.AryEntry(v, i), w, seen); err != nil {
				return err
			}
		}
		return nil
	}
	s, err := valueToS(mrb, v)
	if err != nil {
		return err
	}
	b := s.Bytes()
	if _, err := w.Write(b); err != nil {
		return err
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		_, err = w.Write([]byte{'\n'})
	}
	return err
}

func ioPuts(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	argc := args.Len()
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return RaiseIOError(mrb, "IO Stream does not suport writing")
	}
	writeErr := func(err error) oruby.MrbValue {
		if errors.Is(err, syscall.EBADF) {
			return RaiseIOError(mrb, "not opened for writing")
		}
		return mrb.RaiseError(err)
	}
	if argc == 0 {
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return writeErr(err)
		}
		return mrb.NilValue()
	}
	seen := map[oruby.Value]bool{}
	for i := 0; i < argc; i++ {
		if err := putsValue(mrb, args.Item(i), w, seen); err != nil {
			return writeErr(err)
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

func binaryStringValue(mrb *oruby.MrbState, buf []byte) oruby.Value {
	v := mrb.BytesValue(buf)
	if mrb.RespondTo(v, mrb.Intern("force_encoding")) {
		modEnc := mrb.EncodingClass()
		if !modEnc.IsNil() && mrb.ConstDefined(modEnc, mrb.Intern("BINARY")) {
			binEnc := mrb.ConstGet(modEnc, mrb.Intern("BINARY"))
			mrb.Funcall(v, mrb.Intern("force_encoding"), binEnc)
		} else {
			mrb.Funcall(v, mrb.Intern("force_encoding"), mrb.StringValue("ASCII-8BIT"))
		}
	}
	return v
}

func ioRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var length int64
	var buf []byte
	var err error
	l, outbuf := mrb.GetArgs2()

	data := mrb.Data(self)
	if data == nil {
		return RaiseIOError(mrb, "closed stream")
	}

	r, ok := data.(io.Reader)
	if !ok || r == nil {
		return RaiseIOError(mrb, "IO Stream does not suport reading")
	}

	if !l.IsNil() {
		if !l.IsInteger() {
			if mrb.RespondTo(l, mrb.Intern("to_int")) {
				res, err := mrb.Funcall(l, mrb.Intern("to_int"))
				if err != nil {
					return mrb.RaiseError(err)
				}
				if !res.IsInteger() {
					return mrb.Raisef(mrb.ETypeError(), "can't convert %v into Integer (%v#to_int gives %v)", mrb.TypeName(l), mrb.TypeName(l), mrb.TypeName(res))
				}
				l = res.Value()
			} else {
				return mrb.Raisef(mrb.ETypeError(), "no implicit conversion of %v into Integer", mrb.TypeName(l))
			}
		}
		length = l.Int64()
		if length < 0 {
			return mrb.EArgumentError().Raise(fmt.Sprintf("negative length %d given", length))
		}
	}

	if !outbuf.IsNil() {
		if !outbuf.IsString() && mrb.RespondTo(outbuf, mrb.Intern("to_str")) {
			res, err := mrb.Funcall(outbuf, mrb.Intern("to_str"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			outbuf = res.Value()
		}
		if !outbuf.IsString() {
			return mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
		}
		if err := mrb.CheckFrozen(outbuf); err != nil {
			return mrb.RaiseError(err)
		}
	}

	if !l.IsNil() {
		if length == 0 {
			if outbuf.IsString() {
				mrb.StrResize(outbuf, 0)
				return outbuf
			}
			return binaryStringValue(mrb, []byte(""))
		}
		r = io.LimitReader(r, length)
	}

	buf, err = ioutil.ReadAll(r)
	if err != nil {
		if errors.Is(err, syscall.EBADF) {
			return RaiseIOError(mrb, "not opened for reading")
		}
		return mrb.RaiseError(err)
	}

	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, buf)
		if l.IsNil() {
			extEnc := ioExternalEncoding(mrb, self)
			if !extEnc.IsNil() && mrb.RespondTo(outbuf, mrb.Intern("force_encoding")) {
				mrb.Funcall(outbuf, mrb.Intern("force_encoding"), extEnc)
			}
		}
		if len(buf) == 0 && !l.IsNil() {
			return mrb.NilValue()
		}
		return outbuf
	}

	if len(buf) == 0 && !l.IsNil() {
		return mrb.NilValue()
	}

	ret := mrb.BytesValue(buf)
	if l.IsNil() {
		extEnc := ioExternalEncoding(mrb, self)
		if !extEnc.IsNil() && mrb.RespondTo(ret, mrb.Intern("force_encoding")) {
			mrb.Funcall(ret, mrb.Intern("force_encoding"), extEnc)
		}
	} else {
		if mrb.RespondTo(ret, mrb.Intern("force_encoding")) {
			modEnc := mrb.EncodingClass()
			if !modEnc.IsNil() && mrb.ConstDefined(modEnc, mrb.Intern("BINARY")) {
				binEnc := mrb.ConstGet(modEnc, mrb.Intern("BINARY"))
				mrb.Funcall(ret, mrb.Intern("force_encoding"), binEnc)
			}
		}
	}
	return ret
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
	line, err := readLine(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		mrb.SetGV("$_", mrb.NilValue())
		if errors.Is(err, io.EOF) {
			return RaiseEOF(mrb)
		}
		if errors.Is(err, syscall.EBADF) {
			return RaiseIOError(mrb, "not opened for reading")
		}
		return RaiseIOError(mrb, err.Error())
	}

	return setLastLine(mrb, self, string(line))
}

func ioReadlines(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	kwargs := mrb.KeywordArgs()
	lines := mrb.AryNewCapa(15)
	for {
		line, err := readLine(mrb, self, a, kwargs, 0)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, syscall.EBADF) {
				return RaiseIOError(mrb, "not opened for reading")
			}
			return RaiseIOError(mrb, err.Error())
		}
		setLastLine(mrb, self, string(line))
		lines.PushString(string(line))
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
	switch r := mrb.Data(self).(type) {
	case *bufio.Reader:
		if err := r.UnreadByte(); err != nil {
			return mrb.RaiseError(err)
		}
	case io.Seeker:
		// a seekable file is read unbuffered: pushing back the byte just
		// read is stepping back over it
		n := int64(1)
		if arg := mrb.GetArgsFirst(); arg.IsString() {
			n = int64(len(arg.Bytes()))
		} else if arg.IsNil() {
			n = 0
		}
		if n > 0 {
			if _, err := r.Seek(-n, io.SeekCurrent); err != nil {
				return mrb.RaiseError(err)
			}
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

	data := mrb.Data(self)
	if data == nil {
		return RaiseIOError(mrb, "closed stream")
	}

	w, ok := data.(io.Writer)
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
			if errors.Is(err, syscall.EBADF) {
				return RaiseIOError(mrb, "not opened for writing")
			}
			return mrb.RaiseError(err)
		}
		cnt += int64(n)
	}

	return oruby.Int64(cnt)
}

// RawFd returns the descriptor of f without switching it to blocking mode
// (os.File.Fd does that for pollable files such as non-blocking sockets).
// Returns -1 for a closed file.
func RawFd(f *os.File) int {
	fd := -1
	sc, err := f.SyscallConn()
	if err != nil {
		return -1
	}
	if err := sc.Control(func(p uintptr) { fd = int(p) }); err != nil {
		return -1
	}
	return fd
}
