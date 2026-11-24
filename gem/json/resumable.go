package json

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unsafe"

	"github.com/oruby/oruby"
)

var errIncomplete = errors.New("incomplete json")

type resumablePhase int

const (
	phaseArrFirst resumablePhase = iota
	phaseArrExpectValue
	phaseArrAfterValue
	phaseObjFirst
	phaseObjExpectKey
	phaseObjAfterKey
	phaseObjExpectValue
	phaseObjAfterValue
)

type resumableFrame struct {
	isObject  bool
	phase     resumablePhase
	elems     []oruby.Value
	keys      []oruby.Value
	vals      []oruby.Value
	seen      map[string]struct{}
	hasKey    bool
	curKey    oruby.Value
	curKeyStr string
	createCls string
}

type resumableParser struct {
	initialized bool
	inUse       bool
	opts        jsonParseOpts
	createID    string

	buf         []byte
	pos         int
	docBytes    int
	parsedBytes int
	inDoc       bool

	frames   []resumableFrame
	hasValue bool
	value    oruby.Value
}

func (rp *resumableParser) syncRoots(mrb *oruby.MrbState, self oruby.Value) {
	roots := mrb.AryNew()
	if !rp.opts.onLoad.IsNil() {
		roots.Push(rp.opts.onLoad)
	}
	if !rp.opts.objectClass.IsNil() {
		roots.Push(rp.opts.objectClass)
	}
	if !rp.opts.arrayClass.IsNil() {
		roots.Push(rp.opts.arrayClass)
	}
	if !rp.opts.decimalClass.IsNil() {
		roots.Push(rp.opts.decimalClass)
	}
	if rp.hasValue {
		roots.Push(rp.value)
	}
	for i := range rp.frames {
		f := &rp.frames[i]
		for _, e := range f.elems {
			roots.Push(e)
		}
		for _, k := range f.keys {
			roots.Push(k)
		}
		for _, v := range f.vals {
			roots.Push(v)
		}
		if f.hasKey {
			roots.Push(f.curKey)
		}
	}
	mrb.IVSet(self, mrb.Intern("@__roots"), roots.Value())
}

func (rp *resumableParser) compact() {
	if rp.pos == 0 {
		return
	}
	if rp.pos >= len(rp.buf) {
		rp.buf = rp.buf[:0]
		rp.pos = 0
		return
	}
	if rp.pos >= 256 && rp.pos*2 >= len(rp.buf) {
		rem := len(rp.buf) - rp.pos
		copy(rp.buf[:rem], rp.buf[rp.pos:])
		rp.buf = rp.buf[:rem]
		rp.pos = 0
	}
}

func (rp *resumableParser) advance(n int) {
	rp.pos += n
	if rp.inDoc {
		rp.docBytes += n
	}
}

