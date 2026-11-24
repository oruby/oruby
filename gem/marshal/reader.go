package marshal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

type byteSource interface {
	Read([]byte) (int, error)
}

// rubyReader reads from a Ruby IO (or anything with read)
type rubyReader struct {
	mrb    *oruby.MrbState
	object oruby.Value
}

func (r *rubyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	value, err := r.mrb.Funcall(r.object, oruby.Sym_read, len(p))
	if err != nil {
		return 0, err
	}
	if value.IsNil() {
		return 0, io.EOF
	}
	if value.Type() != oruby.MrbTTString {
		return 0, oruby.ETypeError("IO#read must return String or nil")
	}
	if rubyByteLen(value) > len(p) {
		return 0, oruby.EArgumentError("IO#read returned more bytes than requested")
	}
	data := rubyBytes(value)
	if len(data) == 0 {
		return 0, io.EOF
	}
	return copy(p, data), nil
}

// reader follows MRI's marshal.c (r_object0 & co.). An object is "partial"
// while its contents are read: a link to it then gets it as it is, and the
// load proc and freeze: run once it is complete (r_leave).
type reader struct {
	mrb     *oruby.MrbState
	source  byteSource
	fromIO  bool
	objects []oruby.Value
	partial map[uintptr]bool
	symbols []oruby.MrbSym
	nodes   int
	depth   int
	read    int64
	proc    oruby.Value // Marshal.load(src, proc)
	freeze  bool        // Marshal.load(src, freeze: true)
}

func loadValue(mrb *oruby.MrbState, source byteSource) (oruby.Value, error) {
	return loadValueWith(mrb, source, oruby.Nil, false, false)
}

func loadValueWith(mrb *oruby.MrbState, source byteSource, proc oruby.Value, freeze, fromIO bool) (oruby.Value, error) {
	if !mrb.GCDisabled() {
		mrb.GCDisable()
		defer mrb.GCEnable()
	}
	r := &reader{mrb: mrb, source: source, fromIO: fromIO, proc: proc, freeze: freeze,
		partial: make(map[uintptr]bool)}
	var header [2]byte
	if err := r.full(header[:]); err != nil {
		return oruby.Nil, err
	}
	if header[0] != 4 || header[1] > 8 {
		return oruby.Nil, oruby.ETypeError("incompatible marshal file format (can't be read)\n\tformat version 4.8 required; %d.%d given", header[0], header[1])
	}
	return r.object()
}

func (r *reader) short() error {
	if r.fromIO {
		return oruby.EError("EOFError", "end of file reached")
	}
	return oruby.EArgumentError("marshal data too short")
}

func (r *reader) full(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if int64(len(data)) > int64(maxInputBytes)-r.read {
		return oruby.ERangeError("Marshal input exceeds size limit")
	}
	n, err := io.ReadFull(r.source, data)
	r.read += int64(n)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return r.short()
		}
		return err
	}
	return nil
}

func (r *reader) byte1() (byte, error) {
	var b [1]byte
	err := r.full(b[:])
	return b[0], err
}

// long is MRI's r_long
func (r *reader) long() (int64, error) {
	b, err := r.byte1()
	if err != nil {
		return 0, err
	}
	n := int8(b)
	switch {
	case n == 0:
		return 0, nil
	case n > 4:
		return int64(n) - 5, nil
	case n < -4:
		return int64(n) + 5, nil
	}
	length := int(n)
	negative := length < 0
	if negative {
		length = -length
	}
	var data [8]byte
	if negative {
		for i := range data {
			data[i] = 0xff
		}
	}
	if err := r.full(data[:length]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(data[:])), nil
}

func (r *reader) count() (int, error) {
	n, err := r.long()
	if err != nil {
		return 0, err
	}
	if n < 0 || n > maxNodes {
		return 0, oruby.ERangeError("Marshal item count exceeds size limit")
	}
	return int(n), nil
}

// bytes is MRI's r_bytes
func (r *reader) bytes() ([]byte, error) {
	n, err := r.long()
	if err != nil {
		return nil, err
	}
	if n < 0 || n > maxInputBytes {
		return nil, oruby.ERangeError("Marshal length exceeds size limit")
	}
	data := make([]byte, n)
	if err := r.full(data); err != nil {
		return nil, err
	}
	return data, nil
}

