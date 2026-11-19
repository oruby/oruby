package marshal

import (
	"bytes"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unsafe"

	"github.com/oruby/oruby"
	rx "github.com/oruby/oruby/gem/regexp"
)

const (
	maxInputBytes = 64 << 20
	maxNodes      = 1 << 20
	maxDepth      = 10000 // load nesting; dump follows the limit argument
)

// writer follows MRI's marshal.c (w_object & co.): the object table counts
// every object written that is not an immediate (floats and bignums too),
// so the links ('@' n) a load resolves name the same objects.
type writer struct {
	mrb       *oruby.MrbState
	buf       bytes.Buffer
	objects   map[uintptr]int // object -> index in the object table
	count     int             // objects in the table
	symbols   map[oruby.MrbSym]int
	nodes     int
	respondTo map[uintptr]bool // class -> has its own respond_to?
}

// dumpValue dumps value; a negative limit is no depth limit (MRI's -1)
func dumpValue(mrb *oruby.MrbState, value oruby.Value, limit int) ([]byte, error) {
	if !mrb.GCDisabled() {
		mrb.GCDisable()
		defer mrb.GCEnable()
	}
	w := &writer{
		mrb:       mrb,
		objects:   make(map[uintptr]int),
		symbols:   make(map[oruby.MrbSym]int),
		respondTo: make(map[uintptr]bool),
	}
	w.buf.Write([]byte{4, 8})
	if err := w.object(value, limit); err != nil {
		return nil, err
	}
	return append([]byte(nil), w.buf.Bytes()...), nil
}

func (w *writer) byte1(b byte) error {
	if w.buf.Len() >= maxInputBytes {
		return oruby.ERangeError("Marshal output exceeds size limit")
	}
	return w.buf.WriteByte(b)
}

func (w *writer) raw(b []byte) error {
	if len(b) > maxInputBytes-w.buf.Len() {
		return oruby.ERangeError("Marshal output exceeds size limit")
	}
	_, err := w.buf.Write(b)
	return err
}

// long is MRI's w_long
func (w *writer) long(n int64) error {
	switch {
	case n == 0:
		return w.byte1(0)
	case 0 < n && n < 123:
		return w.byte1(byte(n + 5))
	case -124 < n && n < 0:
		return w.byte1(byte(int8(n - 5)))
	}
	var data [8]byte
	i := 0
	for x := n; i < 8; i++ {
		data[i] = byte(x)
		x >>= 8
		if x == 0 || x == -1 {
			i++
			break
		}
	}
	length := i
	if n < 0 {
		length = -length
	}
	if err := w.byte1(byte(int8(length))); err != nil {
		return err
	}
	return w.raw(data[:i])
}

// bytes is MRI's w_bytes: a length and the bytes
func (w *writer) bytes(data []byte) error {
	if err := w.long(int64(len(data))); err != nil {
		return err
	}
	return w.raw(data)
}

func (w *writer) remember(v oruby.Value) {
	if v.HasBasic() {
		w.objects[v.Pointer()] = w.count
	}
	w.count++
}

// symbol is MRI's w_symbol: a symbol with a non-ASCII name carries its
// encoding as an instance variable
func (w *writer) symbol(sym oruby.MrbSym) error {
	if id, ok := w.symbols[sym]; ok {
		if err := w.byte1(';'); err != nil {
			return err
		}
		return w.long(int64(id))
	}
	w.symbols[sym] = len(w.symbols)
	name := w.mrb.SymName(sym)
	enc := ""
	if !isASCII(name) {
		enc = w.encodingName(w.mrb.SymbolValue(sym))
	}
	hasEnc := enc != "" && !isBinary(enc)
	if hasEnc {
		if err := w.byte1('I'); err != nil {
			return err
		}
	}
	if err := w.byte1(':'); err != nil {
		return err
	}
	if err := w.bytes([]byte(name)); err != nil {
		return err
	}
	if hasEnc {
		if err := w.long(1); err != nil {
			return err
		}
		return w.encoding(enc)
	}
	return nil
}

