package marshal

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/oruby/oruby"
	rx "github.com/oruby/oruby/gem/regexp"
)

const (
	defaultDepth  = 256
	maxDepth      = 512
	maxInputBytes = 64 << 20
	maxNodes      = 1 << 20
)

type writer struct {
	mrb     *oruby.MrbState
	buf     bytes.Buffer
	objects map[uintptr]int
	symbols map[oruby.MrbSym]int
	depth   int
	nodes   int
}

func dumpValue(mrb *oruby.MrbState, value oruby.Value, depth int) ([]byte, error) {
	wasDisabled := mrb.GCDisabled()
	if !wasDisabled {
		mrb.GCDisable()
		defer mrb.GCEnable()
	}
	if depth < 0 || depth > maxDepth {
		depth = maxDepth
	}
	w := &writer{
		mrb:     mrb,
		objects: make(map[uintptr]int),
		symbols: make(map[oruby.MrbSym]int),
		depth:   depth,
	}
	w.buf.Write([]byte{4, 8})
	if err := w.writeValue(value, 0); err != nil {
		return nil, err
	}
	return append([]byte(nil), w.buf.Bytes()...), nil
}

func (w *writer) writeByte(b byte) error {
	if w.buf.Len() >= maxInputBytes {
		return oruby.ERangeError("Marshal output exceeds size limit")
	}
	return w.buf.WriteByte(b)
}

func (w *writer) writeBytes(b []byte) error {
	if len(b) > maxInputBytes-w.buf.Len() {
		return oruby.ERangeError("Marshal output exceeds size limit")
	}
	_, err := w.buf.Write(b)
	return err
}

func (w *writer) writeLong(n int64) error {
	if n == 0 {
		return w.writeByte(0)
	}
	if n > -124 && n < 123 {
		return w.writeByte(byte(int8(n + 5)))
	}
	var data [8]byte
	i := 0
	for x := n; x != 0 && x != -1; x >>= 8 {
		data[i] = byte(x)
		i++
	}
	if i == 0 {
		i = 1
		data[0] = byte(n)
	} else if (n > 0 && data[i-1]&0x80 != 0) || (n < 0 && data[i-1]&0x80 == 0) {
		data[i] = 0
		if n < 0 {
			data[i] = 0xff
		}
		i++
	}
	if i > 8 {
		return oruby.ERangeError("integer too large for Marshal format")
	}
	length := i
	if n < 0 {
		length = -length
	}
	if err := w.writeByte(byte(int8(length))); err != nil {
		return err
	}
	return w.writeBytes(data[:i])
}

func (w *writer) writeStringBytes(tag byte, data []byte) error {
	if len(data) > maxInputBytes {
		return oruby.ERangeError("Marshal string exceeds size limit")
	}
	if err := w.writeByte(tag); err != nil {
		return err
	}
	if err := w.writeLong(int64(len(data))); err != nil {
		return err
	}
	return w.writeBytes(data)
}

func (w *writer) writeSymbol(sym oruby.MrbSym) error {
	if id, ok := w.symbols[sym]; ok {
		if err := w.writeByte(';'); err != nil {
			return err
		}
		return w.writeLong(int64(id))
	}
	w.symbols[sym] = len(w.symbols)
	return w.writeStringBytes(':', []byte(w.mrb.SymName(sym)))
}

func (w *writer) reference(value oruby.Value) (bool, error) {
	pointer := value.Pointer()
	if id, ok := w.objects[pointer]; ok {
		if err := w.writeByte('@'); err != nil {
			return true, err
		}
		return true, w.writeLong(int64(id))
	}
	w.objects[pointer] = len(w.objects)
	return false, nil
}