// symbol is MRI's r_symbol: a Symbol or a link to one
func (r *reader) symbol() (oruby.MrbSym, error) {
	tag, err := r.byte1()
	if err != nil {
		return 0, err
	}
	ivar := false
	if tag == 'I' {
		ivar = true
		if tag, err = r.byte1(); err != nil {
			return 0, err
		}
	}
	switch tag {
	case ':':
		return r.symreal(ivar)
	case ';':
		if ivar {
			return 0, oruby.EArgumentError("dump format error (symlink with encoding)")
		}
		return r.symlink()
	}
	return 0, oruby.EArgumentError("dump format error for symbol(0x%x)", tag)
}

func (r *reader) symlink() (oruby.MrbSym, error) {
	id, err := r.long()
	if err != nil {
		return 0, err
	}
	if id < 0 || id >= int64(len(r.symbols)) {
		return 0, oruby.EArgumentError("bad symbol")
	}
	return r.symbols[id], nil
}

// symreal reads a Symbol's name, and its encoding when ivar: a name in
// another encoding than UTF-8 is another Symbol (String#__oruby_intern_enc),
// and one dumped without an encoding is BINARY unless it is ASCII
func (r *reader) symreal(ivar bool) (oruby.MrbSym, error) {
	name, err := r.bytes()
	if err != nil {
		return 0, err
	}
	idx := len(r.symbols)
	r.symbols = append(r.symbols, 0) // the ivars below may add symbols
	enc := ""
	if !ivar {
		for _, b := range name {
			if b >= 0x80 {
				enc = "ASCII-8BIT"
				break
			}
		}
	} else {
		n, err := r.count()
		if err != nil {
			return 0, err
		}
		for i := 0; i < n; i++ {
			key, err := r.symbol()
			if err != nil {
				return 0, err
			}
			v, err := r.object()
			if err != nil {
				return 0, err
			}
			switch r.mrb.SymName(key) {
			case "E":
			case "encoding":
				if v.Type() == oruby.MrbTTString {
					enc = r.mrb.String(v)
				}
			default:
				return 0, oruby.EArgumentError("invalid symbol ivar")
			}
		}
	}
	sym := r.mrb.Intern(string(name))
	if enc != "" {
		v, err := r.mrb.Funcall(r.mrb.StringValue(string(name)), r.mrb.Intern("__oruby_intern_enc"), enc)
		if err != nil {
			return 0, err
		}
		sym = v.Symbol()
	}
	r.symbols[idx] = sym
	return sym, nil
}

func (r *reader) entry(v oruby.Value) oruby.Value {
	r.objects = append(r.objects, v)
	if v.HasBasic() {
		r.partial[v.Pointer()] = true
	}
	return v
}

// prepare reserves an object-table slot for an object made later (r_prepare)
func (r *reader) prepare() int {
	r.objects = append(r.objects, oruby.Nil)
	return len(r.objects) - 1
}

func (r *reader) entry0(v oruby.Value, idx int) oruby.Value {
	r.objects[idx] = v
	if v.HasBasic() {
		r.partial[v.Pointer()] = true
	}
	return v
}

// leave is MRI's r_leave: a complete object is frozen for freeze: (Strings
// deduplicated, modules left alone, no freeze method called) and given to
// the load proc
func (r *reader) leave(v oruby.Value, partial bool) (oruby.Value, error) {
	if partial {
		return v, nil
	}
	if v.HasBasic() {
		delete(r.partial, v.Pointer())
	}
	if r.freeze {
		switch v.Type() {
		case oruby.MrbTTClass, oruby.MrbTTModule:
		case oruby.MrbTTString:
			// a plain String is deduplicated; one with instance variables
			// or of a subclass is the object itself, frozen
			if r.mrb.ClassName(r.mrb.ObjClass(v)) != "String" || r.hasIvars(v) {
				v.Freeze()
				break
			}
			d, err := r.mrb.Funcall(v, r.mrb.Intern("-@"))
			if err != nil {
				return v, err
			}
			v = d
		default:
			v.Freeze()
		}
	}
	return r.postProc(v)
}

