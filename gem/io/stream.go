package io

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/oruby/oruby"
)

func unsafeBytes(p *byte, n int) []byte { return unsafe.Slice(p, n) }

// ioInitCopy is IO#initialize_copy(orig): self is the new copy, so the
// descriptor to duplicate is orig's
func ioInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src := self
	if mrb.GetArgsCount() > 0 {
		src = mrb.GetArgsFirst()
	}
	if isClosed(mrb, src) {
		return RaiseIOError(mrb, "closed stream")
	}
	if f, ok := mrb.Data(src).(*os.File); ok {
		f2, err := platformDup(f)
		if err != nil {
			return mrb.SysFail(err)
		}
		mrb.DataSetInterface(self, f2)
		// a dup is closed on exec and closes its descriptor, whatever the
		// original says (MRI)
		_ = mrb.IVSet(self, mrb.Intern("@autoclose"), oruby.True)
		_ = setCloseOnExec(f2, true)
		rbufSet(mrb, self, nil)
		return self
	}

	return mrb.EArgumentError().Raise("IO stream is not a file and does not support cloning")
}

func valueToS(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if v.Type() == oruby.MrbTTString {
		return v, nil
	}

	sv, err := mrb.FuncallWithBlock(v, oruby.Sym_to_s)
	if err != nil {
		return mrb.NilValue(), err
	}
	if sv.Type() == oruby.MrbTTString {
		return sv, nil
	}
	return mrb.StrNew(mrb.String(sv)), nil
}

// closedCheck is MRI's rb_io_check_closed
func closedCheck(mrb *oruby.MrbState, self oruby.Value) error {
	if isClosed(mrb, self) {
		return oruby.EError("IOError", "closed stream")
	}
	return nil
}

func ioSetAutosclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	arg := mrb.GetArgsFirst()
	_ = mrb.IVSet(self, mrb.Intern("@autoclose"), oruby.Bool(arg.Bool()))
	return arg
}

func ioIsAutoclose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Bool(autoclose(mrb, self))
}

func autoclose(mrb *oruby.MrbState, self oruby.Value) bool {
	sym := mrb.Intern("@autoclose")
	return !mrb.IVDefined(self, sym) || mrb.IVGet(self, sym).Bool()
}

func ioBinmode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
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
	if !encMod.IsNil() && mrb.RespondTo(encMod, oruby.Sym_default_external) {
		defExt, err := mrb.Funcall(encMod, oruby.Sym_default_external)
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

// kept keeps the files whose IO closed without closing them (autoclose =
// false): Go closes an os.File it collects, the descriptor must stay open
var kept []*os.File

func ioClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if isClosed(mrb, self) {
		return mrb.NilValue()
	}
	mrb.SetIV(self, "@closed", mrb.TrueValue())
	rbufSet(mrb, self, nil)
	f := mrb.Data(self)
	if !autoclose(mrb, self) {
		if file, ok := f.(*os.File); ok {
			kept = append(kept, file)
		}
		return mrb.NilValue()
	}
	if closer, ok := f.(io.Closer); ok {
		_ = closer.Close()
	}
	return mrb.NilValue()
}

func ioSetCloseOnExec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	arg := mrb.GetArgsFirst()
	f, err := getFile(mrb, self)
	if err != nil {
		return arg
	}
	if err := setCloseOnExec(f, arg.Bool()); err != nil {
		return mrb.SysFail(err)
	}
	return arg
}

func ioIsCloseOnExec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	f, err := getFile(mrb, self)
	if err != nil {
		return oruby.True
	}
	on, err := closeOnExec(f)
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Bool(on)
}

func ioIsClosed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(isClosed(mrb, self))
}

func raiseRead(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if errors.Is(err, io.EOF) {
		return RaiseEOF(mrb)
	}
	return mrb.RaiseError(mapReadErr(err))
}

func ioEachByte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return enumFor(mrb, self, "each_byte")
	}
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	for {
		b, err := sr.ReadByte()
		if errors.Is(err, io.EOF) {
			return self
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
		mrb.Yield(block, oruby.Int(int(b)))
		if mrb.Exc() != nil {
			return mrb.RaiseError(mrb.Err())
		}
	}
}

// enumFor: an Enumerator over each_x with no size (an IO has none)
func enumFor(mrb *oruby.MrbState, self oruby.Value, meth string) oruby.MrbValue {
	v, err := mrb.Funcall(mrb.ModuleGet("Kernel"), mrb.Intern("__oruby_io_enum"), self, mrb.SymbolValue(mrb.Intern(meth)))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return v
}