// encoding writes the E / encoding instance variable (MRI's w_encoding)
func (w *writer) encoding(enc string) error {
	switch {
	case strings.EqualFold(enc, "UTF-8"):
		if err := w.symbol(w.mrb.Intern("E")); err != nil {
			return err
		}
		return w.byte1('T')
	case strings.EqualFold(enc, "US-ASCII"):
		if err := w.symbol(w.mrb.Intern("E")); err != nil {
			return err
		}
		return w.byte1('F')
	}
	if err := w.symbol(w.mrb.Intern("encoding")); err != nil {
		return err
	}
	// the name is a String object of its own, as MRI writes it
	w.count++
	if err := w.byte1('"'); err != nil {
		return err
	}
	return w.bytes([]byte(enc))
}

type ivar struct {
	name oruby.MrbSym
	val  oruby.Value
}

// ivars lists the instance variables Marshal keeps: the @names. A String
// also keeps names without @, which Go code sets (Time#_dump's offset and
// zone); the runtime's own (__name, a Hash's ifnone) are not dumped.
func (w *writer) ivars(v oruby.Value) []ivar {
	var list, plain []ivar
	str := v.Type() == oruby.MrbTTString
	w.mrb.IVForEach(v, func(name oruby.MrbSym, val oruby.Value) bool {
		n := w.mrb.SymName(name)
		if n == "@__nil_mesg" {
			// core_ext's mark of an exception made without a message
		} else if strings.HasPrefix(n, "@") {
			list = append(list, ivar{name, val})
		} else if str && !strings.HasPrefix(n, "__") {
			plain = append(plain, ivar{name, val})
		}
		return true
	})
	// the @names first: MRI's Time#_dump copies the Time's own before it
	// adds offset and zone
	return append(list, plain...)
}

// writeIvars is MRI's w_ivar: the count, the encoding first, the variables
func (w *writer) writeIvars(enc string, list []ivar, limit int) error {
	n := len(list)
	if enc != "" {
		n++
	}
	if err := w.long(int64(n)); err != nil {
		return err
	}
	if enc != "" {
		if err := w.encoding(enc); err != nil {
			return err
		}
	}
	for _, iv := range list {
		if err := w.symbol(iv.name); err != nil {
			return err
		}
		if err := w.object(iv.val, limit); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) object(v oruby.Value, limit int) error {
	if limit == 0 {
		return oruby.EArgumentError("exceed depth limit")
	}
	w.nodes++
	if w.nodes > maxNodes {
		return oruby.ERangeError("Marshal object graph exceeds size limit")
	}
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return w.byte1('0')
		}
		return w.byte1('F')
	case oruby.MrbTTTrue:
		return w.byte1('T')
	case oruby.MrbTTInteger:
		n := v.Int64()
		if n >= -(1<<30) && n < 1<<30 {
			if err := w.byte1('i'); err != nil {
				return err
			}
			return w.long(n)
		}
		// MRI writes it as a Bignum object, which takes a table slot
		w.count++
		return w.bignum(big.NewInt(n))
	case oruby.MrbTTSymbol:
		return w.symbol(v.Symbol())
	case oruby.MrbTTFloat:
		w.count++
		if err := w.byte1('f'); err != nil {
			return err
		}
		return w.bytes([]byte(floatText(oruby.MrbFloat(v))))
	}
	if id, ok := w.objects[v.Pointer()]; ok {
		if err := w.byte1('@'); err != nil {
			return err
		}
		return w.long(int64(id))
	}
	if limit > 0 {
		limit--
	}

	switch w.className(v) {
	case "Rational", "Complex":
		// MRI dumps them through marshal_dump: [numerator, denominator] and
		// [real, imaginary]
		return w.numeric(v, limit)
	}
	if w.responds(v, oruby.Sym_marshal_dump) {
		w.remember(v)
		payload, err := w.mrb.Funcall(v, oruby.Sym_marshal_dump)
		if err != nil {
			return err
		}
		if err := w.class('U', v, false); err != nil {
			return err
		}
		return w.object(payload, limit)
	}
	if w.responds(v, oruby.Sym__dump) {
		return w.userDump(v, limit)
	}
	if name := w.undumpable(v); name != "" {
		return oruby.ETypeError("no _dump_data is defined for class %s", name)
	}

	w.remember(v)
	enc, list := "", []ivar(nil)
	switch v.Type() {
	case oruby.MrbTTObject, oruby.MrbTTException, oruby.MrbTTRange:
		// their instance variables are part of the 'o' record
	case oruby.MrbTTClass, oruby.MrbTTModule:
		if name, err := w.classPath(w.mrb.ClassPtr(v)); err == nil && !isASCII(name) {
			enc = "UTF-8"
		}
	case oruby.MrbTTString:
		enc = w.encodingName(v)
		list = w.ivars(v)
	default:
		list = w.ivars(v)
		if w.isRegexp(v) {
			enc = w.encodingName(v)
			// the regexp gem keeps the source and options as @source and
			// @options; they are the record itself
			kept := list[:0]
			for _, iv := range list {
				if n := w.mrb.SymName(iv.name); n != "@source" && n != "@options" {
					kept = append(kept, iv)
				}
			}
			list = kept
		}
	}
	if isBinary(enc) {
		enc = ""
	}
	hasIvars := enc != "" || len(list) > 0
	if hasIvars {
		if err := w.byte1('I'); err != nil {
			return err
		}
	}
	if err := w.body(v, limit); err != nil {
		return err
	}
	if hasIvars {
		return w.writeIvars(enc, list, limit)
	}
	return nil
}