func (rp *resumableParser) skipWS() error {
	for rp.pos < len(rp.buf) {
		b := rp.buf[rp.pos]
		switch b {
		case ' ', '\t', '\n', '\r':
			rp.advance(1)
		case '/':
			if !rp.opts.allowComments {
				return nil
			}
			if rp.pos+1 >= len(rp.buf) {
				return errIncomplete
			}
			next := rp.buf[rp.pos+1]
			if next == '/' {
				nlIdx := -1
				for i := rp.pos + 2; i < len(rp.buf); i++ {
					if rp.buf[i] == '\n' {
						nlIdx = i
						break
					}
				}
				if nlIdx < 0 {
					return errIncomplete
				}
				rp.advance(nlIdx - rp.pos + 1)
			} else if next == '*' {
				closeIdx := -1
				for i := rp.pos + 2; i+1 < len(rp.buf); i++ {
					if rp.buf[i] == '*' && rp.buf[i+1] == '/' {
						closeIdx = i + 2
						break
					}
				}
				if closeIdx < 0 {
					return errIncomplete
				}
				rp.advance(closeIdx - rp.pos)
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return nil
}

func (rp *resumableParser) maybeFreeze(mrb *oruby.MrbState, v oruby.Value) oruby.Value {
	if rp.opts.freeze {
		if v.IsString() {
			if fstr, err := mrb.Funcall(v, mrb.Intern("-@")); err == nil && !fstr.IsNil() {
				return fstr
			}
		}
		_, _ = mrb.Funcall(v, mrb.Intern("freeze"))
	}
	return v
}

func (rp *resumableParser) maybeOnLoad(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if !rp.opts.onLoad.IsNil() {
		res, err := mrb.Funcall(rp.opts.onLoad, mrb.Intern("call"), v)
		if err != nil {
			return mrb.NilValue(), &genError{err: err, isRubyExc: true}
		}
		return res, nil
	}
	return v, nil
}

func (rp *resumableParser) parseStringToken() (string, error) {
	startPos := rp.pos
	startDoc := rp.docBytes
	rollback := func() {
		rp.pos = startPos
		rp.docBytes = startDoc
	}
	if rp.pos >= len(rp.buf) {
		return "", errIncomplete
	}
	if rp.buf[rp.pos] != '"' {
		return "", fmt.Errorf("unexpected character: '%s'", errorSnippet(string(rp.buf), rp.pos))
	}
	i := rp.pos + 1
	segStart := i
	var sb strings.Builder
	hasEscape := false
	parseHex4At := func(idx int) (rune, int, error) {
		var r rune
		for j := 0; j < 4; j++ {
			if idx+j >= len(rp.buf) {
				return 0, idx, errIncomplete
			}
			c := rp.buf[idx+j]
			r <<= 4
			switch {
			case c >= '0' && c <= '9':
				r |= rune(c - '0')
			case c >= 'a' && c <= 'f':
				r |= rune(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				r |= rune(c - 'A' + 10)
			default:
				return 0, idx + j, errors.New("invalid hex digit in unicode escape")
			}
		}
		return r, idx + 4, nil
	}

	for i < len(rp.buf) {
		c := rp.buf[i]
		if c == '"' {
			var res string
			if !hasEscape {
				res = string(rp.buf[segStart:i])
			} else {
				sb.Write(rp.buf[segStart:i])
				res = sb.String()
			}
			rp.advance(i + 1 - startPos)
			return res, nil
		}
		if c < 0x20 && !rp.opts.allowControlCharacters {
			return "", errors.New("unescaped control character in string")
		}
		if c == '\\' {
			if !hasEscape {
				hasEscape = true
			}
			sb.Write(rp.buf[segStart:i])
			i++
			if i >= len(rp.buf) {
				rollback()
				return "", errIncomplete
			}
			esc := rp.buf[i]
			i++
			switch esc {
			case '"', '\\', '/':
				sb.WriteByte(esc)
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				r1, nextI, err := parseHex4At(i)
				if err != nil {
					if errors.Is(err, errIncomplete) {
						rollback()
					}
					return "", err
				}
				i = nextI
				if utf16.IsSurrogate(r1) {
					if r1 >= 0xDC00 {
						return "", errors.New("unexpected low surrogate")
					}
					if i >= len(rp.buf) {
						rollback()
						return "", errIncomplete
					}
					if rp.buf[i] != '\\' {
						return "", errors.New("incomplete surrogate pair")
					}
					if i+1 >= len(rp.buf) {
						rollback()
						return "", errIncomplete
					}
					if rp.buf[i+1] != 'u' {
						return "", errors.New("incomplete surrogate pair")
					}
					i += 2
					r2, nextI2, err := parseHex4At(i)
					if err != nil {
						if errors.Is(err, errIncomplete) {
							rollback()
						}
						return "", err
					}
					i = nextI2
					if r2 < 0xDC00 || r2 > 0xDFFF {
						return "", errors.New("invalid low surrogate")
					}
					sb.WriteRune(utf16.DecodeRune(r1, r2))
				} else {
					sb.WriteRune(r1)
				}
			default:
				if rp.opts.allowInvalidEscape && (esc >= 0x20 || rp.opts.allowControlCharacters) {
					sb.WriteByte(esc)
				} else {
					return "", fmt.Errorf("invalid escape character \\%c", esc)
				}
			}
			segStart = i
		} else {
			i++
		}
	}
	rollback()
	return "", errIncomplete
}

func (rp *resumableParser) matchKeyword(kw string) (bool, error) {
	rem := rp.buf[rp.pos:]
	n := len(kw)
	if len(rem) < n {
		for i := 0; i < len(rem); i++ {
			if rem[i] != kw[i] {
				return false, fmt.Errorf("unexpected token '%s'", errorSnippet(string(rp.buf), rp.pos))
			}
		}
		return false, errIncomplete
	}
	for i := 0; i < n; i++ {
		if rem[i] != kw[i] {
			return false, fmt.Errorf("unexpected token '%s'", errorSnippet(string(rp.buf), rp.pos))
		}
	}
	rp.advance(n)
	return true, nil
}

func (rp *resumableParser) parseNumberToken(mrb *oruby.MrbState) (oruby.Value, error) {
	startPos := rp.pos
	i := startPos
	if i < len(rp.buf) && rp.buf[i] == '-' {
		i++
	}
	if i >= len(rp.buf) {
		return mrb.NilValue(), errIncomplete
	}
	if rp.buf[i] == '0' {
		i++
		if i >= len(rp.buf) {
			return mrb.NilValue(), errIncomplete
		}
		if rp.buf[i] >= '0' && rp.buf[i] <= '9' {
			return mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(string(rp.buf), startPos))
		}
	} else if rp.buf[i] >= '1' && rp.buf[i] <= '9' {
		for i < len(rp.buf) && rp.buf[i] >= '0' && rp.buf[i] <= '9' {
			i++
		}
		if i >= len(rp.buf) {
			return mrb.NilValue(), errIncomplete
		}
	} else {
		return mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(string(rp.buf), startPos))
	}
	isFloat := false
	if i < len(rp.buf) && rp.buf[i] == '.' {
		isFloat = true
		i++
		if i >= len(rp.buf) {
			return mrb.NilValue(), errIncomplete
		}
		if rp.buf[i] < '0' || rp.buf[i] > '9' {
			return mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(string(rp.buf), startPos))
		}
		for i < len(rp.buf) && rp.buf[i] >= '0' && rp.buf[i] <= '9' {
			i++
		}
		if i >= len(rp.buf) {
			return mrb.NilValue(), errIncomplete
		}
	}
	if i < len(rp.buf) && (rp.buf[i] == 'e' || rp.buf[i] == 'E') {
		isFloat = true
		i++
		if i >= len(rp.buf) {
			return mrb.NilValue(), errIncomplete
		}
		if rp.buf[i] == '+' || rp.buf[i] == '-' {
			i++
			if i >= len(rp.buf) {
				return mrb.NilValue(), errIncomplete
			}
		}
		if rp.buf[i] < '0' || rp.buf[i] > '9' {
			return mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(string(rp.buf), startPos))
		}
		for i < len(rp.buf) && rp.buf[i] >= '0' && rp.buf[i] <= '9' {
			i++
		}
		if i >= len(rp.buf) {
			return mrb.NilValue(), errIncomplete
		}
	}

	raw := string(rp.buf[startPos:i])
	rp.advance(i - startPos)

	if isFloat {
		if !rp.opts.decimalClass.IsNil() {
			var v oruby.Value
			var err error
			if mrb.RespondTo(rp.opts.decimalClass, mrb.Intern("new")) {
				v, err = mrb.Funcall(rp.opts.decimalClass, mrb.Intern("new"), mrb.StrNew(raw))
			} else {
				v, err = mrb.Funcall(mrb.TopSelf(), mrb.Intern("BigDecimal"), mrb.StrNew(raw))
			}
			if err != nil {
				return mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
			return v, nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return mrb.NilValue(), err
		}
		return mrb.FloatValue(f), nil
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return mrb.Int64Value(n), nil
	}
	v, err := mrb.Funcall(mrb.StrNew(raw), mrb.Intern("to_i"))
	if err != nil {
		return mrb.NilValue(), err
	}
	return v, nil
}

func (rp *resumableParser) buildArray(mrb *oruby.MrbState, elems []oruby.Value) (oruby.Value, error) {
	var ary oruby.Value
	if !rp.opts.arrayClass.IsNil() {
		var err error
		ary, err = mrb.Funcall(rp.opts.arrayClass, mrb.Intern("new"))
		if err != nil {
			return mrb.NilValue(), &genError{err: err, isRubyExc: true}
		}
		for _, el := range elems {
			if _, err := mrb.Funcall(ary, mrb.Intern("<<"), el); err != nil {
				return mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
		}
	} else {
		a := mrb.AryNewCapa(len(elems))
		for _, el := range elems {
			a.Push(el)
		}
		ary = a.Value()
	}
	ary = rp.maybeFreeze(mrb, ary)
	return rp.maybeOnLoad(mrb, ary)
}

func (rp *resumableParser) buildObject(mrb *oruby.MrbState, keys, vals []oruby.Value, createCls string) (oruby.Value, error) {
	var obj oruby.Value
	custom := !rp.opts.objectClass.IsNil()
	if custom {
		var err error
		obj, err = mrb.Funcall(rp.opts.objectClass, mrb.Intern("new"))
		if err != nil {
			return mrb.NilValue(), &genError{err: err, isRubyExc: true}
		}
		for i := range keys {
			if _, err := mrb.Funcall(obj, mrb.Intern("[]="), keys[i], vals[i]); err != nil {
				return mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
		}
	} else {
		h := mrb.HashNew()
		for i := range keys {
			mrb.HashSet(h, keys[i], vals[i])
		}
		obj = h.Value()
	}
	if rp.opts.createAdditions && createCls != "" {
		klass, err := mrb.Eval(createCls)
		if err == nil && !klass.IsNil() && mrb.RespondTo(klass, mrb.Intern("json_create")) {
			created, cerr := mrb.Funcall(klass, mrb.Intern("json_create"), obj)
			if cerr != nil {
				return mrb.NilValue(), &genError{err: cerr, isRubyExc: true}
			}
			return rp.maybeOnLoad(mrb, created)
		}
	}
	obj = rp.maybeFreeze(mrb, obj)
	return rp.maybeOnLoad(mrb, obj)
}

func (rp *resumableParser) finishValue(mrb *oruby.MrbState, val oruby.Value, wasNumber bool) (bool, error) {
	if len(rp.frames) == 0 {
		rp.parsedBytes = rp.docBytes
		rp.inDoc = false
		if wasNumber {
			// Consume delimiter whitespace after a top-level number so eos? becomes true for "123 "
			for rp.pos < len(rp.buf) {
				b := rp.buf[rp.pos]
				if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
					rp.pos++
				} else {
					break
				}
			}
		}
		rp.value = val
		rp.hasValue = true
		rp.compact()
		return true, nil
	}
	top := &rp.frames[len(rp.frames)-1]
	if top.isObject {
		top.keys = append(top.keys, top.curKey)
		top.vals = append(top.vals, val)
		if rp.opts.createAdditions && top.curKeyStr == rp.createID && val.IsString() {
			top.createCls = mrb.String(val)
		}
		top.hasKey = false
		top.curKey = mrb.NilValue()
		top.curKeyStr = ""
		top.phase = phaseObjAfterValue
	} else {
		top.elems = append(top.elems, val)
		top.phase = phaseArrAfterValue
	}
	return false, nil
}

func (rp *resumableParser) parseNextValue(mrb *oruby.MrbState) (bool, error) {
	if rp.pos >= len(rp.buf) {
		return false, errIncomplete
	}
	c := rp.buf[rp.pos]
	switch c {
	case '[':
		openPos := rp.pos
		openDoc := rp.docBytes
		wasInDoc := rp.inDoc
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		rp.advance(1)
		if err := rp.skipWS(); err != nil {
			rp.pos = openPos
			rp.docBytes = openDoc
			rp.inDoc = wasInDoc
			return false, err
		}
		if rp.pos >= len(rp.buf) {
			rp.pos = openPos
			rp.docBytes = openDoc
			rp.inDoc = wasInDoc
			return false, errIncomplete
		}
		if len(rp.frames) >= rp.opts.maxNesting {
			return false, errNesting
		}
		if rp.buf[rp.pos] == ']' {
			rp.advance(1)
			ary, err := rp.buildArray(mrb, nil)
			if err != nil {
				return false, err
			}
			return rp.finishValue(mrb, ary, false)
		}
		rp.frames = append(rp.frames, resumableFrame{
			isObject: false,
			phase:    phaseArrExpectValue,
		})
		return false, nil

	case '{':
		openPos := rp.pos
		openDoc := rp.docBytes
		wasInDoc := rp.inDoc
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		rp.advance(1)
		if err := rp.skipWS(); err != nil {
			rp.pos = openPos
			rp.docBytes = openDoc
			rp.inDoc = wasInDoc
			return false, err
		}
		if rp.pos >= len(rp.buf) {
			rp.pos = openPos
			rp.docBytes = openDoc
			rp.inDoc = wasInDoc
			return false, errIncomplete
		}
		if len(rp.frames) >= rp.opts.maxNesting {
			return false, errNesting
		}
		if rp.buf[rp.pos] == '}' {
			rp.advance(1)
			obj, err := rp.buildObject(mrb, nil, nil, "")
			if err != nil {
				return false, err
			}
			return rp.finishValue(mrb, obj, false)
		}
		var seen map[string]struct{}
		if !rp.opts.allowDuplicateKey {
			seen = make(map[string]struct{})
		}
		rp.frames = append(rp.frames, resumableFrame{
			isObject: true,
			phase:    phaseObjExpectKey,
			seen:     seen,
		})
		return false, nil

	case '"':
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		s, err := rp.parseStringToken()
		if err != nil {
			return false, err
		}
		v := rp.maybeFreeze(mrb, mrb.StrNew(s))
		v, err = rp.maybeOnLoad(mrb, v)
		if err != nil {
			return false, err
		}
		return rp.finishValue(mrb, v, false)

	case 't':
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		if _, err := rp.matchKeyword("true"); err != nil {
			return false, err
		}
		v, err := rp.maybeOnLoad(mrb, mrb.TrueValue())
		if err != nil {
			return false, err
		}
		return rp.finishValue(mrb, v, false)

	case 'f':
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		if _, err := rp.matchKeyword("false"); err != nil {
			return false, err
		}
		v, err := rp.maybeOnLoad(mrb, mrb.FalseValue())
		if err != nil {
			return false, err
		}
		return rp.finishValue(mrb, v, false)

	case 'n':
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		if _, err := rp.matchKeyword("null"); err != nil {
			return false, err
		}
		v, err := rp.maybeOnLoad(mrb, mrb.NilValue())
		if err != nil {
			return false, err
		}
		return rp.finishValue(mrb, v, false)

	case 'N':
		if rp.opts.allowNaN {
			if !rp.inDoc {
				rp.inDoc = true
				rp.docBytes = 0
			}
			if _, err := rp.matchKeyword("NaN"); err != nil {
				return false, err
			}
			v, err := rp.maybeOnLoad(mrb, mrb.FloatValue(math.NaN()))
			if err != nil {
				return false, err
			}
			return rp.finishValue(mrb, v, false)
		}
		return false, fmt.Errorf("unexpected token '%s'", errorSnippet(string(rp.buf), rp.pos))

	case 'I':
		if rp.opts.allowNaN {
			if !rp.inDoc {
				rp.inDoc = true
				rp.docBytes = 0
			}
			if _, err := rp.matchKeyword("Infinity"); err != nil {
				return false, err
			}
			v, err := rp.maybeOnLoad(mrb, mrb.FloatValue(math.Inf(1)))
			if err != nil {
				return false, err
			}
			return rp.finishValue(mrb, v, false)
		}
		return false, fmt.Errorf("unexpected token '%s'", errorSnippet(string(rp.buf), rp.pos))

	case '-':
		if !rp.inDoc {
			rp.inDoc = true
			rp.docBytes = 0
		}
		if rp.opts.allowNaN && rp.pos+1 < len(rp.buf) && rp.buf[rp.pos+1] == 'I' {
			if _, err := rp.matchKeyword("-Infinity"); err != nil {
				return false, err
			}
			v, err := rp.maybeOnLoad(mrb, mrb.FloatValue(math.Inf(-1)))
			if err != nil {
				return false, err
			}
			return rp.finishValue(mrb, v, false)
		}
		v, err := rp.parseNumberToken(mrb)
		if err != nil {
			return false, err
		}
		v, err = rp.maybeOnLoad(mrb, v)
		if err != nil {
			return false, err
		}
		return rp.finishValue(mrb, v, true)

	default:
		if c >= '0' && c <= '9' {
			if !rp.inDoc {
				rp.inDoc = true
				rp.docBytes = 0
			}
			v, err := rp.parseNumberToken(mrb)
			if err != nil {
				return false, err
			}
			v, err = rp.maybeOnLoad(mrb, v)
			if err != nil {
				return false, err
			}
			return rp.finishValue(mrb, v, true)
		}
		return false, fmt.Errorf("unexpected character: '%s'", errorSnippet(string(rp.buf), rp.pos))
	}
}

func (rp *resumableParser) stepLoop(mrb *oruby.MrbState) (bool, error) {
	for {
		if err := rp.skipWS(); err != nil {
			return false, err
		}
		if len(rp.frames) == 0 {
			if rp.pos >= len(rp.buf) {
				rp.compact()
				return false, errIncomplete
			}
			done, err := rp.parseNextValue(mrb)
			if err != nil {
				return false, err
			}
			if done {
				return true, nil
			}
			continue
		}

		top := &rp.frames[len(rp.frames)-1]
		switch top.phase {
		case phaseArrExpectValue:
			if rp.pos >= len(rp.buf) {
				return false, errIncomplete
			}
			done, err := rp.parseNextValue(mrb)
			if err != nil {
				return false, err
			}
			if done {
				return true, nil
			}

		case phaseArrAfterValue:
			if rp.pos >= len(rp.buf) {
				return false, errIncomplete
			}
			b := rp.buf[rp.pos]
			if b == ']' {
				rp.advance(1)
				elems := top.elems
				rp.frames = rp.frames[:len(rp.frames)-1]
				ary, err := rp.buildArray(mrb, elems)
				if err != nil {
					return false, err
				}
				done, err := rp.finishValue(mrb, ary, false)
				if err != nil {
					return false, err
				}
				if done {
					return true, nil
				}
			} else if b == ',' {
				commaPos := rp.pos
				commaDoc := rp.docBytes
				rp.advance(1)
				if err := rp.skipWS(); err != nil {
					rp.pos = commaPos
					rp.docBytes = commaDoc
					return false, err
				}
				if rp.pos >= len(rp.buf) {
					rp.pos = commaPos
					rp.docBytes = commaDoc
					return false, errIncomplete
				}
				if rp.opts.allowTrailingComma && rp.buf[rp.pos] == ']' {
					rp.advance(1)
					elems := top.elems
					rp.frames = rp.frames[:len(rp.frames)-1]
					ary, err := rp.buildArray(mrb, elems)
					if err != nil {
						return false, err
					}
					done, err := rp.finishValue(mrb, ary, false)
					if err != nil {
						return false, err
					}
					if done {
						return true, nil
					}
				} else {
					top.phase = phaseArrExpectValue
				}
			} else {
				return false, fmt.Errorf("expected ',' or ']' after array value, got: '%s'", errorSnippet(string(rp.buf), rp.pos))
			}

		case phaseObjExpectKey:
			if rp.pos >= len(rp.buf) {
				return false, errIncomplete
			}
			if rp.buf[rp.pos] != '"' {
				return false, fmt.Errorf("expected object key, got: '%s'", errorSnippet(string(rp.buf), rp.pos))
			}
			kstr, err := rp.parseStringToken()
			if err != nil {
				return false, err
			}
			var kval oruby.Value
			if rp.opts.symbolizeNames {
				kval = mrb.SymbolValue(mrb.Intern(kstr))
			} else {
				kval = rp.maybeFreeze(mrb, mrb.StrNew(kstr))
			}
			if !rp.opts.onLoad.IsNil() {
				kval, err = rp.maybeOnLoad(mrb, kval)
				if err != nil {
					return false, err
				}
			}
			top.curKey = kval
			top.curKeyStr = kstr
			top.hasKey = true
			if top.seen != nil {
				if _, dup := top.seen[kstr]; dup {
					return false, fmt.Errorf("duplicate key %q", kstr)
				}
				top.seen[kstr] = struct{}{}
			}
			top.phase = phaseObjAfterKey

		case phaseObjAfterKey:
			if rp.pos >= len(rp.buf) {
				return false, errIncomplete
			}
			if rp.buf[rp.pos] != ':' {
				return false, errors.New("expected ':' after object key")
			}
			rp.advance(1)
			top.phase = phaseObjExpectValue

		case phaseObjExpectValue:
			if rp.pos >= len(rp.buf) {
				return false, errIncomplete
			}
			done, err := rp.parseNextValue(mrb)
			if err != nil {
				return false, err
			}
			if done {
				return true, nil
			}

		case phaseObjAfterValue:
			if rp.pos >= len(rp.buf) {
				return false, errIncomplete
			}
			b := rp.buf[rp.pos]
			if b == '}' {
				rp.advance(1)
				keys, vals, createCls := top.keys, top.vals, top.createCls
				rp.frames = rp.frames[:len(rp.frames)-1]
				obj, err := rp.buildObject(mrb, keys, vals, createCls)
				if err != nil {
					return false, err
				}
				done, err := rp.finishValue(mrb, obj, false)
				if err != nil {
					return false, err
				}
				if done {
					return true, nil
				}
			} else if b == ',' {
				commaPos := rp.pos
				commaDoc := rp.docBytes
				rp.advance(1)
				if err := rp.skipWS(); err != nil {
					rp.pos = commaPos
					rp.docBytes = commaDoc
					return false, err
				}
				if rp.pos >= len(rp.buf) {
					rp.pos = commaPos
					rp.docBytes = commaDoc
					return false, errIncomplete
				}
				if rp.opts.allowTrailingComma && rp.buf[rp.pos] == '}' {
					rp.advance(1)
					keys, vals, createCls := top.keys, top.vals, top.createCls
					rp.frames = rp.frames[:len(rp.frames)-1]
					obj, err := rp.buildObject(mrb, keys, vals, createCls)
					if err != nil {
						return false, err
					}
					done, err := rp.finishValue(mrb, obj, false)
					if err != nil {
						return false, err
					}
					if done {
						return true, nil
					}
				} else {
					top.phase = phaseObjExpectKey
				}
			} else {
				return false, fmt.Errorf("expected ',' or '}' after object value, got: '%s'", errorSnippet(string(rp.buf), rp.pos))
			}
		}
	}
}

func (rp *resumableParser) pathValues(mrb *oruby.MrbState) oruby.Value {
	ary := mrb.AryNewCapa(len(rp.frames))
	for i := range rp.frames {
		f := &rp.frames[i]
		if f.isObject {
			if f.hasKey {
				ary.Push(f.curKey)
			}
		} else {
			ary.Push(mrb.FixnumValue(len(f.elems)))
		}
	}
	return ary.Value()
}

func (rp *resumableParser) buildPartialValue(mrb *oruby.MrbState) (oruby.Value, error) {
	if len(rp.frames) == 0 {
		return mrb.NilValue(), nil
	}
	hasChild := false
	childVal := mrb.NilValue()
	for i := len(rp.frames) - 1; i >= 0; i-- {
		f := &rp.frames[i]
		if f.isObject {
			var keys []oruby.Value
			var vals []oruby.Value
			if f.hasKey {
				keys = make([]oruby.Value, len(f.keys)+1)
				vals = make([]oruby.Value, len(f.vals)+1)
				copy(keys, f.keys)
				copy(vals, f.vals)
				keys[len(f.keys)] = f.curKey
				if hasChild {
					vals[len(f.vals)] = childVal
				} else {
					vals[len(f.vals)] = mrb.NilValue()
				}
			} else {
				keys = f.keys
				vals = f.vals
			}
			obj, err := rp.buildObject(mrb, keys, vals, "")
			if err != nil {
				return mrb.NilValue(), err
			}
			childVal = obj
			hasChild = true
		} else {
			elems := f.elems
			if hasChild {
				tmp := make([]oruby.Value, len(f.elems)+1)
				copy(tmp, f.elems)
				tmp[len(f.elems)] = childVal
				elems = tmp
			}
			ary, err := rp.buildArray(mrb, elems)
			if err != nil {
				return mrb.NilValue(), err
			}
			childVal = ary
			hasChild = true
		}
	}
	return childVal, nil
}

func getResumableParser(mrb *oruby.MrbState, self oruby.Value) *resumableParser {
	p, ok := mrb.Data(self).(*resumableParser)
	if !ok || p == nil {
		return nil
	}
	return p
}

func initResumableParser(mrb *oruby.MrbState, jsonMod oruby.RClass) {
	cls := mrb.DefineClassUnder(jsonMod, "ResumableParser", mrb.ObjectClass())
	oruby.MrbSetInstanceTT(cls, oruby.MrbTTCData)
	cls.DefineMethod("initialize", mrbResumableInitialize, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))
	cls.DefineMethod("<<", mrbResumableFeed, mrb.ArgsReq(1))
	cls.DefineMethod("parse", mrbResumableParse, mrb.ArgsNone())
	cls.DefineMethod("value", mrbResumableValue, mrb.ArgsNone())
	cls.DefineMethod("value?", mrbResumableValueP, mrb.ArgsNone())
	cls.DefineMethod("clear", mrbResumableClear, mrb.ArgsNone())
	cls.DefineMethod("rest", mrbResumableRest, mrb.ArgsNone())
	cls.DefineMethod("eos?", mrbResumableEOSP, mrb.ArgsNone())
	cls.DefineMethod("empty?", mrbResumableEmptyP, mrb.ArgsNone())
	cls.DefineMethod("partial_value", mrbResumablePartialValue, mrb.ArgsNone())
	cls.DefineMethod("partial_value?", mrbResumablePartialValueP, mrb.ArgsNone())
	cls.DefineMethod("parsed_bytes", mrbResumableParsedBytes, mrb.ArgsNone())
}

func mrbResumableInitialize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if existing := getResumableParser(mrb, self); existing != nil && existing.initialized {
		return mrb.Raise(mrb.ETypeError(), "already initialized")
	}
	args := mrb.GetArgs()
	var optsVal oruby.Value
	if args.Len() > 0 && !args.Item(0).IsNil() {
		optsVal = args.Item(0)
	} else if kw := mrb.KeywordArgs().Value(); !kw.IsNil() {
		optsVal = kw
	}
	var validated oruby.Value
	if !optsVal.IsNil() {
		parserCls := mrb.ClassGetUnder(mrb.ModuleGet("JSON"), "Parser")
		v, err := mrb.Funcall(parserCls, mrb.Intern("validate_opts"), optsVal)
		if err != nil {
			return mrb.RaiseError(err)
		}
		validated = v
	}
	opts := jsonParseOpts{
		maxNesting:        defaultMaxNesting,
		allowDuplicateKey: false,
	}
	if !validated.IsNil() && validated.IsHash() {
		getOpt := func(name string) (oruby.Value, bool) {
			sym := mrb.SymbolValue(mrb.Intern(name))
			if mrb.HashKeyP(validated, sym) {
				return mrb.HashGet(validated, sym), true
			}
			return mrb.NilValue(), false
		}
		if v, ok := getOpt("max_nesting"); ok {
			if v.IsNil() || v.Type() == oruby.MrbTTFalse {
				opts.maxNesting = hardMaxNesting
			} else if v.IsInt() {
				n := int(v.Int64())
				if n <= 0 || n > hardMaxNesting {
					opts.maxNesting = hardMaxNesting
				} else {
					opts.maxNesting = n
				}
			}
		}
		if v, ok := getOpt("symbolize_names"); ok {
			opts.symbolizeNames = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("freeze"); ok {
			opts.freeze = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_nan"); ok {
			opts.allowNaN = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_trailing_comma"); ok {
			opts.allowTrailingComma = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_duplicate_key"); ok {
			opts.allowDuplicateKey = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_comments"); ok {
			opts.allowComments = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_control_characters"); ok {
			opts.allowControlCharacters = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_invalid_escape"); ok {
			opts.allowInvalidEscape = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("allow_invalid_escapes"); ok && oruby.MrbBoolean(v) {
			opts.allowInvalidEscape = true
		}
		if v, ok := getOpt("create_additions"); ok {
			opts.createAdditions = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("object_class"); ok && !v.IsNil() {
			opts.objectClass = v
		}
		if v, ok := getOpt("array_class"); ok && !v.IsNil() {
			opts.arrayClass = v
		}
		if v, ok := getOpt("decimal_class"); ok && !v.IsNil() {
			opts.decimalClass = v
		}
		if v, ok := getOpt("on_load"); ok && !v.IsNil() && v.Type() != oruby.MrbTTFalse {
			opts.onLoad = v
		}
	}
	createID := "json_class"
	if opts.createAdditions {
		if cid, cerr := mrb.Funcall(mrb.ModuleGet("JSON"), mrb.Intern("create_id")); cerr == nil && cid.IsString() {
			createID = mrb.String(cid)
		}
	}
	rp := &resumableParser{
		initialized: true,
		opts:        opts,
		createID:    createID,
		value:       mrb.NilValue(),
	}
	mrb.DataSetInterface(self, rp)
	rp.syncRoots(mrb, self)
	return self
}

func mrbResumableFeed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized ResumableParser")
	}
	if rp.inUse {
		return mrb.Raise(mrb.EArgumentError(), "ResumableParser can't be used recursively")
	}
	arg := mrb.GetArgsFirst()
	if arg.Type() != oruby.MrbTTString {
		if mrb.RespondTo(arg, mrb.Intern("to_str")) {
			sv, err := mrb.Funcall(arg, mrb.Intern("to_str"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !sv.IsString() {
				return mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
			}
			arg = sv
		} else {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("no implicit conversion of %s into String", mrb.ClassOf(arg).Name()))
		}
	}
	rp.compact()
	ptr, n := oruby.RStringMem(arg)
	if n > 0 && ptr != nil {
		rp.buf = append(rp.buf, unsafe.Slice(ptr, n)...)
	}
	return self
}

func mrbResumableParse(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized ResumableParser")
	}
	if rp.inUse {
		return mrb.Raise(mrb.EArgumentError(), "ResumableParser can't be used recursively")
	}
	rp.inUse = true
	defer func() {
		rp.inUse = false
		rp.syncRoots(mrb, self)
	}()

	done, err := rp.stepLoop(mrb)
	if err != nil {
		if errors.Is(err, errIncomplete) {
			if rp.inDoc {
				rp.parsedBytes = rp.docBytes + (len(rp.buf) - rp.pos)
			}
			return mrb.FalseValue()
		}
		var ge *genError
		if errors.As(err, &ge) && ge.isRubyExc {
			return mrb.RaiseError(ge.err)
		}
		if errors.Is(err, errNesting) {
			return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "nesting of %d is too deep", rp.opts.maxNesting+1)
		}
		cls := jsonErrorClass(mrb, "ParserError")
		exc, ferr := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(err.Error()))
		if ferr == nil && !exc.IsNil() {
			mrb.IVSet(exc, mrb.Intern("@line"), mrb.FixnumValue(0))
			mrb.IVSet(exc, mrb.Intern("@column"), mrb.FixnumValue(0))
			mrb.IVSet(exc, mrb.Intern("@json_path"), rp.pathValues(mrb))
			mrb.ExcRaise(exc)
			return mrb.NilValue()
		}
		return mrb.Raisef(cls, "%s", err.Error())
	}
	return mrb.BoolValue(done)
}