func (r *reader) hasIvars(v oruby.Value) bool {
	found := false
	r.mrb.IVForEach(v, func(name oruby.MrbSym, _ oruby.Value) bool {
		found = !strings.HasPrefix(r.mrb.SymName(name), "__")
		return !found
	})
	return found
}

func (r *reader) postProc(v oruby.Value) (oruby.Value, error) {
	if r.proc.IsNil() {
		return v, nil
	}
	return r.mrb.Funcall(r.proc, oruby.Sym_call, v)
}

// object is MRI's r_object: a complete value
func (r *reader) object() (oruby.Value, error) {
	return r.object0(false, nil, nil)
}

// object0 is MRI's r_object0: ivp, when given, is set false by a value that
// takes the 'I' instance variables itself (a _dump String's, a Regexp's);
// extmod are the modules of an 'e' prefix still to be applied
func (r *reader) object0(partial bool, ivp *bool, extmod *[]oruby.Value) (oruby.Value, error) {
	r.nodes++
	if r.nodes > maxNodes {
		return oruby.Nil, oruby.ERangeError("Marshal object graph exceeds size limit")
	}
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > maxDepth {
		return oruby.Nil, oruby.EArgumentError("Marshal data nests too deep")
	}
	tag, err := r.byte1()
	if err != nil {
		return oruby.Nil, err
	}
	return r.objectFor(partial, ivp, extmod, tag)
}

func (r *reader) objectFor(partial bool, ivp *bool, extmod *[]oruby.Value, tag byte) (oruby.Value, error) {
	mrb := r.mrb
	switch tag {
	case '@':
		id, err := r.long()
		if err != nil {
			return oruby.Nil, err
		}
		if id < 0 || id >= int64(len(r.objects)) {
			return oruby.Nil, oruby.EArgumentError("dump format error (unlinked)")
		}
		// a complete object goes to the proc again; one still being read, or
		// one _load / marshal_load made (MRI never takes those off the
		// partial list), does not
		v := r.objects[id]
		if v.HasBasic() && r.partial[v.Pointer()] {
			return v, nil
		}
		return r.postProc(v)

	case 'I':
		ivar := true
		v, err := r.object0(true, &ivar, extmod)
		if err != nil {
			return oruby.Nil, err
		}
		if ivar {
			if err := r.ivars(v); err != nil {
				return oruby.Nil, err
			}
		}
		return r.leave(v, partial)

	case 'e':
		path, err := r.symbol()
		if err != nil {
			return oruby.Nil, err
		}
		m, err := r.pathToClass(mrb.SymName(path))
		if err != nil {
			return oruby.Nil, err
		}
		if m.Type() != oruby.MrbTTModule {
			return oruby.Nil, oruby.EArgumentError("%s does not refer to module", mrb.SymName(path))
		}
		mods := []oruby.Value{m}
		if extmod != nil {
			mods = append(*extmod, m)
		}
		v, err := r.object0(true, nil, &mods)
		if err != nil {
			return oruby.Nil, err
		}
		for i := len(mods) - 1; i >= 0; i-- {
			if _, err := mrb.Funcall(v, oruby.Sym_extend, mods[i]); err != nil {
				return oruby.Nil, err
			}
		}
		if extmod != nil {
			*extmod = (*extmod)[:0]
		}
		return r.leave(v, partial)

	case 'C':
		return r.uclass(partial, extmod)

	case '0':
		return r.leave(oruby.Nil, false)
	case 'T':
		return r.leave(oruby.True, false)
	case 'F':
		return r.leave(oruby.False, false)
	case 'i':
		n, err := r.long()
		if err != nil {
			return oruby.Nil, err
		}
		return r.leave(mrb.Value(n), false)

	case 'f':
		data, err := r.bytes()
		if err != nil {
			return oruby.Nil, err
		}
		f, err := parseFloat(data)
		if err != nil {
			return oruby.Nil, err
		}
		return r.leave(r.entry(mrb.FloatValue(f)), false)

	case 'l':
		z, err := r.bignum()
		if err != nil {
			return oruby.Nil, err
		}
		return r.leave(r.entry(mrb.BigIntValue(z)), false)

	case '"':
		data, err := r.bytes()
		if err != nil {
			return oruby.Nil, err
		}
		s := mrb.BytesValue(data)
		if _, err := mrb.Funcall(s, oruby.Sym_force_encoding, "ASCII-8BIT"); err != nil {
			return oruby.Nil, err
		}
		return r.leave(r.entry(s), partial)

	case '/':
		return r.regexp(partial, ivp, oruby.Nil)

	case '[':
		n, err := r.count()
		if err != nil {
			return oruby.Nil, err
		}
		a := mrb.AryNewCapa(n)
		r.entry(a.Value())
		for i := 0; i < n; i++ {
			item, err := r.object()
			if err != nil {
				return oruby.Nil, err
			}
			a.Push(item)
		}
		return r.leave(a.Value(), partial)

	case '{', '}':
		return r.hash(partial, tag == '}', false)

	case ':':
		var sym oruby.MrbSym
		var err error
		if ivp != nil {
			sym, err = r.symreal(*ivp)
			*ivp = false
		} else {
			sym, err = r.symreal(false)
		}
		if err != nil {
			return oruby.Nil, err
		}
		return r.leave(mrb.SymbolValue(sym), partial)
	case ';':
		sym, err := r.symlink()
		if err != nil {
			return oruby.Nil, err
		}
		return mrb.SymbolValue(sym), nil

	case 'S':
		return r.structure(partial)
	case 'u':
		return r.userLoad(partial, ivp)
	case 'U':
		return r.marshalLoad(extmod)
	case 'o':
		return r.objectRecord(partial)
	case 'd':
		return r.dataRecord(partial, extmod)

	case 'c', 'm', 'M':
		data, err := r.bytes()
		if err != nil {
			return oruby.Nil, err
		}
		name := string(data)
		v, err := r.pathToClass(name)
		if err != nil {
			return oruby.Nil, err
		}
		switch {
		case tag == 'c' && v.Type() != oruby.MrbTTClass:
			return oruby.Nil, oruby.EArgumentError("%s does not refer to class", name)
		case tag == 'm' && v.Type() != oruby.MrbTTModule:
			return oruby.Nil, oruby.EArgumentError("%s does not refer to module", name)
		}
		return r.leave(r.entry(v), partial)
	}
	return oruby.Nil, oruby.EArgumentError("dump format error(0x%x)", tag)
}