// readChar reads one UTF-8 character; an invalid or cut short sequence is
// its first byte (ok false)
func readChar(sr *streamReader) ([]byte, bool, error) {
	b0, err := sr.ReadByte()
	if err != nil {
		return nil, false, err
	}
	need := 1
	switch {
	case b0 >= 0xF0 && b0 < 0xF8:
		need = 4
	case b0 >= 0xE0:
		need = 3
	case b0 >= 0xC2:
		need = 2
	}
	if b0 >= 0xF8 {
		need = 1
	}
	if need == 1 {
		return []byte{b0}, b0 < 0x80, nil
	}
	rest, err := sr.ReadN(need - 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	c := append([]byte{b0}, rest...)
	if utf8.Valid(c) {
		return c, true, nil
	}
	rbufUnread(sr.mrb, sr.self, rest)
	return c[:1], false, nil
}

func ioEachChar(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return enumFor(mrb, self, "each_char")
	}
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	for {
		c, _, err := readChar(sr)
		if errors.Is(err, io.EOF) {
			return self
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
		mrb.Yield(block, mrb.StrNew(string(c)))
		if mrb.Exc() != nil {
			return mrb.RaiseError(mrb.Err())
		}
	}
}

func ioEachCodepoint(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return enumFor(mrb, self, "each_codepoint")
	}
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	binary := false
	if v, err := mrb.Funcall(self, mrb.Intern("binmode?")); err == nil {
		binary = v.Bool()
	}
	for {
		var cp int
		if binary {
			b, err := sr.ReadByte()
			if errors.Is(err, io.EOF) {
				return self
			}
			if err != nil {
				return mrb.RaiseError(err)
			}
			cp = int(b)
		} else {
			c, ok, err := readChar(sr)
			if errors.Is(err, io.EOF) {
				return self
			}
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !ok && c[0] >= 0x80 {
				return mrb.Raise(mrb.EArgumentError(), "invalid byte sequence in UTF-8")
			}
			r, _ := utf8.DecodeRune(c)
			cp = int(r)
		}
		mrb.Yield(block, oruby.Int(cp))
		if mrb.Exc() != nil {
			return mrb.RaiseError(mrb.Err())
		}
	}
}

func ioIsEof(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	eof, err := sr.AtEOF()
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Bool(eof)
}

func ioFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	return oruby.Int(RawFd(f))
}

func ioIsTty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.FalseValue()
	}
	return oruby.Bool(platformIsatty(f))
}

func ioFlush(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
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
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	f, err := getFile(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	_ = f.Sync()
	return oruby.Int(0)
}

func ioGetbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	b, err := sr.ReadByte()
	if errors.Is(err, io.EOF) {
		return mrb.NilValue()
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(int(b))
}

func ioGetc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	c, _, err := readChar(sr)
	if errors.Is(err, io.EOF) {
		return mrb.NilValue()
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(string(c))
}

func setLastLine(mrb *oruby.MrbState, self oruby.Value, line string) oruby.Value {
	v := mrb.StrNew(line)
	mrb.GVSet(oruby.GvSym__, v)

	lineno := oruby.IvSym_lineno
	noV := mrb.IVGet(self, lineno)
	no := 0
	if !noV.IsNil() {
		no = noV.Int()
	}
	_ = mrb.IVSet(self, lineno, oruby.MrbFixnumValue(no+1))

	return v
}

func setLineNo(mrb *oruby.MrbState, self oruby.Value, v int) {
	_ = mrb.IVSet(self, oruby.IvSym_lineno, mrb.FixnumValue(v))
}

func ioGets(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	line, err := readLine(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		mrb.SetGV("$_", mrb.NilValue())
		if errors.Is(err, io.EOF) {
			return mrb.NilValue()
		}
		return mrb.RaiseError(err)
	}

	return setLastLine(mrb, self, string(line))
}

// ioInspect is MRI's rb_io_inspect: #<Class:path> or #<Class:fd N>, and
// (closed)
func ioInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.ClassName(mrb.ObjClass(self))
	what := ""
	if p := mrb.IVGet(self, mrb.Intern("@__oruby_path")); p.IsString() {
		what = mrb.String(p)
	} else if p := mrb.GetIV(self, "@path"); p.IsString() && mrb.ClassDefined("File") && mrb.ObjIsKindOf(self, mrb.ClassGet("File")) {
		// a File opened by path
		what = mrb.String(p)
	} else if f, ok := mrb.Data(self).(*os.File); ok && f != nil {
		switch f {
		case os.Stdin:
			what = "<STDIN>"
		case os.Stdout:
			what = "<STDOUT>"
		case os.Stderr:
			what = "<STDERR>"
		default:
			if fd := RawFd(f); fd >= 0 {
				what = fmt.Sprintf("fd %d", fd)
			}
		}
	}
	if isClosed(mrb, self) {
		if what == "" {
			return mrb.StrNew(fmt.Sprintf("#<%s:(closed)>", name))
		}
		return mrb.StrNew(fmt.Sprintf("#<%s:%s (closed)>", name, what))
	}
	return mrb.StrNew(fmt.Sprintf("#<%s:%s>", name, what))
}

