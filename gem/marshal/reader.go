package marshal

import (
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

type rubyReader struct {
	mrb    *oruby.MrbState
	object oruby.Value
}

func (r *rubyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	value, err := r.mrb.Funcall(r.object, r.mrb.Intern("read"), len(p))
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
	data, err := rubyBytesLimited(value)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, data)
	return n, nil
}

type reader struct {
	mrb     *oruby.MrbState
	source  byteSource
	objects []oruby.Value
	symbols []oruby.MrbSym
	nodes   int
	read    int64
	proc    oruby.Value // Marshal.load(src, proc): called with each loaded object
	freeze  bool        // Marshal.load(src, freeze: true)
}

func loadValue(mrb *oruby.MrbState, source byteSource) (oruby.Value, error) {
	return loadValueWith(mrb, source, oruby.Nil, false)
}

func loadValueWith(mrb *oruby.MrbState, source byteSource, proc oruby.Value, freeze bool) (oruby.Value, error) {
	wasDisabled := mrb.GCDisabled()
	if !wasDisabled {
		mrb.GCDisable()
		defer mrb.GCEnable()
	}
	r := &reader{mrb: mrb, source: source, proc: proc, freeze: freeze}
	var header [2]byte
	if err := r.readFull(header[:]); err != nil {
		return oruby.Nil, oruby.EArgumentError("marshal data too short")
	}
	if header[0] != 4 || header[1] != 8 {
		return oruby.Nil, oruby.ETypeError("incompatible marshal file format (version %d.%d)", header[0], header[1])
	}
	return r.readValue(0)
}

func (r *reader) readByte() (byte, error) {
	var b [1]byte
	if err := r.readFull(b[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, oruby.EArgumentError("marshal data too short")
		}
		return 0, err
	}
	return b[0], nil
}

func (r *reader) readLong() (int64, error) {
	b, err := r.readByte()
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
	if length < 1 || length > 8 {
		return 0, oruby.EArgumentError("invalid long integer in Marshal data")
	}
	var data [8]byte
	if err := r.readFull(data[:length]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, oruby.EArgumentError("marshal data too short")
		}
		return 0, err
	}
	if negative && length < len(data) {
		for i := length; i < len(data); i++ {
			data[i] = 0xff
		}
	}
	value := int64(binary.LittleEndian.Uint64(data[:]))
	if negative && value >= 0 {
		return 0, oruby.EArgumentError("invalid negative long integer in Marshal data")
	}
	return value, nil
}

func (r *reader) readLength() (int, error) {
	n, err := r.readLong()
	if err != nil {
		return 0, err
	}
	if n < 0 || n > maxInputBytes {
		return 0, oruby.ERangeError("Marshal length exceeds size limit")
	}
	return int(n), nil
}

func (r *reader) readCount() (int, error) {
	n, err := r.readLong()
	if err != nil {
		return 0, err
	}
	if n < 0 || n > maxNodes {
		return 0, oruby.ERangeError("Marshal item count exceeds size limit")
	}
	return int(n), nil
}

func (r *reader) readFull(data []byte) error {
	if err := r.checkReadBudget(len(data)); err != nil {
		return err
	}
	n, err := io.ReadFull(r.source, data)
	r.read += int64(n)
	return err
}

func (r *reader) checkReadBudget(n int) error {
	if n < 0 || int64(n) > int64(maxInputBytes)-r.read {
		return oruby.ERangeError("Marshal input exceeds size limit")
	}
	return nil
}

func (r *reader) readBytes() ([]byte, error) {
	n, err := r.readLength()
	if err != nil {
		return nil, err
	}
	if err := r.checkReadBudget(n); err != nil {
		return nil, err
	}
	data := make([]byte, n)
	if err := r.readFull(data); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, oruby.EArgumentError("marshal data too short")
		}
		return nil, err
	}
	return data, nil
}