// ivars is MRI's r_ivar: E and encoding set the encoding, the others are
// instance variables
func (r *reader) ivars(v oruby.Value) error {
	n, err := r.count()
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		key, err := r.symbol()
		if err != nil {
			return err
		}
		val, err := r.object()
		if err != nil {
			return err
		}
		if enc, ok := r.encodingOf(key, val); ok {
			if v.Type() == oruby.MrbTTString || r.mrb.RespondTo(v, oruby.Sym_force_encoding) {
				if _, err := r.mrb.Funcall(v, oruby.Sym_force_encoding, enc); err != nil {
					return err
				}
			}
			continue
		}
		if err := r.setIvar(v, key, val); err != nil {
			return err
		}
	}
	return nil
}

func (r *reader) setIvar(v oruby.Value, key oruby.MrbSym, val oruby.Value) error {
	if v.Type() == oruby.MrbTTModule {
		return oruby.EArgumentError("module %s can't have instance variables", r.mrb.String(r.mrb.Inspect(v)))
	}
	if v.Frozen() {
		return oruby.EFrozenError("can't modify frozen %s: %s", r.mrb.ClassName(r.mrb.ObjClass(v)), r.mrb.String(r.mrb.Inspect(v)))
	}
	return r.mrb.IVSet(v, key, val)
}

// encodingOf: the encoding an E / encoding instance variable names
func (r *reader) encodingOf(key oruby.MrbSym, val oruby.Value) (string, bool) {
	switch r.mrb.SymName(key) {
	case "E":
		if val.Type() == oruby.MrbTTTrue {
			return "UTF-8", true
		}
		return "US-ASCII", true
	case "encoding":
		return r.mrb.String(val), true
	}
	return "", false
}