func ioLineno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.IVGet(self, oruby.IvSym_lineno)
}

func ioSetLineno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	no := mrb.GetArgsFirst().Int()
	setLineNo(mrb, self, no)
	return oruby.Int(no)
}

func ioPid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.NilValue()
}

// offArg is an off_t argument: Integer or #to_int, RangeError past int64
func offArg(mrb *oruby.MrbState, v oruby.Value) (int64, error) {
	if v.Type() == oruby.MrbTTFloat {
		f := v.Float64()
		if f >= math.MaxInt64 || f < math.MinInt64 {
			return 0, oruby.ERangeError("float %v out of range of off_t", f)
		}
		return int64(f), nil
	}
	if !v.IsInteger() && v.Type() != oruby.MrbTTBigInt {
		r, err := mrb.Funcall(mrb.ModuleGet("Kernel"), mrb.Intern("__to_int"), v)
		if err != nil {
			return 0, err
		}
		v = r
	}
	n, err := mrb.AsInt64(v)
	if err != nil {
		return 0, oruby.ERangeError("bignum too big to convert into 'long long'")
	}
	return n, nil
}

func ioPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	s, ok := mrb.Data(self).(io.Seeker)
	if !ok {
		return RaiseIOError(mrb, "stream cannot tell position")
	}
	pos, err := s.Seek(0, io.SeekCurrent)
	if err != nil {
		return mrb.SysFail(err)
	}
	pos -= int64(rbufLen(mrb, self))
	if pos < 0 {
		pos = 0
	}
	return oruby.Int64(pos)
}

func ioSetPos(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pos, err := offArg(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	s, ok := mrb.Data(self).(io.Seeker)
	if !ok {
		return RaiseIOError(mrb, "stream cannot tell position")
	}
	rbufSet(mrb, self, nil)
	if _, err := s.Seek(pos, io.SeekStart); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int64(pos)
}

// ioPread is IO#pread(maxlen, offset[, outbuf]): one positioned read, the
// file position and the buffer left alone
func ioPread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	maxlen, err := offArg(mrb, args.Item(0))
	if err != nil {
		return mrb.RaiseError(err)
	}
	offset, err := offArg(mrb, args.Item(1))
	if err != nil {
		return mrb.RaiseError(err)
	}
	outbuf := args.Item(2)
	if !outbuf.IsNil() {
		if outbuf, err = strArg(mrb, outbuf); err != nil {
			return mrb.RaiseError(err)
		}
	}
	if maxlen < 0 {
		return mrb.Raise(mrb.EArgumentError(), "negative string size (or size too big)")
	}
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	if !rubyReadable(mrb, self) {
		return RaiseIOError(mrb, "not opened for reading")
	}
	if maxlen == 0 {
		if outbuf.IsString() {
			return outbuf
		}
		return binaryStringValue(mrb, nil)
	}
	if offset < 0 {
		return mrb.SysFail(syscall.EINVAL)
	}
	r, ok := mrb.Data(self).(io.ReaderAt)
	if !ok {
		return RaiseIOError(mrb, "not opened for reading")
	}
	buf := make([]byte, maxlen)
	n, err := r.ReadAt(buf, offset)
	if n == 0 {
		if outbuf.IsString() {
			mrb.StrResize(outbuf, 0)
		}
		if err == nil || errors.Is(err, io.EOF) {
			return RaiseEOF(mrb)
		}
		return mrb.RaiseError(mapReadErr(err))
	}
	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, buf[:n])
		return outbuf
	}
	return binaryStringValue(mrb, buf[:n])
}

// strArg is a String argument: String or #to_str
func strArg(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if v.IsString() {
		return v, nil
	}
	return mrb.Funcall(mrb.ModuleGet("Kernel"), mrb.Intern("__implicit"), v, mrb.ClassGet("String"))
}