// body writes v's own record, after its 'I' and before its instance
// variables
func (w *writer) body(v oruby.Value, limit int) error {
	switch v.Type() {
	case oruby.MrbTTSClass:
		return oruby.ETypeError("singleton class can't be dumped")
	case oruby.MrbTTClass, oruby.MrbTTModule:
		c := w.mrb.ClassPtr(v)
		name, err := w.classPath(c)
		if err != nil {
			return err
		}
		tag := byte('c')
		if v.Type() == oruby.MrbTTModule {
			tag = 'm'
		}
		if err := w.byte1(tag); err != nil {
			return err
		}
		return w.bytes([]byte(name))
	case oruby.MrbTTString:
		if err := w.uclass(v, "String"); err != nil {
			return err
		}
		if err := w.byte1('"'); err != nil {
			return err
		}
		return w.bytes(rubyBytes(v))
	case oruby.MrbTTArray:
		if err := w.uclass(v, "Array"); err != nil {
			return err
		}
		items := oruby.RArraySlice(v)
		if err := w.byte1('['); err != nil {
			return err
		}
		if err := w.long(int64(len(items))); err != nil {
			return err
		}
		for _, item := range append([]oruby.Value(nil), items...) {
			if err := w.object(item, limit); err != nil {
				return err
			}
		}
		return nil
	case oruby.MrbTTHash:
		return w.hash(v, limit)
	case oruby.MrbTTStruct:
		return w.structure(v, limit)
	case oruby.MrbTTRange:
		return w.rangeObject(v, limit)
	case oruby.MrbTTException:
		return w.exception(v, limit)
	case oruby.MrbTTObject:
		if err := w.class('o', v, true); err != nil {
			return err
		}
		return w.writeIvarsBare(w.ivars(v), limit)
	case oruby.MrbTTBigInt:
		z, err := w.mrb.BigInt(v)
		if err != nil {
			return err
		}
		return w.bignum(z)
	}
	if w.isRegexp(v) {
		return w.regexp(v)
	}
	if v.Type() == oruby.MrbTTCData || v.Type() == oruby.MrbTTIStruct {
		return oruby.ETypeError("no _dump_data is defined for class %s", w.className(v))
	}
	return oruby.ETypeError("can't dump %s", w.className(v))
}