func (r *reader) bignum() (*big.Int, error) {
	sign, err := r.byte1()
	if err != nil {
		return nil, err
	}
	if sign != '+' && sign != '-' {
		return nil, oruby.EArgumentError("dump format error (bignum sign)")
	}
	words, err := r.long()
	if err != nil {
		return nil, err
	}
	if words < 0 || words > maxInputBytes/2 {
		return nil, oruby.ERangeError("Marshal bignum exceeds size limit")
	}
	data := make([]byte, words*2)
	if err := r.full(data); err != nil {
		return nil, err
	}
	for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
		data[i], data[j] = data[j], data[i]
	}
	z := new(big.Int).SetBytes(data)
	if sign == '-' {
		z.Neg(z)
	}
	return z, nil
}

// parseFloat: MRI's r_object TYPE_FLOAT; old dumps add the mantissa bytes
// after a NUL, which are ignored
func parseFloat(data []byte) (float64, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	switch string(data) {
	case "nan":
		return math.NaN(), nil
	case "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	}
	f, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
			return f, nil
		}
		return 0, oruby.EArgumentError("invalid value for Float(): %q", data)
	}
	return f, nil
}

func (r *reader) hash(partial, hasDefault, identity bool) (oruby.Value, error) {
	n, err := r.count()
	if err != nil {
		return oruby.Nil, err
	}
	h := r.mrb.HashNewCapa(n)
	if identity {
		if _, err := r.mrb.Funcall(h, r.mrb.Intern("compare_by_identity")); err != nil {
			return oruby.Nil, err
		}
	}
	r.entry(h.Value())
	for i := 0; i < n; i++ {
		key, err := r.object()
		if err != nil {
			return oruby.Nil, err
		}
		val, err := r.object()
		if err != nil {
			return oruby.Nil, err
		}
		if _, err := r.mrb.Funcall(h, oruby.OpSym_aset, key, val); err != nil {
			return oruby.Nil, err
		}
	}
	if hasDefault {
		def, err := r.object()
		if err != nil {
			return oruby.Nil, err
		}
		if _, err := r.mrb.Funcall(h, oruby.SymE_default, def); err != nil {
			return oruby.Nil, err
		}
	}
	return r.leave(h.Value(), partial)
}

// uclass is MRI's TYPE_UCLASS: a core object (String, Array, Hash, Regexp)
// made an instance of a user subclass in place
func (r *reader) uclass(partial bool, extmod *[]oruby.Value) (oruby.Value, error) {
	mrb := r.mrb
	path, err := r.symbol()
	if err != nil {
		return oruby.Nil, err
	}
	c, err := r.pathToClass(mrb.SymName(path))
	if err != nil {
		return oruby.Nil, err
	}
	if c.Type() != oruby.MrbTTClass {
		return oruby.Nil, oruby.EArgumentError("%s does not refer to class", mrb.SymName(path))
	}
	tag, err := r.byte1()
	if err != nil {
		return oruby.Nil, err
	}
	if mrb.ClassName(mrb.ClassPtr(c)) == "Hash" && (tag == '{' || tag == '}') {
		return r.hash(partial, tag == '}', true)
	}
	if tag == '/' {
		// a Regexp subclass: made as one, not frozen like a Regexp is
		return r.regexp(partial, nil, c)
	}
	v, err := r.objectFor(partial, nil, extmod, tag)
	if err != nil {
		return oruby.Nil, err
	}
	switch v.Type() {
	case oruby.MrbTTString, oruby.MrbTTArray, oruby.MrbTTHash, oruby.MrbTTCData:
	default:
		return oruby.Nil, oruby.EArgumentError("dump format error (user class)")
	}
	klass := mrb.ClassPtr(c)
	inherits, err := mrb.Funcall(c, oruby.OpSym_le, mrb.ObjClass(v))
	if err != nil || !inherits.Bool() {
		return oruby.Nil, oruby.EArgumentError("dump format error (user class)")
	}
	mrb.ObjSetClass(v, klass)
	return v, nil
}