// ioPwrite is IO#pwrite(string, offset)
func ioPwrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str, off := mrb.GetArgs2()
	offset, err := offArg(mrb, off)
	if err != nil {
		return mrb.RaiseError(err)
	}
	s, err := valueToS(mrb, str)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	if offset < 0 {
		return mrb.SysFail(syscall.EINVAL)
	}
	w, ok := mrb.Data(self).(io.WriterAt)
	if !ok {
		return RaiseIOError(mrb, "not opened for writing")
	}
	n, err := w.WriteAt(s.Bytes(), offset)
	if err != nil {
		return mrb.RaiseError(mapWriteErr(err))
	}
	return oruby.Int(n)
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

// lengthArg: a read length, Integer or #to_int, not negative
func lengthArg(mrb *oruby.MrbState, l oruby.Value) (int64, error) {
	if !l.IsInteger() {
		r, err := mrb.Funcall(mrb.ModuleGet("Kernel"), mrb.Intern("__implicit"), l, mrb.ClassGet("Integer"))
		if err != nil {
			return 0, err
		}
		l = r
	}
	n := l.Int64()
	if n < 0 {
		return 0, oruby.EArgumentError("negative length %d given", n)
	}
	return n, nil
}

// bufArg: an output buffer, String or #to_str, not frozen
func bufArg(mrb *oruby.MrbState, outbuf oruby.Value) (oruby.Value, error) {
	if outbuf.IsNil() {
		return outbuf, nil
	}
	b, err := strArg(mrb, outbuf)
	if err != nil {
		return oruby.Nil, err
	}
	if err := mrb.CheckFrozen(b); err != nil {
		return oruby.Nil, err
	}
	return b, nil
}

func ioRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	l, outbuf := mrb.GetArgs2()
	var length int64
	var err error
	if !l.IsNil() {
		if length, err = lengthArg(mrb, l); err != nil {
			return mrb.RaiseError(err)
		}
	}
	if outbuf, err = bufArg(mrb, outbuf); err != nil {
		return mrb.RaiseError(err)
	}
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if !l.IsNil() && length == 0 {
		if outbuf.IsString() {
			mrb.StrResize(outbuf, 0)
			return outbuf
		}
		return binaryStringValue(mrb, nil)
	}

	var buf []byte
	if l.IsNil() {
		buf, err = sr.ReadAll()
	} else {
		buf, err = sr.ReadN(int(length))
		if errors.Is(err, io.EOF) {
			buf, err = nil, nil
		}
	}
	if err != nil {
		return mrb.RaiseError(err)
	}

	if !l.IsNil() && len(buf) == 0 {
		if outbuf.IsString() {
			mrb.StrResize(outbuf, 0)
		}
		return mrb.NilValue()
	}
	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, buf)
		if l.IsNil() {
			if ext := ioExternalEncoding(mrb, self); !ext.IsNil() {
				mrb.Funcall(outbuf, oruby.Sym_force_encoding, ext)
			}
		}
		return outbuf
	}
	if !l.IsNil() {
		return binaryStringValue(mrb, buf)
	}
	ret := mrb.BytesValue(buf)
	if ext := ioExternalEncoding(mrb, self); !ext.IsNil() {
		mrb.Funcall(ret, oruby.Sym_force_encoding, ext)
	}
	return ret
}

func ioReadbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	b, err := sr.ReadByte()
	if err != nil {
		return raiseRead(mrb, err)
	}
	return oruby.Int(int(b))
}

func ioReadchar(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	c, _, err := readChar(sr)
	if err != nil {
		return raiseRead(mrb, err)
	}
	return mrb.StrNew(string(c))
}

func ioReadline(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	line, err := readLine(mrb, self, mrb.GetArgs(), mrb.KeywordArgs(), 0)
	if err != nil {
		mrb.SetGV("$_", mrb.NilValue())
		return raiseRead(mrb, err)
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
			return mrb.RaiseError(err)
		}
		setLastLine(mrb, self, string(line))
		lines.PushString(string(line))
	}
	return lines
}