// readValue reads one value and applies the load proc and freeze: option
func (r *reader) readValue(depth int) (oruby.Value, error) {
	v, err := r.readValue0(depth)
	if err != nil || (r.proc.IsNil() && !r.freeze) || v.Type() == oruby.MrbTTSymbol {
		return v, err
	}
	if r.freeze && v.Type() != oruby.MrbTTClass && v.Type() != oruby.MrbTTModule {
		if _, err := r.mrb.Funcall(v, r.mrb.Intern("freeze")); err != nil {
			return oruby.Nil, err
		}
	}
	if !r.proc.IsNil() {
		return r.mrb.Funcall(r.proc, r.mrb.Intern("call"), v)
	}
	return v, nil
}

func (r *reader) readValue0(depth int) (oruby.Value, error) {
	r.nodes++
	if r.nodes > maxNodes {
		return oruby.Nil, oruby.ERangeError("Marshal object graph exceeds size limit")
	}
	if depth > maxDepth {
		return oruby.Nil, oruby.ERangeError("Marshal nesting exceeds depth limit")
	}
	tag, err := r.readByte()
	if err != nil {
		return oruby.Nil, err
	}
	switch tag {
	case '0':
		return oruby.Nil, nil
	case 'T':
		return oruby.True, nil
	case 'F':
		return oruby.False, nil
	case 'i':
		n, err := r.readLong()
		if err != nil {
			return oruby.Nil, err
		}
		return r.mrb.Value(n), nil
	case ':':
		name, err := r.readBytes()
		if err != nil {
			return oruby.Nil, err
		}
		sym := r.mrb.Intern(string(name))
		r.symbols = append(r.symbols, sym)
		return r.mrb.SymbolValue(sym), nil
	case ';':
		id, err := r.readLong()
		if err != nil {
			return oruby.Nil, err
		}
		if id < 0 || id >= int64(len(r.symbols)) {
			return oruby.Nil, oruby.EArgumentError("symbol link out of range")
		}
		return r.mrb.SymbolValue(r.symbols[id]), nil
	case '@':
		id, err := r.readLong()
		if err != nil {
			return oruby.Nil, err
		}
		if id < 0 || id >= int64(len(r.objects)) {
			return oruby.Nil, oruby.EArgumentError("object link out of range")
		}
		return r.objects[id], nil
	case '"':
		return r.readStringObject()
	case '[':
		return r.readArray(depth)
	case '{', '}':
		return r.readHash(tag == '}', depth)
	case 'f':
		return r.readFloat()
	case 'I':
		return r.readIvars(depth)
	case 'o':
		return r.readObject(depth)
	case 'S':
		return r.readStruct(depth)
	case 'U':
		return r.readMarshalLoad(depth)
	case 'u':
		return r.readUserLoad()
	case 'c', 'm':
		name, err := r.readBytes()
		if err != nil {
			return oruby.Nil, err
		}
		value, err := r.lookupConstant(string(name))
		if err != nil {
			return oruby.Nil, err
		}
		if (tag == 'c' && value.Type() != oruby.MrbTTClass) || (tag == 'm' && value.Type() != oruby.MrbTTModule) {
			return oruby.Nil, oruby.ETypeError("class/module type mismatch in Marshal data")
		}
		r.addObject(value)
		return value, nil
	case '/':
		return r.readRegexp()
	case 'l':
		return r.readBignum()
	case 'e':
		// object extended with a module: 'e' module-name object
		name, err := r.readClassSymbol(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		mod, err := r.lookupConstant(name)
		if err != nil {
			return oruby.Nil, err
		}
		v, err := r.readValue0(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if _, err := r.mrb.Funcall(v, r.mrb.Intern("extend"), mod); err != nil {
			return oruby.Nil, err
		}
		return v, nil
	case 'C':
		// instance of a user subclass of String, Array, Hash or Regexp
		name, err := r.readClassSymbol(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		klass, err := r.lookupConstant(name)
		if err != nil {
			return oruby.Nil, err
		}
		at := len(r.objects)
		v, err := r.readValue0(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		obj, err := r.mrb.Funcall(klass, r.mrb.Intern("allocate"))
		if err != nil {
			return oruby.Nil, err
		}
		if _, err := r.mrb.Funcall(obj, r.mrb.Intern("replace"), v); err != nil {
			return oruby.Nil, err
		}
		if at < len(r.objects) {
			r.objects[at] = obj
		}
		return obj, nil
	default:
		return oruby.Nil, oruby.EArgumentError("invalid/unknown Marshal format (0x%02x)", tag)
	}
}

func (r *reader) readBignum() (oruby.Value, error) {
	sign, err := r.readByte()
	if err != nil {
		return oruby.Nil, err
	}
	if sign != '+' && sign != '-' {
		return oruby.Nil, oruby.EArgumentError("invalid bignum sign in Marshal data")
	}
	words, err := r.readLong()
	if err != nil {
		return oruby.Nil, err
	}
	if words < 0 || words > maxInputBytes/2 {
		return oruby.Nil, oruby.ERangeError("Marshal bignum exceeds size limit")
	}
	byteCount := int(words) * 2
	if err := r.checkReadBudget(byteCount); err != nil {
		return oruby.Nil, err
	}
	littleEndian := make([]byte, byteCount)
	if err := r.readFull(littleEndian); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return oruby.Nil, oruby.EArgumentError("marshal data too short")
		}
		return oruby.Nil, err
	}
	for left, right := 0, len(littleEndian)-1; left < right; left, right = left+1, right-1 {
		littleEndian[left], littleEndian[right] = littleEndian[right], littleEndian[left]
	}
	value := new(big.Int).SetBytes(littleEndian)
	if sign == '-' {
		value.Neg(value)
	}
	result := r.mrb.BigIntValue(value)
	r.addObject(result)
	return result, nil
}

func (r *reader) readStringObject() (oruby.Value, error) {
	data, err := r.readBytes()
	if err != nil {
		return oruby.Nil, err
	}
	value := r.mrb.BytesValue(data)
	r.addObject(value)
	return value, nil
}

func (r *reader) readArray(depth int) (oruby.Value, error) {
	n, err := r.readCount()
	if err != nil {
		return oruby.Nil, err
	}
	array := r.mrb.AryNewCapa(n)
	r.addObject(array.Value())
	for i := 0; i < n; i++ {
		value, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		array.Push(value)
	}
	return array.Value(), nil
}

func (r *reader) readHash(hasDefault bool, depth int) (oruby.Value, error) {
	n, err := r.readCount()
	if err != nil {
		return oruby.Nil, err
	}
	hash := r.mrb.HashNewCapa(n)
	r.addObject(hash.Value())
	for i := 0; i < n; i++ {
		key, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		value, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		hash.Set(key, value)
	}
	if hasDefault {
		value, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if _, err := r.mrb.Funcall(hash, r.mrb.Intern("default="), value); err != nil {
			return oruby.Nil, err
		}
	}
	return hash.Value(), nil
}

func (r *reader) readFloat() (oruby.Value, error) {
	data, err := r.readBytes()
	if err != nil {
		return oruby.Nil, err
	}
	var value float64
	switch string(data) {
	case "nan":
		value = math.NaN()
	case "inf":
		value = math.Inf(1)
	case "-inf":
		value = math.Inf(-1)
	default:
		value, err = strconv.ParseFloat(string(data), 64)
		if err != nil {
			return oruby.Nil, oruby.EArgumentError("invalid value for Float(): %q", data)
		}
	}
	result := r.mrb.FloatValue(value)
	r.addObject(result)
	return result, nil
}

func (r *reader) readIvars(depth int) (oruby.Value, error) {
	value, err := r.readValue(depth)
	if err != nil {
		return oruby.Nil, err
	}
	n, err := r.readCount()
	if err != nil {
		return oruby.Nil, err
	}
	for i := 0; i < n; i++ {
		key, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if key.Type() != oruby.MrbTTSymbol {
			return oruby.Nil, oruby.EArgumentError("instance variable name is not a Symbol")
		}
		item, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		name := r.mrb.SymName(key.Symbol())
		if value.Type() == oruby.MrbTTCData && r.mrb.ClassOf(value).Name() == "Regexp" && name == "E" {
			if item.Type() != oruby.MrbTTFalse {
				return oruby.Nil, oruby.ETypeError("Regexp encoding metadata is not supported")
			}
			continue
		}
		if value.Type() == oruby.MrbTTString && (name == "E" || name == "encoding") {
			encoding := "US-ASCII"
			if name == "encoding" {
				if item.Type() != oruby.MrbTTString {
					return oruby.Nil, oruby.EArgumentError("String encoding metadata is not a String")
				}
				encoding = r.mrb.String(item)
			} else {
				switch item.Type() {
				case oruby.MrbTTTrue:
					encoding = "UTF-8"
				case oruby.MrbTTFalse:
				default:
					return oruby.Nil, oruby.EArgumentError("String encoding flag is not boolean")
				}
			}
			if !r.mrb.RespondTo(value, r.mrb.Intern("force_encoding")) {
				return oruby.Nil, oruby.ETypeError("String encoding metadata cannot be restored")
			}
			if _, err := r.mrb.Funcall(value, r.mrb.Intern("force_encoding"), encoding); err != nil {
				return oruby.Nil, err
			}
			continue
		}
		if value.Type() == oruby.MrbTTString {
			return oruby.Nil, oruby.ETypeError("String instance variables cannot be restored by this mruby runtime")
		}
		if err := r.mrb.IVSet(value, key.Symbol(), item); err != nil {
			return oruby.Nil, err
		}
	}
	return value, nil
}

func (r *reader) readObject(depth int) (oruby.Value, error) {
	name, err := r.readClassSymbol(depth)
	if err != nil {
		return oruby.Nil, err
	}
	klass, err := r.lookupConstant(name)
	if err != nil {
		return oruby.Nil, err
	}
	if klass.Type() != oruby.MrbTTClass {
		return oruby.Nil, oruby.ETypeError("%s is not a class", name)
	}
	if name == "Range" {
		return r.readRange(klass, depth)
	}
	value, err := r.mrb.Funcall(klass, r.mrb.Intern("allocate"))
	if err != nil {
		return oruby.Nil, err
	}
	r.addObject(value)
	n, err := r.readCount()
	if err != nil {
		return oruby.Nil, err
	}
	if value.Type() == oruby.MrbTTException {
		return r.readException(value, n, depth)
	}
	for i := 0; i < n; i++ {
		key, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if key.Type() != oruby.MrbTTSymbol {
			return oruby.Nil, oruby.EArgumentError("instance variable name is not a Symbol")
		}
		item, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if err := r.mrb.IVSet(value, key.Symbol(), item); err != nil {
			return oruby.Nil, err
		}
	}
	return value, nil
}

func (r *reader) readRange(klass oruby.Value, depth int) (oruby.Value, error) {
	value, err := r.mrb.Funcall(klass, r.mrb.Intern("allocate"))
	if err != nil {
		return oruby.Nil, err
	}
	if value.Type() != oruby.MrbTTRange {
		return oruby.Nil, oruby.ETypeError("Range.allocate did not return a Range")
	}
	r.addObject(value)
	n, err := r.readCount()
	if err != nil {
		return oruby.Nil, err
	}
	if n != 3 {
		return oruby.Nil, oruby.EArgumentError("invalid Range field count in Marshal data")
	}
	var begin, end oruby.Value
	var exclusive bool
	seen := make(map[string]bool, 3)
	for i := 0; i < n; i++ {
		key, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if key.Type() != oruby.MrbTTSymbol {
			return oruby.Nil, oruby.EArgumentError("Range field name is not a Symbol")
		}
		name := r.mrb.SymName(key.Symbol())
		if seen[name] {
			return oruby.Nil, oruby.EArgumentError("duplicate Range field %s in Marshal data", name)
		}
		seen[name] = true
		field, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		switch name {
		case "begin":
			begin = field
		case "end":
			end = field
		case "excl":
			if field.Type() != oruby.MrbTTTrue && field.Type() != oruby.MrbTTFalse {
				return oruby.Nil, oruby.EArgumentError("Range exclusivity flag is not boolean")
			}
			exclusive = field.Bool()
		default:
			return oruby.Nil, oruby.EArgumentError("unknown Range field %s in Marshal data", name)
		}
	}
	for _, name := range []string{"begin", "end", "excl"} {
		if !seen[name] {
			return oruby.Nil, oruby.EArgumentError("missing Range field %s in Marshal data", name)
		}
	}
	if _, err := r.mrb.Funcall(value, r.mrb.Intern("initialize"), begin, end, exclusive); err != nil {
		return oruby.Nil, err
	}
	return value, nil
}

func (r *reader) readException(value oruby.Value, n, depth int) (oruby.Value, error) {
	var message, backtrace oruby.Value
	hasMessage, hasBacktrace := false, false
	var variables []struct {
		name  oruby.MrbSym
		value oruby.Value
	}
	for i := 0; i < n; i++ {
		key, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if key.Type() != oruby.MrbTTSymbol {
			return oruby.Nil, oruby.EArgumentError("instance variable name is not a Symbol")
		}
		item, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		switch r.mrb.SymName(key.Symbol()) {
		case "mesg":
			if hasMessage {
				return oruby.Nil, oruby.EArgumentError("duplicate Exception message in Marshal data")
			}
			message, hasMessage = item, true
		case "bt":
			if hasBacktrace {
				return oruby.Nil, oruby.EArgumentError("duplicate Exception backtrace in Marshal data")
			}
			backtrace, hasBacktrace = item, true
		default:
			variables = append(variables, struct {
				name  oruby.MrbSym
				value oruby.Value
			}{key.Symbol(), item})
		}
	}
	if !hasMessage {
		return oruby.Nil, oruby.EArgumentError("missing Exception fields in Marshal data")
	}
	if err := r.mrb.SetExcMessage(value, message); err != nil {
		return oruby.Nil, err
	}
	if err := r.mrb.SetExcBacktrace(value, backtrace); err != nil {
		return oruby.Nil, err
	}
	for _, variable := range variables {
		if err := r.mrb.IVSet(value, variable.name, variable.value); err != nil {
			return oruby.Nil, err
		}
	}
	return value, nil
}

func (r *reader) readStruct(depth int) (oruby.Value, error) {
	name, err := r.readClassSymbol(depth)
	if err != nil {
		return oruby.Nil, err
	}
	klass, err := r.lookupConstant(name)
	if err != nil {
		return oruby.Nil, err
	}
	if klass.Type() != oruby.MrbTTClass {
		return oruby.Nil, oruby.ETypeError("%s is not a Struct class", name)
	}
	n, err := r.readCount()
	if err != nil {
		return oruby.Nil, err
	}
	args := make([]interface{}, n)
	for i := range args {
		args[i] = oruby.Nil
	}
	value, err := r.mrb.Funcall(klass, r.mrb.Intern("new"), args...)
	if err != nil {
		return oruby.Nil, err
	}
	r.addObject(value)
	for i := 0; i < n; i++ {
		member, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if member.Type() != oruby.MrbTTSymbol {
			return oruby.Nil, oruby.EArgumentError("Struct member name is not a Symbol")
		}
		item, err := r.readValue(depth + 1)
		if err != nil {
			return oruby.Nil, err
		}
		if _, err := r.mrb.Funcall(value, r.mrb.Intern("[]="), member, item); err != nil {
			return oruby.Nil, err
		}
	}
	return value, nil
}

func (r *reader) readMarshalLoad(depth int) (oruby.Value, error) {
	name, err := r.readClassSymbol(depth)
	if err != nil {
		return oruby.Nil, err
	}
	klass, err := r.lookupConstant(name)
	if err != nil {
		return oruby.Nil, err
	}
	value, err := r.mrb.Funcall(klass, r.mrb.Intern("allocate"))
	if err != nil {
		return oruby.Nil, err
	}
	link := r.addObject(value)
	payload, err := r.readValue(depth + 1)
	if err != nil {
		return oruby.Nil, err
	}
	if _, err := r.mrb.Funcall(value, r.mrb.Intern("marshal_load"), payload); err != nil {
		return oruby.Nil, err
	}
	r.objects[link] = value
	return value, nil
}

func (r *reader) readUserLoad() (oruby.Value, error) {
	name, err := r.readClassSymbol(0)
	if err != nil {
		return oruby.Nil, err
	}
	klass, err := r.lookupConstant(name)
	if err != nil {
		return oruby.Nil, err
	}
	n, err := r.readLength()
	if err != nil {
		return oruby.Nil, err
	}
	if err := r.checkReadBudget(n); err != nil {
		return oruby.Nil, err
	}
	data := make([]byte, n)
	if err := r.readFull(data); err != nil {
		return oruby.Nil, oruby.EArgumentError("marshal data too short")
	}
	value, err := r.mrb.Funcall(klass, r.mrb.Intern("_load"), r.mrb.BytesValue(data))
	if err != nil {
		return oruby.Nil, err
	}
	r.addObject(value)
	return value, nil
}

func (r *reader) readRegexp() (oruby.Value, error) {
	pattern, err := r.readBytes()
	if err != nil {
		return oruby.Nil, err
	}
	optionByte, err := r.readByte()
	if err != nil {
		return oruby.Nil, err
	}
	if !r.mrb.ClassDefined("Regexp") {
		return oruby.Nil, oruby.ETypeError("Regexp class is not available")
	}
	klass := r.mrb.ClassGet("Regexp")
	value, err := r.mrb.Funcall(klass, r.mrb.Intern("new"), r.mrb.BytesValue(pattern), int(optionByte))
	if err != nil {
		return oruby.Nil, err
	}
	r.addObject(value)
	return value, nil
}

func (r *reader) readClassSymbol(depth int) (string, error) {
	value, err := r.readValue(depth)
	if err != nil {
		return "", err
	}
	if value.Type() != oruby.MrbTTSymbol {
		return "", oruby.EArgumentError("class name is not a Symbol")
	}
	return r.mrb.SymName(value.Symbol()), nil
}

func (r *reader) lookupConstant(name string) (oruby.Value, error) {
	parts := strings.Split(name, "::")
	current := r.mrb.ObjectClass()
	var value oruby.Value
	for _, part := range parts {
		if part == "" {
			continue
		}
		sym := r.mrb.Intern(part)
		if !r.mrb.ConstDefined(current, sym) {
			return oruby.Nil, oruby.EArgumentError("undefined class/module %s", name)
		}
		value = r.mrb.ConstGet(current, sym)
		if value.Type() != oruby.MrbTTClass && value.Type() != oruby.MrbTTModule {
			return oruby.Nil, oruby.ETypeError("%s is not a class/module", name)
		}
		current = r.mrb.ClassPtr(value)
	}
	if value.IsNil() {
		return oruby.Nil, oruby.EArgumentError("undefined class/module %s", name)
	}
	return value, nil
}

func (r *reader) addObject(value oruby.Value) int {
	r.objects = append(r.objects, value)
	return len(r.objects) - 1
}