// regexp: '/' source options; the encoding instance variables go to the
// source. A Regexp is frozen (MRI 4.1), so other instance variables raise
func (r *reader) regexp(partial bool, ivp *bool, subclass oruby.Value) (oruby.Value, error) {
	mrb := r.mrb
	src, err := r.bytes()
	if err != nil {
		return oruby.Nil, err
	}
	options, err := r.byte1()
	if err != nil {
		return oruby.Nil, err
	}
	idx := r.prepare()
	source := mrb.BytesValue(src)
	var extra []ivar
	if ivp != nil && *ivp {
		*ivp = false
		n, err := r.count()
		if err != nil {
			return oruby.Nil, err
		}
		for i := 0; i < n; i++ {
			key, err := r.symbol()
			if err != nil {
				return oruby.Nil, err
			}
			val, err := r.object()
			if err != nil {
				return oruby.Nil, err
			}
			if enc, ok := r.encodingOf(key, val); ok {
				if _, err := mrb.Funcall(source, oruby.Sym_force_encoding, enc); err != nil {
					return oruby.Nil, err
				}
				continue
			}
			extra = append(extra, ivar{key, val})
		}
	}
	opts := int(options) & 0x0f
	var v oruby.Value
	if subclass.IsNil() {
		v, err = mrb.Funcall(mrb.ClassGet("Regexp"), oruby.Sym_new, source, opts)
		if err == nil {
			v.Freeze()
		}
	} else {
		v, err = mrb.Funcall(mrb.ModuleGet("Marshal"), mrb.Intern("__oruby_new_regexp"), subclass, source, opts)
	}
	if err != nil {
		return oruby.Nil, err
	}
	r.entry0(v, idx)
	for _, iv := range extra {
		if err := r.setIvar(v, iv.name, iv.val); err != nil {
			return oruby.Nil, err
		}
	}
	return r.leave(v, partial)
}

// classOf reads a class name and finds the class
func (r *reader) classOf() (oruby.Value, string, error) {
	path, err := r.symbol()
	if err != nil {
		return oruby.Nil, "", err
	}
	name := r.mrb.SymName(path)
	c, err := r.pathToClass(name)
	if err != nil {
		return oruby.Nil, name, err
	}
	if c.Type() != oruby.MrbTTClass {
		return oruby.Nil, name, oruby.EArgumentError("%s does not refer to class", name)
	}
	return c, name, nil
}

// structure: 'S', a Struct or a Data, filled without its initialize (MRI's
// rb_struct_initialize)
func (r *reader) structure(partial bool) (oruby.Value, error) {
	mrb := r.mrb
	idx := r.prepare()
	c, name, err := r.classOf()
	if err != nil {
		return oruby.Nil, err
	}
	n, err := r.count()
	if err != nil {
		return oruby.Nil, err
	}
	v, err := mrb.Funcall(c, oruby.Sym_allocate)
	if err != nil {
		return oruby.Nil, err
	}
	if v.Type() != oruby.MrbTTStruct {
		return oruby.Nil, oruby.ETypeError("class %s not a struct", name)
	}
	members, err := mrb.Funcall(c, oruby.Sym_members)
	if err != nil {
		return oruby.Nil, err
	}
	names := append([]oruby.Value(nil), oruby.RArraySlice(members)...)
	if len(names) != n {
		return oruby.Nil, oruby.ETypeError("struct %s not compatible (struct size differs)", name)
	}
	r.entry0(v, idx)
	values := mrb.AryNewCapa(n)
	for i := 0; i < n; i++ {
		slot, err := r.symbol()
		if err != nil {
			return oruby.Nil, err
		}
		if slot != names[i].Symbol() {
			return oruby.Nil, oruby.ETypeError("struct %s not compatible (:%s for :%s)", name, mrb.SymName(slot), mrb.SymName(names[i].Symbol()))
		}
		val, err := r.object()
		if err != nil {
			return oruby.Nil, err
		}
		values.Push(val)
	}
	if _, err := mrb.Funcall(mrb.ModuleGet("Marshal"), mrb.Intern("__oruby_init_struct"), v, values); err != nil {
		return oruby.Nil, err
	}
	return r.leave(v, partial)
}