func (w *writer) writeValue(value oruby.Value, depth int) error {
	w.nodes++
	if w.nodes > maxNodes {
		return oruby.ERangeError("Marshal object graph exceeds size limit")
	}
	if depth > w.depth {
		return oruby.EArgumentError("exceed depth limit")
	}
	switch value.Type() {
	case oruby.MrbTTFalse:
		if value.IsNil() {
			return w.writeByte('0')
		}
		return w.writeByte('F')
	case oruby.MrbTTTrue:
		return w.writeByte('T')
	case oruby.MrbTTInteger:
		return w.writeInteger(big.NewInt(value.Int64()))
	case oruby.MrbTTSymbol:
		return w.writeSymbol(value.Symbol())
	}
	hasMarshalDump := w.mrb.RespondTo(value, oruby.Sym_marshal_dump)
	hasUserDump := w.mrb.RespondTo(value, oruby.Sym__dump)
	if value.HasBasic() {
		linked, err := w.reference(value)
		if err != nil || linked {
			return err
		}
	}

	if hasMarshalDump {
		return w.writeMarshalDump(value, depth)
	}
	if hasUserDump {
		return w.writeUserDump(value)
	}
	if value.Type() == oruby.MrbTTCData && w.mrb.ClassOf(value).Name() == "Regexp" {
		return w.writeRegexp(value)
	}

	switch value.Type() {
	case oruby.MrbTTFloat:
		f := oruby.MrbFloat(value)
		text := strconv.FormatFloat(f, 'g', -1, 64)
		if math.IsNaN(f) {
			text = "nan"
		} else if math.IsInf(f, 1) {
			text = "inf"
		} else if math.IsInf(f, -1) {
			text = "-inf"
		}
		return w.writeStringBytes('f', []byte(text))
	case oruby.MrbTTString:
		return w.writeString(value, depth)
	case oruby.MrbTTArray:
		items := oruby.RArraySlice(value)
		if len(items) > maxNodes {
			return oruby.ERangeError("Marshal array exceeds size limit")
		}
		if err := w.writeByte('['); err != nil {
			return err
		}
		if err := w.writeLong(int64(len(items))); err != nil {
			return err
		}
		for _, item := range items {
			if err := w.writeValue(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	case oruby.MrbTTHash:
		if oruby.MrbRHashProcDefaultP(value) {
			return oruby.ETypeError("can't dump hash with default proc")
		}
		entries := oruby.RHashEntries(value)
		if len(entries) > maxNodes {
			return oruby.ERangeError("Marshal hash exceeds size limit")
		}
		defaultValue := w.mrb.RHashIfNone(value)
		tag := byte('{')
		if !defaultValue.IsNil() {
			tag = '}'
		}
		if err := w.writeByte(tag); err != nil {
			return err
		}
		if err := w.writeLong(int64(len(entries))); err != nil {
			return err
		}
		for _, pair := range entries {
			if err := w.writeValue(pair[0], depth+1); err != nil {
				return err
			}
			if err := w.writeValue(pair[1], depth+1); err != nil {
				return err
			}
		}
		if tag == '}' {
			return w.writeValue(defaultValue, depth+1)
		}
		return nil
	case oruby.MrbTTClass, oruby.MrbTTModule:
		name, err := w.classPath(value)
		if err != nil {
			return err
		}
		tag := byte('c')
		if value.Type() == oruby.MrbTTModule {
			tag = 'm'
		}
		return w.writeStringBytes(tag, []byte(name))
	case oruby.MrbTTStruct, oruby.MrbTTIStruct:
		return w.writeStruct(value, depth)
	case oruby.MrbTTRange:
		return w.writeRange(value, depth)
	case oruby.MrbTTObject:
		return w.writeObject(value, depth)
	case oruby.MrbTTException:
		return w.writeException(value, depth)
	case oruby.MrbTTBigInt:
		z, err := w.mrb.BigInt(value)
		if err != nil {
			return err
		}
		return w.writeInteger(z)
	default:
		return oruby.ETypeError("can't dump %s", w.mrb.TypeName(value))
	}
}

func (w *writer) writeInteger(value *big.Int) error {
	if isMarshalFixnum(value) {
		if err := w.writeByte('i'); err != nil {
			return err
		}
		return w.writeLong(value.Int64())
	}

	if value.BitLen() > maxInputBytes*8 {
		return oruby.ERangeError("Marshal bignum exceeds size limit")
	}
	magnitude := new(big.Int).Abs(value).Bytes()
	words := (len(magnitude) + 1) / 2
	if err := w.writeByte('l'); err != nil {
		return err
	}
	sign := byte('+')
	if value.Sign() < 0 {
		sign = '-'
	}
	if err := w.writeByte(sign); err != nil {
		return err
	}
	if err := w.writeLong(int64(words)); err != nil {
		return err
	}
	for i := 0; i < words*2; i++ {
		index := len(magnitude) - i - 1
		var b byte
		if index >= 0 {
			b = magnitude[index]
		}
		if err := w.writeByte(b); err != nil {
			return err
		}
	}
	return nil
}

func isMarshalFixnum(value *big.Int) bool {
	return value.Cmp(big.NewInt(-(1<<30))) >= 0 && value.Cmp(big.NewInt((1<<30)-1)) <= 0
}

func (w *writer) writeString(value oruby.Value, depth int) error {
	data, err := rubyBytesLimited(value)
	if err != nil {
		return err
	}
	encoding := "UTF-8"
	if w.mrb.RespondTo(value, oruby.Sym_encoding) {
		enc, err := w.mrb.Funcall(value, oruby.Sym_encoding)
		if err != nil {
			return err
		}
		if enc.Type() == oruby.MrbTTString {
			encoding = w.mrb.String(enc)
		} else if w.mrb.RespondTo(enc, oruby.Sym_name) {
			name, err := w.mrb.Funcall(enc, oruby.Sym_name)
			if err != nil {
				return err
			}
			encoding = w.mrb.String(name)
		}
	} else if !utf8.Valid(data) {
		encoding = "ASCII-8BIT"
	}
	vars, err := w.ivars(value)
	if err != nil {
		return err
	}
	if len(vars) != 0 {
		return oruby.ETypeError("can't dump String instance variables")
	}
	binaryEncoding := strings.EqualFold(encoding, "ASCII-8BIT") || strings.EqualFold(encoding, "BINARY")
	if binaryEncoding {
		return w.writeStringBytes('"', data)
	}
	if err := w.writeByte('I'); err != nil {
		return err
	}
	if err := w.writeStringBytes('"', data); err != nil {
		return err
	}
	encodingName := "E"
	encodingValue := oruby.True
	if strings.EqualFold(encoding, "UTF-8") {
	} else if strings.EqualFold(encoding, "US-ASCII") || binaryEncoding {
		encodingValue = oruby.False
	} else {
		encodingName = "encoding"
		encodingValue = w.mrb.StrNew(encoding).Value()
	}
	if err := w.writeLong(1); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(encodingName)); err != nil {
		return err
	}
	if err := w.writeValue(encodingValue, depth); err != nil {
		return err
	}
	return nil
}

func (w *writer) writeMarshalDump(value oruby.Value, depth int) error {
	name, err := w.classPath(w.mrb.ClassOf(value).Value())
	if err != nil {
		return err
	}
	payload, err := w.mrb.Funcall(value, oruby.Sym_marshal_dump)
	if err != nil {
		return err
	}
	if err := w.writeByte('U'); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(name)); err != nil {
		return err
	}
	return w.writeValue(payload, depth+1)
}