// writeIvarsBare: the count and the pairs, as in an 'o' record
func (w *writer) writeIvarsBare(list []ivar, limit int) error {
	if err := w.long(int64(len(list))); err != nil {
		return err
	}
	for _, iv := range list {
		if err := w.symbol(iv.name); err != nil {
			return err
		}
		if err := w.object(iv.val, limit); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) bignum(z *big.Int) error {
	if z.BitLen() > maxInputBytes*8 {
		return oruby.ERangeError("Marshal bignum exceeds size limit")
	}
	magnitude := new(big.Int).Abs(z).Bytes()
	words := (len(magnitude) + 1) / 2
	sign := byte('+')
	if z.Sign() < 0 {
		sign = '-'
	}
	if err := w.raw([]byte{'l', sign}); err != nil {
		return err
	}
	if err := w.long(int64(words)); err != nil {
		return err
	}
	data := make([]byte, words*2)
	for i := range data {
		if j := len(magnitude) - i - 1; j >= 0 {
			data[i] = magnitude[j]
		}
	}
	return w.raw(data)
}

// floatText is MRI's w_float: the shortest digits that read back the same
func floatText(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case f == 0:
		if math.Signbit(f) {
			return "-0"
		}
		return "0"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // [-]d[.ddd]e±xx
	sign := ""
	if e[0] == '-' {
		sign, e = "-", e[1:]
	}
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	decpt := x + 1 // digits before the point
	n := len(digits)
	switch {
	case decpt < -3 || decpt > n:
		s := digits[:1]
		if n > 1 {
			s += "." + digits[1:]
		}
		return sign + s + "e" + strconv.Itoa(decpt-1)
	case decpt > 0:
		if n > decpt {
			return sign + digits[:decpt] + "." + digits[decpt:]
		}
		return sign + digits
	default:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	}
}

func (w *writer) hash(v oruby.Value, limit int) error {
	if err := w.uclass(v, "Hash"); err != nil {
		return err
	}
	if cbi, err := w.mrb.Funcall(v, w.mrb.Intern("compare_by_identity?")); err == nil && cbi.Bool() {
		if err := w.byte1('C'); err != nil {
			return err
		}
		if err := w.symbol(w.mrb.Intern("Hash")); err != nil {
			return err
		}
	}
	if oruby.MrbRHashProcDefaultP(v) {
		return oruby.ETypeError("can't dump hash with default proc")
	}
	entries := append([][2]oruby.Value(nil), oruby.RHashEntries(v)...)
	live := entries[:0]
	for _, e := range entries {
		if e[0].Type() != oruby.MrbTTUndef {
			live = append(live, e)
		}
	}
	def := w.mrb.RHashIfNone(v)
	tag := byte('{')
	if !def.IsNil() {
		tag = '}'
	}
	if err := w.byte1(tag); err != nil {
		return err
	}
	if err := w.long(int64(len(live))); err != nil {
		return err
	}
	for _, e := range live {
		if err := w.object(e[0], limit); err != nil {
			return err
		}
		if err := w.object(e[1], limit); err != nil {
			return err
		}
	}
	if tag == '}' {
		return w.object(def, limit)
	}
	return nil
}

// structure writes a Struct or a Data (both T_STRUCT in MRI) from its
// slots, whatever its accessors do
func (w *writer) structure(v oruby.Value, limit int) error {
	if err := w.class('S', v, true); err != nil {
		return err
	}
	members, err := w.mrb.Funcall(w.mrb.ObjClass(v), oruby.Sym_members)
	if err != nil {
		return err
	}
	names := append([]oruby.Value(nil), oruby.RArraySlice(members)...)
	values := append([]oruby.Value(nil), oruby.RArraySlice(v)...)
	if err := w.long(int64(len(names))); err != nil {
		return err
	}
	for i, name := range names {
		if name.Type() != oruby.MrbTTSymbol {
			return oruby.ETypeError("struct member is not a Symbol")
		}
		if err := w.symbol(name.Symbol()); err != nil {
			return err
		}
		item := oruby.Nil
		if i < len(values) {
			item = values[i]
		}
		if err := w.object(item, limit); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) rangeObject(v oruby.Value, limit int) error {
	r, err := w.mrb.MrbRangePtr(v)
	if err != nil {
		return err
	}
	if err := w.class('o', v, true); err != nil {
		return err
	}
	if err := w.long(3); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		v    oruby.Value
	}{{"excl", w.mrb.BoolValue(r.Exclusive())}, {"begin", r.Begin()}, {"end", r.End()}} {
		if err := w.symbol(w.mrb.Intern(f.name)); err != nil {
			return err
		}
		if err := w.object(f.v, limit); err != nil {
			return err
		}
	}
	return nil
}

// exception: an 'o' record with mesg and bt first, as MRI keeps them as
// instance variables; then cause (when set) and the others
func (w *writer) exception(v oruby.Value, limit int) error {
	if err := w.class('o', v, true); err != nil {
		return err
	}
	mesg := w.mrb.ExcMessage(v)
	if w.mrb.IVGet(v, w.mrb.Intern("@__nil_mesg")).Bool() {
		mesg = oruby.Nil // made without a message: MRI dumps nil
	} else if m, err := w.mrb.Funcall(v, oruby.Sym_message); err == nil && m.IsString() {
		mesg = m
	}
	list := []ivar{
		{w.mrb.Intern("mesg"), mesg},
		{w.mrb.Intern("bt"), w.mrb.ExcBacktrace(v)},
	}
	if c := w.mrb.IVGet(v, w.mrb.Intern("cause")); !c.IsNil() {
		list = append(list, ivar{w.mrb.Intern("cause"), c})
	}
	list = append(list, w.ivars(v)...)
	return w.writeIvarsBare(list, limit)
}

// numeric writes a Rational or a Complex as MRI's marshal_dump does
func (w *writer) numeric(v oruby.Value, limit int) error {
	w.remember(v)
	a, b := oruby.MrbSym(0), oruby.MrbSym(0)
	if w.className(v) == "Rational" {
		a, b = w.mrb.Intern("numerator"), w.mrb.Intern("denominator")
	} else {
		a, b = w.mrb.Intern("real"), w.mrb.Intern("imaginary")
	}
	x, err := w.mrb.Funcall(v, a)
	if err != nil {
		return err
	}
	y, err := w.mrb.Funcall(v, b)
	if err != nil {
		return err
	}
	if err := w.class('U', v, false); err != nil {
		return err
	}
	pair := w.mrb.AryNew()
	pair.Push(x)
	pair.Push(y)
	return w.object(pair.Value(), limit)
}

// userDump: 'u' with the _dump String, whose instance variables (and
// encoding) come first in the object table, then the object itself
func (w *writer) userDump(v oruby.Value, limit int) error {
	payload, err := w.mrb.Funcall(v, oruby.Sym__dump, limit)
	if err != nil {
		return err
	}
	if payload.Type() != oruby.MrbTTString {
		return oruby.ETypeError("_dump() must return string")
	}
	enc := w.encodingName(payload)
	if isBinary(enc) {
		enc = ""
	}
	list := w.ivars(payload)
	hasIvars := enc != "" || len(list) > 0
	if hasIvars {
		if err := w.byte1('I'); err != nil {
			return err
		}
	}
	if err := w.class('u', v, false); err != nil {
		return err
	}
	if err := w.bytes(rubyBytes(payload)); err != nil {
		return err
	}
	if hasIvars {
		if err := w.writeIvars(enc, list, limit); err != nil {
			return err
		}
	}
	w.remember(v)
	return nil
}

func (w *writer) isRegexp(v oruby.Value) bool {
	if v.Type() != oruby.MrbTTCData {
		return false
	}
	_, _, ok := rx.SourceOptions(w.mrb.Data(v))
	return ok
}

func (w *writer) regexp(v oruby.Value) error {
	source, options, ok := rx.SourceOptions(w.mrb.Data(v))
	if !ok {
		return oruby.ETypeError("can't dump Regexp")
	}
	if err := w.uclass(v, "Regexp"); err != nil {
		return err
	}
	if err := w.byte1('/'); err != nil {
		return err
	}
	if err := w.bytes([]byte(source)); err != nil {
		return err
	}
	return w.byte1(byte(options))
}

// class is MRI's w_class: the modules the object is extended with, the
// tag, the class name
func (w *writer) class(tag byte, v oruby.Value, check bool) error {
	if err := w.extended(v, check); err != nil {
		return err
	}
	name, err := w.classPath(w.mrb.ObjClass(v))
	if err != nil {
		return err
	}
	if err := w.byte1(tag); err != nil {
		return err
	}
	return w.symbol(w.mrb.Intern(name))
}

// uclass is MRI's w_uclass: a subclass of a core class names itself with
// 'C'
func (w *writer) uclass(v oruby.Value, base string) error {
	if err := w.extended(v, true); err != nil {
		return err
	}
	real := w.mrb.ObjClass(v)
	if w.mrb.ClassName(real) == base {
		return nil
	}
	name, err := w.classPath(real)
	if err != nil {
		return err
	}
	if err := w.byte1('C'); err != nil {
		return err
	}
	return w.symbol(w.mrb.Intern(name))
}

// extended is MRI's w_extended: an 'e' for each module the object's
// singleton class includes; a singleton class with methods or instance
// variables of its own can't be dumped
func (w *writer) extended(v oruby.Value, check bool) error {
	sc := w.mrb.ClassOf(v)
	if sc.Value().Type() != oruby.MrbTTSClass {
		return nil
	}
	if check {
		for _, m := range []string{"instance_methods", "private_instance_methods"} {
			list, err := w.mrb.Funcall(sc, w.mrb.Intern(m), false)
			if err != nil {
				return err
			}
			if oruby.RArrayLen(list) > 0 {
				return oruby.ETypeError("singleton can't be dumped")
			}
		}
		if len(w.ivars(sc.Value())) > 0 {
			return oruby.ETypeError("singleton can't be dumped")
		}
	}
	ancestors, err := w.mrb.Funcall(sc, oruby.Sym_ancestors)
	if err != nil {
		return err
	}
	real := w.mrb.ObjClass(v).Value()
	for _, m := range append([]oruby.Value(nil), oruby.RArraySlice(ancestors)...) {
		if m == real {
			break
		}
		if m.Type() != oruby.MrbTTModule {
			continue
		}
		path := w.mrb.ClassPath(w.mrb.ClassPtr(m))
		if path.IsNil() || strings.HasPrefix(w.mrb.String(path), "#") {
			// MRI's w_extended calls it a class, whatever it is
			return oruby.ETypeError("can't dump anonymous class %s", w.mrb.String(w.mrb.Inspect(m)))
		}
		name := w.mrb.String(path)
		if err := w.byte1('e'); err != nil {
			return err
		}
		if err := w.symbol(w.mrb.Intern(name)); err != nil {
			return err
		}
	}
	return nil
}

// classPath is MRI's class2path: an anonymous class can't be dumped
func (w *writer) classPath(c oruby.RClass) (string, error) {
	kind := "class"
	if c.Value().Type() == oruby.MrbTTModule {
		kind = "module"
	}
	path := w.mrb.ClassPath(c)
	if path.IsNil() {
		return "", oruby.ETypeError("can't dump anonymous %s %s", kind, w.mrb.String(w.mrb.Inspect(c)))
	}
	name := w.mrb.String(path)
	if strings.HasPrefix(name, "#") {
		return "", oruby.ETypeError("can't dump anonymous %s %s", kind, name)
	}
	return name, nil
}

// className: the name of v's real class, for messages and the special
// cases; no method of v is called (BasicObject has none)
func (w *writer) className(v oruby.Value) string {
	return w.mrb.ClassName(w.mrb.ObjClass(v))
}

// undumpable names the class of a core object MRI keeps as C data with no
// _dump_data (or as IO): those raise TypeError
func (w *writer) undumpable(v oruby.Value) string {
	if v.Type() == oruby.MrbTTProc {
		return "Proc"
	}
	for _, name := range []string{"Method", "UnboundMethod", "Binding", "IO", "StringIO",
		"MatchData", "Thread", "Dir", "Enumerator", "Fiber"} {
		if !w.mrb.ClassDefined(name) {
			continue
		}
		if w.mrb.ObjIsKindOf(v, w.mrb.ClassGet(name)) {
			return w.className(v)
		}
	}
	if w.mrb.ClassDefined("Thread") {
		if m := w.mrb.ClassGetUnder(w.mrb.ClassGet("Thread"), "Mutex"); w.mrb.ObjIsKindOf(v, m) {
			return w.className(v)
		}
	}
	return ""
}

// responds is MRI's rb_obj_respond_to(obj, id, TRUE): an object whose class
// defines respond_to? decides itself; otherwise its methods, private ones
// included, do
func (w *writer) responds(v oruby.Value, sym oruby.MrbSym) bool {
	c := w.mrb.ClassOf(v)
	key := c.Value().Pointer()
	own, ok := w.respondTo[key]
	if !ok {
		own = w.ownRespondTo(c)
		w.respondTo[key] = own
	}
	if own {
		r, err := w.mrb.Funcall(v, oruby.SymQ_respond_to, w.mrb.SymbolValue(sym), true)
		return err == nil && r.Bool()
	}
	return w.mrb.RespondTo(v, sym)
}

// ownRespondTo: c's respond_to? is not Kernel's
func (w *writer) ownRespondTo(c oruby.RClass) bool {
	m, err := w.mrb.Funcall(c, w.mrb.Intern("instance_method"), w.mrb.SymbolValue(oruby.SymQ_respond_to))
	if err != nil {
		return false
	}
	owner, err := w.mrb.Funcall(m, w.mrb.Intern("owner"))
	return err == nil && owner != w.mrb.KernelModule().Value()
}

// encodingName: the name of v's encoding, "" when it has none
func (w *writer) encodingName(v oruby.Value) string {
	if !w.mrb.RespondTo(v, oruby.Sym_encoding) {
		return ""
	}
	enc, err := w.mrb.Funcall(v, oruby.Sym_encoding)
	if err != nil || enc.IsNil() {
		return ""
	}
	if enc.Type() == oruby.MrbTTString {
		return w.mrb.String(enc)
	}
	name, err := w.mrb.Funcall(enc, oruby.Sym_name)
	if err != nil {
		return ""
	}
	return w.mrb.String(name)
}

func isBinary(enc string) bool {
	return strings.EqualFold(enc, "ASCII-8BIT") || strings.EqualFold(enc, "BINARY")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func rubyBytes(value oruby.Value) []byte {
	ptr, n := oruby.RStringMem(value)
	if n == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(ptr, n)...)
}

func rubyByteLen(value oruby.Value) int {
	_, n := oruby.RStringMem(value)
	return n
}

func rubyBytesLimited(value oruby.Value) ([]byte, error) {
	if rubyByteLen(value) > maxInputBytes {
		return nil, oruby.ERangeError("Marshal string exceeds size limit")
	}
	return rubyBytes(value), nil
}