// userLoad: 'u' class bytes, given to class._load; the 'I' instance
// variables belong to the bytes
func (r *reader) userLoad(partial bool, ivp *bool) (oruby.Value, error) {
	mrb := r.mrb
	path, err := r.symbol()
	if err != nil {
		return oruby.Nil, err
	}
	name := mrb.SymName(path)
	c, err := r.pathToClass(name)
	if err != nil {
		return oruby.Nil, err
	}
	if !mrb.RespondTo(c, oruby.Sym__load) {
		return oruby.Nil, oruby.ETypeError("class %s needs to have method '_load'", name)
	}
	raw, err := r.bytes()
	if err != nil {
		return oruby.Nil, err
	}
	data := mrb.BytesValue(raw)
	if _, err := mrb.Funcall(data, oruby.Sym_force_encoding, "ASCII-8BIT"); err != nil {
		return oruby.Nil, err
	}
	if ivp != nil && *ivp {
		if err := r.ivars(data); err != nil {
			return oruby.Nil, err
		}
		*ivp = false
	}
	v, err := mrb.Funcall(c, oruby.Sym__load, data)
	if err != nil {
		return oruby.Nil, err
	}
	r.entry(v) // stays partial, as in MRI: links to it skip the proc
	if partial {
		return v, nil
	}
	if r.freeze {
		v.Freeze()
	}
	return r.postProc(v)
}

// marshalLoad: 'U' class payload: allocate, extend, marshal_load(payload)
func (r *reader) marshalLoad(extmod *[]oruby.Value) (oruby.Value, error) {
	mrb := r.mrb
	c, name, err := r.classOf()
	if err != nil {
		return oruby.Nil, err
	}
	if name == "Rational" || name == "Complex" {
		return r.numeric(name)
	}
	v, err := mrb.Funcall(c, oruby.Sym_allocate)
	if err != nil {
		return oruby.Nil, err
	}
	if extmod != nil {
		for i := len(*extmod) - 1; i >= 0; i-- {
			if _, err := mrb.Funcall(v, oruby.Sym_extend, (*extmod)[i]); err != nil {
				return oruby.Nil, err
			}
		}
		*extmod = (*extmod)[:0]
	}
	if !mrb.RespondTo(v, oruby.Sym_marshal_load) {
		return oruby.Nil, oruby.ETypeError("instance of %s needs to have method 'marshal_load'", name)
	}
	r.entry(v)
	data, err := r.object()
	if err != nil {
		return oruby.Nil, err
	}
	if _, err := mrb.Funcall(v, oruby.Sym_marshal_load, data); err != nil {
		return oruby.Nil, err
	}
	if r.freeze {
		v.Freeze()
	}
	return r.postProc(v)
}

// numeric: a Rational or Complex from its [a, b] marshal_dump
func (r *reader) numeric(name string) (oruby.Value, error) {
	idx := r.prepare()
	data, err := r.object()
	if err != nil {
		return oruby.Nil, err
	}
	if data.Type() != oruby.MrbTTArray || oruby.RArrayLen(data) != 2 {
		return oruby.Nil, oruby.EArgumentError("marshaled %s must have an array whose length is 2 but %s",
			strings.ToLower(name), r.mrb.String(r.mrb.Inspect(data)))
	}
	pair := oruby.RArraySlice(data)
	v, err := r.mrb.Funcall(r.mrb.KernelModule(), r.mrb.Intern(name), pair[0], pair[1])
	if err != nil {
		return oruby.Nil, err
	}
	r.objects[idx] = v
	return r.postProc(v)
}