// ioReadpartial is IO#readpartial(maxlen[, outbuf]): what is buffered, or
// what one read gives; it waits only while nothing is there
func ioReadpartial(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	l, outbuf := mrb.GetArgs2()
	length, err := lengthArg(mrb, l)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if outbuf, err = bufArg(mrb, outbuf); err != nil {
		return mrb.RaiseError(err)
	}
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if length == 0 {
		if outbuf.IsString() {
			mrb.StrResize(outbuf, 0)
			return outbuf
		}
		return binaryStringValue(mrb, nil)
	}
	buf, err := sr.Partial(int(length))
	if err != nil {
		if outbuf.IsString() {
			mrb.StrResize(outbuf, 0)
		}
		return raiseRead(mrb, err)
	}
	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, buf)
		return outbuf
	}
	return binaryStringValue(mrb, buf)
}

// ioSysread is IO#sysread(maxlen[, outbuf]): one read of the descriptor,
// which buffered bytes would put out of order
func ioSysread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	l, outbuf := mrb.GetArgs2()
	length, err := lengthArg(mrb, l)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if outbuf, err = bufArg(mrb, outbuf); err != nil {
		return mrb.RaiseError(err)
	}
	if length == 0 {
		if outbuf.IsString() {
			return outbuf
		}
		return binaryStringValue(mrb, nil)
	}
	sr, err := ioReader(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if rbufLen(mrb, self) > 0 {
		return RaiseIOError(mrb, "sysread for buffered IO")
	}
	chunk := make([]byte, length)
	n, err := interruptible(mrb, sr.r).Read(chunk)
	if n == 0 {
		if outbuf.IsString() {
			mrb.StrResize(outbuf, 0)
		}
		if err == nil || errors.Is(err, io.EOF) {
			return mrb.Raise(mrb.ExcGet("EOFError"), "end of file reached")
		}
		return mrb.RaiseError(mapReadErr(err))
	}
	if outbuf.IsString() {
		mrb.StrResize(outbuf, 0)
		mrb.StrCatBytes(outbuf, chunk[:n])
		return outbuf
	}
	return binaryStringValue(mrb, chunk[:n])
}