func mrbResumableValue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil || !rp.hasValue {
		return mrb.Raise(mrb.EArgumentError(), "no value available")
	}
	v := rp.value
	rp.hasValue = false
	rp.value = mrb.NilValue()
	rp.syncRoots(mrb, self)
	return v
}

func mrbResumableValueP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.FalseValue()
	}
	return mrb.BoolValue(rp.hasValue)
}

func mrbResumableClear(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return self
	}
	if rp.inUse {
		return mrb.Raise(mrb.EArgumentError(), "ResumableParser can't be used recursively")
	}
	rp.buf = rp.buf[:0]
	rp.pos = 0
	rp.docBytes = 0
	rp.parsedBytes = 0
	rp.inDoc = false
	rp.frames = rp.frames[:0]
	rp.hasValue = false
	rp.value = mrb.NilValue()
	rp.syncRoots(mrb, self)
	return self
}

func mrbResumableRest(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil || rp.pos >= len(rp.buf) {
		return mrb.StrNew("")
	}
	return mrb.BytesValue(rp.buf[rp.pos:])
}

func mrbResumableEOSP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(rp.pos >= len(rp.buf))
}

func mrbResumableEmptyP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(!rp.hasValue && len(rp.frames) == 0 && rp.pos >= len(rp.buf))
}

func mrbResumablePartialValueP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.FalseValue()
	}
	return mrb.BoolValue(len(rp.frames) > 0)
}

func mrbResumablePartialValue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.NilValue()
	}
	if rp.inUse {
		return mrb.Raise(mrb.EArgumentError(), "ResumableParser can't be used recursively")
	}
	rp.inUse = true
	defer func() { rp.inUse = false }()
	v, err := rp.buildPartialValue(mrb)
	if err != nil {
		var ge *genError
		if errors.As(err, &ge) && ge.isRubyExc {
			return mrb.RaiseError(ge.err)
		}
		return mrb.Raise(jsonErrorClass(mrb, "ParserError"), err.Error())
	}
	return v
}

func mrbResumableParsedBytes(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rp := getResumableParser(mrb, self)
	if rp == nil {
		return mrb.FixnumValue(0)
	}
	return mrb.FixnumValue(rp.parsedBytes)
}