func (w *writer) writeUserDump(value oruby.Value) error {
	name, err := w.classPath(w.mrb.ClassOf(value).Value())
	if err != nil {
		return err
	}
	payload, err := w.mrb.Funcall(value, oruby.Sym__dump, -1)
	if err != nil {
		return err
	}
	if payload.Type() != oruby.MrbTTString {
		return oruby.ETypeError("_dump must return String")
	}
	if err := w.writeByte('u'); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(name)); err != nil {
		return err
	}
	data, err := rubyBytesLimited(payload)
	if err != nil {
		return err
	}
	if err := w.writeLong(int64(len(data))); err != nil {
		return err
	}
	return w.writeBytes(data)
}

func (w *writer) writeRegexp(value oruby.Value) error {
	source := w.mrb.IVGet(value, oruby.IvSym_source)
	options := w.mrb.IVGet(value, oruby.IvSym_options)
	if source.Type() != oruby.MrbTTString && w.mrb.RespondTo(value, oruby.Sym_source) {
		var err error
		source, err = w.mrb.Funcall(value, oruby.Sym_source)
		if err != nil {
			return err
		}
	}
	if source.Type() != oruby.MrbTTString {
		return oruby.ETypeError("Regexp source/options cannot be represented in Marshal format")
	}
	compiledSource, compiledOptions, ok := rx.SourceOptions(w.mrb.Data(value))
	optionBits := -1
	if options.Type() == oruby.MrbTTInteger {
		optionBits = int(options.Int())
	} else if ok {
		optionBits = compiledOptions
	}
	if optionBits < 0 || optionBits > 0xff {
		return oruby.ETypeError("Regexp source/options cannot be represented in Marshal format")
	}
	if !ok || compiledSource != w.mrb.String(source) || compiledOptions != optionBits {
		return oruby.ETypeError("Regexp compiled pattern does not match its stored source/options")
	}
	if err := w.writeByte('I'); err != nil {
		return err
	}
	pattern, err := rubyBytesLimited(source)
	if err != nil {
		return err
	}
	if err := w.writeStringBytes('/', pattern); err != nil {
		return err
	}
	if err := w.writeByte(byte(optionBits)); err != nil {
		return err
	}
	if err := w.writeLong(1); err != nil {
		return err
	}
	if err := w.writeSymbol(oruby.Sym_E); err != nil {
		return err
	}
	return w.writeByte('F')
}