func ioRewind(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	setLineNo(mrb, self, 0)
	rbufSet(mrb, self, nil)
	if seeker, ok := mrb.Data(self).(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Int(0)
}

// whenceArg: an Integer or :SET, :CUR, :END (and :DATA, :HOLE where the OS
// has them)
func whenceArg(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	if v.IsNil() {
		return io.SeekStart, nil
	}
	if v.Type() == oruby.MrbTTSymbol {
		switch mrb.SymName(v.Symbol()) {
		case "SET":
			return io.SeekStart, nil
		case "CUR":
			return io.SeekCurrent, nil
		case "END":
			return io.SeekEnd, nil
		case "DATA":
			return seekData, nil
		case "HOLE":
			return seekHole, nil
		}
		return 0, oruby.EArgumentError("unknown whence: %s", mrb.SymName(v.Symbol()))
	}
	n, err := offArg(mrb, v)
	return int(n), err
}

func seekArgs(mrb *oruby.MrbState) (int64, int, error) {
	args := mrb.GetArgs()
	offset, err := offArg(mrb, args.Item(0))
	if err != nil {
		return 0, 0, err
	}
	whence, err := whenceArg(mrb, args.Item(1))
	return offset, whence, err
}

func ioSeek(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	offset, whence, err := seekArgs(mrb)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	seeker, ok := mrb.Data(self).(io.Seeker)
	if !ok || seeker == nil {
		return RaiseIOError(mrb, "IO object does not support seek")
	}
	if whence == io.SeekCurrent {
		offset -= int64(rbufLen(mrb, self))
	}
	rbufSet(mrb, self, nil)
	if _, err := seeker.Seek(offset, whence); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int(0)
}

// ioSysseek: the descriptor's own seek; it returns the new offset
func ioSysseek(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	offset, whence, err := seekArgs(mrb)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	if rbufLen(mrb, self) > 0 {
		return RaiseIOError(mrb, "sysseek for buffered IO")
	}
	seeker, ok := mrb.Data(self).(io.Seeker)
	if !ok || seeker == nil {
		return RaiseIOError(mrb, "IO object does not support seek")
	}
	pos, err := seeker.Seek(offset, whence)
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int64(pos)
}

func ioStat(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	f, ok := mrb.Data(self).(*os.File)
	if !ok {
		// a stream over a pipe (IO.popen): the pipe's
		if rf, isPipe := mrb.Data(self).(interface{ ReadFile() *os.File }); isPipe {
			f = rf.ReadFile()
			ok = f != nil
		}
	}
	if ok {
		stat, err := f.Stat()
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.Value(stat)
	}
	return mrb.EArgumentError().Raise("IO object does not support stat")
}

func ioToIo(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return self
}

// ioUngetbyte is IO#ungetbyte(byte | string | nil): the bytes go back in
// front of the buffer
func ioUngetbyte(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if _, err := ioReader(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	var b []byte
	switch {
	case arg.IsNil():
		return mrb.NilValue()
	case arg.IsInteger() || arg.Type() == oruby.MrbTTBigInt:
		n, err := mrb.Funcall(arg, oruby.OpSym_and, 0xff)
		if err != nil {
			return mrb.RaiseError(err)
		}
		b = []byte{byte(n.Int())}
	default:
		s, err := strArg(mrb, arg)
		if err != nil {
			return mrb.RaiseError(err)
		}
		b = rubyBytesOf(s)
	}
	rbufUnread(mrb, self, b)
	return mrb.NilValue()
}

// ioUngetc is IO#ungetc(char | codepoint): the character goes back in front
// of the buffer
func ioUngetc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if _, err := ioReader(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	var b []byte
	switch {
	case arg.IsInteger():
		// the character in the stream's encoding (the Ruby side's, which
		// set_encoding changes)
		var enc oruby.MrbValue
		enc, err := mrb.Funcall(self, oruby.Sym_external_encoding)
		if err != nil || enc.IsNil() {
			enc = ioExternalEncoding(mrb, self)
		}
		ch, err := mrb.Funcall(arg, oruby.Sym_chr, enc)
		if err != nil {
			ch, err = mrb.Funcall(arg, oruby.Sym_chr)
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
		b = rubyBytesOf(ch)
	case arg.Type() == oruby.MrbTTBigInt:
		return mrb.Raise(mrb.ERangeError(), "bignum out of char range")
	default:
		s, err := strArg(mrb, arg)
		if err != nil {
			return mrb.RaiseError(err)
		}
		b = rubyBytesOf(s)
	}
	rbufUnread(mrb, self, b)
	return mrb.NilValue()
}

// ioWrite is IO#write(*objects): the read-ahead goes back first, so the
// bytes land where the program is
func ioWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	w, ok := mrb.Data(self).(io.Writer)
	if !ok || w == nil {
		return RaiseIOError(mrb, "not opened for writing")
	}
	total := 0
	parts := make([][]byte, 0, args.Len())
	for i := 0; i < args.Len(); i++ {
		s, err := valueToS(mrb, args.Item(i))
		if err != nil {
			return mrb.RaiseError(err)
		}
		b := rubyBytesOf(s)
		total += len(b)
		parts = append(parts, b)
	}
	if total == 0 {
		return oruby.Int(0)
	}
	// a descriptor not open for writing fails with EBADF: IOError
	rbufDrop(mrb, self)
	w = interruptibleWriter(mrb, w)
	cnt := int64(0)
	for _, b := range parts {
		n, err := w.Write(b)
		cnt += int64(n)
		if err != nil {
			if errors.Is(err, syscall.EPIPE) {
				// MRI's FMODE_SIGNAL_ON_EPIPE: a closed stdout or stderr ends
				// the program as SIGPIPE would
				if f, ok := mrb.Data(self).(*os.File); ok && (f == os.Stdout || f == os.Stderr) {
					if exc, e := mrb.Funcall(mrb.ClassGet("SignalException").Value(), oruby.Sym_new, "PIPE"); e == nil {
						return mrb.RaiseObj(exc)
					}
				}
			}
			return mrb.RaiseError(mapWriteErr(err))
		}
	}
	return oruby.Int64(cnt)
}

// ioSyswrite is IO#syswrite: the bytes of to_s, written at once
func ioSyswrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := closedCheck(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	f, ok := mrb.Data(self).(*os.File)
	if !ok {
		return ioWrite(mrb, self)
	}
	str, err := valueToS(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	b := rubyBytesOf(str)
	if len(b) == 0 {
		return oruby.Int(0)
	}
	rbufDrop(mrb, self)
	n, err := writeOnce(mrb, f, b)
	if err != nil {
		return mrb.RaiseError(mapWriteErr(err))
	}
	return oruby.Int(n)
}

// mapWriteErr: a descriptor not open for writing is MRI's IOError; a broken
// pipe is Errno::EPIPE (oruby ignores SIGPIPE the way MRI does)
func mapWriteErr(err error) error {
	if errors.Is(err, syscall.EBADF) || errors.Is(err, os.ErrClosed) {
		return oruby.EError("IOError", "not opened for writing")
	}
	return err
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
