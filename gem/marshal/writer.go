package marshal

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/oruby/oruby"
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
		if err := w.writeByte('i'); err != nil {
			return err
		}
		return w.writeLong(value.Int64())
	case oruby.MrbTTSymbol:
		return w.writeSymbol(value.Symbol())
	}

	if value.HasBasic() {
		linked, err := w.reference(value)
		if err != nil || linked {
			return err
		}
	}

	if w.mrb.RespondTo(value, w.mrb.Intern("marshal_dump")) {
		return w.writeMarshalDump(value, depth)
	}
	if w.mrb.RespondTo(value, w.mrb.Intern("_dump")) {
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
	case oruby.MrbTTObject, oruby.MrbTTException:
		return w.writeObject(value, depth)
	case oruby.MrbTTBigInt:
		return oruby.ETypeError("can't dump bignum values")
	default:
		return oruby.ETypeError("can't dump %s", w.mrb.TypeName(value))
	}
}

func (w *writer) writeString(value oruby.Value, depth int) error {
	data := rubyBytes(value)
	encoding := "UTF-8"
	if w.mrb.RespondTo(value, w.mrb.Intern("encoding")) {
		enc, err := w.mrb.Funcall(value, w.mrb.Intern("encoding"))
		if err != nil {
			return err
		}
		if enc.Type() == oruby.MrbTTString {
			encoding = w.mrb.String(enc)
		} else if w.mrb.RespondTo(enc, w.mrb.Intern("name")) {
			name, err := w.mrb.Funcall(enc, w.mrb.Intern("name"))
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
	binaryEncoding := strings.EqualFold(encoding, "ASCII-8BIT") || strings.EqualFold(encoding, "BINARY")
	if binaryEncoding && len(vars) == 0 {
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
		encodingValue = w.mrb.BytesValue([]byte(encoding))
	}
	if err := w.writeLong(int64(len(vars) + 1)); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern(encodingName)); err != nil {
		return err
	}
	if err := w.writeValue(encodingValue, depth); err != nil {
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

func (w *writer) writeMarshalDump(value oruby.Value, depth int) error {
	name, err := w.classPath(w.mrb.ClassOf(value).Value())
	if err != nil {
		return err
	}
	payload, err := w.mrb.Funcall(value, w.mrb.Intern("marshal_dump"))
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
	payload, err := w.mrb.Funcall(value, w.mrb.Intern("_dump"), -1)
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
	data := rubyBytes(payload)
	if err := w.writeLong(int64(len(data))); err != nil {
		return err
	}
	return w.writeBytes(data)
}

func (w *writer) writeRegexp(value oruby.Value) error {
	source := w.mrb.IVGet(value, w.mrb.Intern("@source"))
	options := w.mrb.IVGet(value, w.mrb.Intern("@options"))
	if source.Type() != oruby.MrbTTString && w.mrb.RespondTo(value, w.mrb.Intern("source")) {
		var err error
		source, err = w.mrb.Funcall(value, w.mrb.Intern("source"))
		if err != nil {
			return err
		}
	}
	if source.Type() != oruby.MrbTTString {
		return oruby.ETypeError("Regexp source/options cannot be represented in Marshal format")
	}
	optionBits := -1
	if options.Type() == oruby.MrbTTInteger {
		optionBits = int(options.Int())
	} else if compiled, ok := w.mrb.Data(value).(*regexp.Regexp); ok {
		optionBits = regexpOptions(compiled.String())
	}
	if optionBits < 0 || optionBits > 0xff {
		return oruby.ETypeError("Regexp source/options cannot be represented in Marshal format")
	}
	compiled, ok := w.mrb.Data(value).(*regexp.Regexp)
	if !ok || (optionBits&2 == 0 && compiled.String() != regexpSourceWithOptions(w.mrb.String(source), optionBits)) {
		return oruby.ETypeError("Regexp compiled pattern does not match its stored source/options")
	}
	if err := w.writeByte('I'); err != nil {
		return err
	}
	if err := w.writeStringBytes('/', rubyBytes(source)); err != nil {
		return err
	}
	if err := w.writeByte(byte(optionBits)); err != nil {
		return err
	}
	if err := w.writeLong(1); err != nil {
		return err
	}
	if err := w.writeSymbol(w.mrb.Intern("E")); err != nil {
		return err
	}
	return w.writeByte('F')
}

func regexpOptions(pattern string) int {
	if !strings.HasPrefix(pattern, "(?") {
		return -1
	}
	end := strings.IndexByte(pattern, ')')
	if end < 3 {
		return -1
	}
	flags := strings.SplitN(pattern[2:end], "-", 2)[0]
	options := 0
	if strings.Contains(flags, "i") {
		options |= 1
	}
	if strings.Contains(flags, "m") || strings.Contains(flags, "s") {
		options |= 4
	}
	return options
}

func regexpSourceWithOptions(source string, options int) string {
	enabled, disabled := "", "-"
	if options&1 != 0 {
		enabled += "i"
	} else {
		disabled += "i"
	}
	if options&4 != 0 {
		enabled += "ms"
	} else {
		disabled += "ms"
	}
	if disabled == "-" {
		disabled = ""
	}
	return fmt.Sprintf("(?%s%s)%s", enabled, disabled, source)
}

func (w *writer) classPath(value oruby.Value) (string, error) {
	var klass oruby.RClass
	if value.Type() == oruby.MrbTTClass || value.Type() == oruby.MrbTTModule {
		klass = w.mrb.ClassPtr(value)
	} else {
		klass = w.mrb.ClassOf(value)
	}
	path := w.mrb.ClassPath(klass)
	if path.IsNil() {
		return "", oruby.ETypeError("can't dump anonymous class")
	}
	return w.mrb.String(path), nil
}

func (w *writer) ivars(value oruby.Value) ([]oruby.Value, error) {
	result, err := w.mrb.Funcall(value, w.mrb.Intern("instance_variables"))
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

func (w *writer) writeObject(value oruby.Value, depth int) error {
	name, err := w.classPath(value)
	if err != nil {
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
	klass := w.mrb.ClassOf(value)
	members, err := w.mrb.Funcall(klass, w.mrb.Intern("members"))
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
		item, err := w.mrb.Funcall(value, w.mrb.Intern("[]"), field)
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

func rubyBytes(value oruby.Value) []byte {
	ptr, n := oruby.RStringMem(value)
	if n == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(ptr, n)...)
}