// objectRecord: 'o' class ivars; Exception and Range are records too
func (r *reader) objectRecord(partial bool) (oruby.Value, error) {
	mrb := r.mrb
	idx := r.prepare()
	c, _, err := r.classOf()
	if err != nil {
		return oruby.Nil, err
	}
	if isSubclass(mrb, c, "Range") {
		return r.rangeRecord(c, idx, partial)
	}
	v, err := mrb.Funcall(c, oruby.Sym_allocate)
	if err != nil {
		return oruby.Nil, err
	}
	switch v.Type() {
	case oruby.MrbTTObject, oruby.MrbTTException:
	default:
		return oruby.Nil, oruby.EArgumentError("dump format error")
	}
	r.entry0(v, idx)
	n, err := r.count()
	if err != nil {
		return oruby.Nil, err
	}
	for i := 0; i < n; i++ {
		key, err := r.symbol()
		if err != nil {
			return oruby.Nil, err
		}
		val, err := r.object()
		if err != nil {
			return oruby.Nil, err
		}
		if v.Type() == oruby.MrbTTException {
			switch mrb.SymName(key) {
			case "mesg":
				if err := mrb.SetExcMessage(v, val); err != nil {
					return oruby.Nil, err
				}
				if val.IsNil() {
					// core_ext's mark of an exception without a message
					_ = mrb.IVSet(v, mrb.Intern("@__nil_mesg"), oruby.True)
				}
				continue
			case "bt":
				if err := mrb.SetExcBacktrace(v, val); err != nil {
					return oruby.Nil, err
				}
				continue
			}
		}
		if err := r.setIvar(v, key, val); err != nil {
			return oruby.Nil, err
		}
	}
	return r.leave(v, partial)
}

func isSubclass(mrb *oruby.MrbState, c oruby.Value, base string) bool {
	if !mrb.ClassDefined(base) {
		return false
	}
	le, err := mrb.Funcall(c, oruby.OpSym_le, mrb.ClassGet(base))
	return err == nil && le.Bool()
}

func (r *reader) rangeRecord(c oruby.Value, idx int, partial bool) (oruby.Value, error) {
	mrb := r.mrb
	// made first, so an endpoint that refers to the Range links to it
	v, err := mrb.Funcall(c, oruby.Sym_allocate)
	if err != nil {
		return oruby.Nil, err
	}
	r.entry0(v, idx)
	n, err := r.count()
	if err != nil {
		return oruby.Nil, err
	}
	var begin, end oruby.Value
	excl := false
	for i := 0; i < n; i++ {
		key, err := r.symbol()
		if err != nil {
			return oruby.Nil, err
		}
		val, err := r.object()
		if err != nil {
			return oruby.Nil, err
		}
		switch mrb.SymName(key) {
		case "begin":
			begin = val
		case "end":
			end = val
		case "excl":
			excl = val.Bool()
		}
	}
	if _, err := mrb.Funcall(v, oruby.Sym_initialize, begin, end, excl); err != nil {
		return oruby.Nil, err
	}
	return r.leave(v, partial)
}

// dataRecord: 'd' class data, given to _load_data
func (r *reader) dataRecord(partial bool, extmod *[]oruby.Value) (oruby.Value, error) {
	mrb := r.mrb
	c, name, err := r.classOf()
	if err != nil {
		return oruby.Nil, err
	}
	v, err := mrb.Funcall(c, oruby.Sym_allocate)
	if err != nil {
		return oruby.Nil, err
	}
	if v.Type() != oruby.MrbTTCData {
		return oruby.Nil, oruby.EArgumentError("dump format error")
	}
	r.entry(v)
	if !mrb.RespondTo(v, mrb.Intern("_load_data")) {
		return oruby.Nil, oruby.ETypeError("class %s needs to have instance method '_load_data'", name)
	}
	data, err := r.object0(partial, nil, extmod)
	if err != nil {
		return oruby.Nil, err
	}
	if _, err := mrb.Funcall(v, mrb.Intern("_load_data"), data); err != nil {
		return oruby.Nil, err
	}
	return r.leave(v, partial)
}

// pathToClass is MRI's rb_path_to_class
func (r *reader) pathToClass(name string) (oruby.Value, error) {
	current := r.mrb.ObjectClass()
	var v oruby.Value
	for _, part := range strings.Split(name, "::") {
		if part == "" {
			return oruby.Nil, oruby.EArgumentError("undefined class/module %s", name)
		}
		sym := r.mrb.Intern(part)
		if !r.mrb.ConstDefined(current, sym) {
			return oruby.Nil, oruby.EArgumentError("undefined class/module %s", name)
		}
		v = r.mrb.ConstGet(current, sym)
		if v.Type() != oruby.MrbTTClass && v.Type() != oruby.MrbTTModule {
			return oruby.Nil, oruby.ETypeError("%s does not refer to class/module", name)
		}
		current = r.mrb.ClassPtr(v)
	}
	return v, nil
}