func (w *writer) classPath(value oruby.Value) (string, error) {
	var klass oruby.RClass
	if value.Type() == oruby.MrbTTClass || value.Type() == oruby.MrbTTModule {
		klass = w.mrb.ClassPtr(value)
	} else {
		// the real class: ClassOf is the singleton class of an extended object
		c, err := w.mrb.Funcall(value, oruby.Sym_class)
		if err != nil {
			return "", err
		}
		klass = w.mrb.ClassPtr(c)
	}
	path := w.mrb.ClassPath(klass)
	if path.IsNil() {
		return "", oruby.ETypeError("can't dump anonymous class")
	}
	return w.mrb.String(path), nil
}

func (w *writer) ivars(value oruby.Value) ([]oruby.Value, error) {
	result, err := w.mrb.Funcall(value, oruby.Sym_instance_variables)
	if err != nil {
		return nil, err
	}
	if result.Type() != oruby.MrbTTArray {
		return nil, oruby.ETypeError("instance_variables did not return an Array")
	}
	items := append([]oruby.Value(nil), oruby.RArraySlice(result)...)
	if len(items) > maxNodes {
		return nil, oruby.ERangeError("too many instance variables")
	}
	for _, item := range items {
		if item.Type() != oruby.MrbTTSymbol {
			return nil, oruby.ETypeError("instance variable name must be a Symbol")
		}
	}
	return items, nil
}

// writeExtended writes 'e' for each module an object's singleton class
// includes; a singleton with methods can't be dumped (MRI's w_extended)
func (w *writer) writeExtended(value oruby.Value) error {
	mods, err := w.mrb.Eval(`->(o) {
  sc = o.singleton_class
  raise TypeError, "singleton can't be dumped" unless o.singleton_methods(false).empty?
  mods = sc.ancestors.take_while { |m| !m.equal?(o.class) }.reject { |m| m.equal?(sc) || Class === m }
  raise TypeError, "can't dump anonymous class" if mods.any? { |m| m.name.nil? }
  mods.map(&:name).join("\0")
}`)
	if err != nil {
		return err
	}
	names, err := w.mrb.Funcall(mods, oruby.Sym_call, value)
	if err != nil {
		return err
	}
	if w.mrb.String(names) == "" {
		return nil
	}
	for _, n := range strings.Split(w.mrb.String(names), "\x00") {
		if err := w.writeByte('e'); err != nil {
			return err
		}
		if err := w.writeSymbol(w.mrb.Intern(n)); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) writeObject(value oruby.Value, depth int) error {
	name, err := w.classPath(value)
	if err != nil {
		return err
	}
	if err := w.writeExtended(value); err != nil {
		return err
	}
	vars, err := w.ivars(value)
	if err != nil {
		return err
	}
	if err := w.writeByte('o'); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(name)); err != nil {
		return err
	}
	if err := w.writeLong(int64(len(vars))); err != nil {
		return err
	}
	for _, name := range vars {
		if err := w.writeSymbol(name.Symbol()); err != nil {
			return err
		}
		item := w.mrb.IVGet(value, name.Symbol())
		if err := w.writeValue(item, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) writeStruct(value oruby.Value, depth int) error {
	name, err := w.classPath(value)
	if err != nil {
		return err
	}
	if err := w.writeExtended(value); err != nil {
		return err
	}
	klass := w.mrb.ClassOf(value)
	members, err := w.mrb.Funcall(klass, oruby.Sym_members)
	if err != nil {
		return fmt.Errorf("Marshal Struct members: %w", err)
	}
	fields := oruby.RArraySlice(members)
	if len(fields) > maxNodes {
		return oruby.ERangeError("too many Struct members")
	}
	if err := w.writeByte('S'); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(name)); err != nil {
		return err
	}
	if err := w.writeLong(int64(len(fields))); err != nil {
		return err
	}
	for _, field := range fields {
		if field.Type() != oruby.MrbTTSymbol {
			return fmt.Errorf("unsupported Struct member %s", w.mrb.TypeName(field))
		}
		item, err := w.mrb.Funcall(value, oruby.OpSym_aref, field)
		if err != nil {
			return fmt.Errorf("Marshal Struct member %s: %w", w.mrb.SymName(field.Symbol()), err)
		}
		if err := w.writeSymbol(field.Symbol()); err != nil {
			return err
		}
		if err := w.writeValue(item, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) writeRange(value oruby.Value, depth int) error {
	rangeValue, err := w.mrb.MrbRangePtr(value)
	if err != nil {
		return err
	}
	if err := w.writeByte('o'); err != nil {
		return err
	}
	if err := w.writeSymbol(oruby.Sym_Range); err != nil {
		return err
	}
	if err := w.writeLong(3); err != nil {
		return err
	}
	fields := []struct {
		name  string
		value oruby.Value
	}{
		{"excl", w.mrb.BoolValue(rangeValue.Exclusive())},
		{"begin", rangeValue.Begin()},
		{"end", rangeValue.End()},
	}
	for _, field := range fields {
		if err := w.writeSymbol(w.mrb.Intern(field.name)); err != nil {
			return err
		}
		if err := w.writeValue(field.value, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) writeException(value oruby.Value, depth int) error {
	name, err := w.classPath(value)
	if err != nil {
		return err
	}
	message := w.mrb.ExcMessage(value)
	backtrace := w.mrb.ExcBacktrace(value)
	vars, err := w.ivars(value)
	if err != nil {
		return err
	}
	if err := w.writeByte('o'); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(name)); err != nil {
		return err
	}
	fieldCount := 1 + len(vars)
	writeBacktrace := !message.IsNil() || !backtrace.IsNil()
	if writeBacktrace {
		fieldCount++
	}
	if err := w.writeLong(int64(fieldCount)); err != nil {
		return err
	}
	fields := []struct {
		name  string
		value oruby.Value
	}{{"mesg", message}}
	if writeBacktrace {
		fields = append(fields, struct {
			name  string
			value oruby.Value
		}{"bt", backtrace})
	}
	for _, field := range fields {
		if err := w.writeSymbol(w.mrb.Intern(field.name)); err != nil {
			return err
		}
		if err := w.writeValue(field.value, depth+1); err != nil {
			return err
		}
	}
	for _, variable := range vars {
		if err := w.writeSymbol(variable.Symbol()); err != nil {
			return err
		}
		if err := w.writeValue(w.mrb.IVGet(value, variable.Symbol()), depth+1); err != nil {
			return err
		}
	}
	return nil
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
