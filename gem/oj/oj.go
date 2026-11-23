// Package oj provides a pure-Go implementation of the Ruby Oj (Optimized JSON) gem.
package oj

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/oruby/oruby"
)

func init() {
	oruby.LazyGem("oj", initOj)
}

type ojMode int

const (
	modeCompat ojMode = iota
	modeStrict
	modeRails
	modeObject
	modeNull
)

type escapeMode int

const (
	escJSON escapeMode = iota
	escXSSSafe
	escASCII
)

type ojOpts struct {
	mode        ojMode
	symbolKeys  bool
	indent      string
	escape      escapeMode
	allowNil    bool
	emptyString bool
	useAsJSON   bool
	useToJSON   bool
	timeFormat  string
	maxDepth    int
}

func defaultOjOpts() ojOpts {
	return ojOpts{
		mode:       modeCompat,
		escape:     escJSON,
		allowNil:   true,
		useAsJSON:  true,
		useToJSON:  true,
		timeFormat: "xmlschema",
		maxDepth:   100,
	}
}

func parseOjOpts(mrb *oruby.MrbState, base ojOpts, h oruby.Value) ojOpts {
	if h.IsNil() || !h.IsHash() {
		return base
	}
	get := func(k string) (oruby.Value, bool) {
		if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern(k).Value()); err == nil && !v.IsNil() {
			return v, true
		}
		if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.StrNew(k)); err == nil && !v.IsNil() {
			return v, true
		}
		return mrb.NilValue(), false
	}
	if v, ok := get("mode"); ok {
		switch mrb.String(v) {
		case "strict":
			base.mode = modeStrict
			base.useAsJSON = false
			base.useToJSON = false
		case "compat":
			base.mode = modeCompat
		case "rails":
			base.mode = modeRails
			base.useAsJSON = true
			base.escape = escXSSSafe
		case "object":
			base.mode = modeObject
		case "null":
			base.mode = modeNull
		}
	}
	if v, ok := get("symbol_keys"); ok {
		base.symbolKeys = v.Bool()
	}
	if v, ok := get("indent"); ok {
		if v.IsInt() {
			n := int(v.Int64())
			if n > 0 {
				base.indent = strings.Repeat(" ", n)
			} else {
				base.indent = ""
			}
		} else if v.IsString() {
			base.indent = v.String()
		}
	}
	if v, ok := get("escape_mode"); ok {
		switch mrb.String(v) {
		case "xss_safe":
			base.escape = escXSSSafe
		case "ascii":
			base.escape = escASCII
		case "json", "slash":
			base.escape = escJSON
		}
	}
	if v, ok := get("allow_nil"); ok {
		base.allowNil = v.Bool()
	}
	if v, ok := get("empty_string"); ok {
		base.emptyString = v.Bool()
	}
	if v, ok := get("use_as_json"); ok {
		base.useAsJSON = v.Bool()
	}
	if v, ok := get("use_to_json"); ok {
		base.useToJSON = v.Bool()
	}
	if v, ok := get("time_format"); ok {
		base.timeFormat = mrb.String(v)
	}
	return base
}

func currentDefaultOpts(mrb *oruby.MrbState, mod oruby.RClass) ojOpts {
	o := defaultOjOpts()
	h := mrb.IVGet(mod, mrb.Intern("@default_options"))
	if !h.IsNil() && h.IsHash() {
		o = parseOjOpts(mrb, o, h)
	}
	return o
}

func collectOpts(mrb *oruby.MrbState, mod oruby.RClass, base ojOpts, args oruby.RArgs, optIdx int) ojOpts {
	o := base
	if args.Len() > optIdx && args.Item(optIdx).IsHash() {
		o = parseOjOpts(mrb, o, args.Item(optIdx))
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
		o = parseOjOpts(mrb, o, kw)
	}
	return o
}

// ---------------------------------------------------------------------------
// JSON Parser (directly into oruby.Value)
// ---------------------------------------------------------------------------

type jsonParser struct {
	mrb  *oruby.MrbState
	s    string
	pos  int
	opts ojOpts
}

func (p *jsonParser) skipWS() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) err(msg string) error {
	line := 1
	col := 1
	for i := 0; i < p.pos && i < len(p.s); i++ {
		if p.s[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return fmt.Errorf("%s at line %d, column %d", msg, line, col)
}

func (p *jsonParser) parseValue(depth int) (oruby.Value, error) {
	if depth > p.opts.maxDepth {
		return p.mrb.NilValue(), p.err("nesting too deep")
	}
	p.skipWS()
	if p.pos >= len(p.s) {
		return p.mrb.NilValue(), p.err("unexpected end of input")
	}
	switch p.s[p.pos] {
	case '{':
		return p.parseObject(depth + 1)
	case '[':
		return p.parseArray(depth + 1)
	case '"':
		str, err := p.parseString()
		if err != nil {
			return p.mrb.NilValue(), err
		}
		return p.mrb.StrNew(str), nil
	case 't':
		if strings.HasPrefix(p.s[p.pos:], "true") {
			p.pos += 4
			return p.mrb.TrueValue(), nil
		}
		return p.mrb.NilValue(), p.err("invalid token")
	case 'f':
		if strings.HasPrefix(p.s[p.pos:], "false") {
			p.pos += 5
			return p.mrb.FalseValue(), nil
		}
		return p.mrb.NilValue(), p.err("invalid token")
	case 'n':
		if strings.HasPrefix(p.s[p.pos:], "null") {
			p.pos += 4
			return p.mrb.NilValue(), nil
		}
		return p.mrb.NilValue(), p.err("invalid token")
	default:
		if p.s[p.pos] == '-' || (p.s[p.pos] >= '0' && p.s[p.pos] <= '9') {
			return p.parseNumber()
		}
		return p.mrb.NilValue(), p.err(fmt.Sprintf("unexpected character %q", p.s[p.pos]))
	}
}

func (p *jsonParser) parseObject(depth int) (oruby.Value, error) {
	p.pos++ // skip '{'
	h := p.mrb.HashNew()
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return h.Value(), nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.s) || p.s[p.pos] != '"' {
			return p.mrb.NilValue(), p.err("expected object key string")
		}
		key, err := p.parseString()
		if err != nil {
			return p.mrb.NilValue(), err
		}
		p.skipWS()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			return p.mrb.NilValue(), p.err("expected ':' after object key")
		}
		p.pos++
		val, err := p.parseValue(depth)
		if err != nil {
			return p.mrb.NilValue(), err
		}
		var kVal oruby.Value
		if p.opts.symbolKeys {
			kVal = p.mrb.Intern(key).Value()
		} else {
			kVal = p.mrb.StrNew(key)
		}
		p.mrb.HashSet(h, kVal, val)
		p.skipWS()
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), p.err("unterminated object")
		}
		if p.s[p.pos] == '}' {
			p.pos++
			return h.Value(), nil
		}
		if p.s[p.pos] != ',' {
			return p.mrb.NilValue(), p.err("expected ',' or '}' in object")
		}
		p.pos++
	}
}

func (p *jsonParser) parseArray(depth int) (oruby.Value, error) {
	p.pos++ // skip '['
	ary := p.mrb.AryNew()
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return ary.Value(), nil
	}
	for {
		val, err := p.parseValue(depth)
		if err != nil {
			return p.mrb.NilValue(), err
		}
		p.mrb.AryPush(ary, val)
		p.skipWS()
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), p.err("unterminated array")
		}
		if p.s[p.pos] == ']' {
			p.pos++
			return ary.Value(), nil
		}
		if p.s[p.pos] != ',' {
			return p.mrb.NilValue(), p.err("expected ',' or ']' in array")
		}
		p.pos++
	}
}

func (p *jsonParser) parseString() (string, error) {
	p.pos++ // skip '"'
	start := p.pos
	// Fast path: no escapes
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '"' {
			res := p.s[start:p.pos]
			p.pos++
			return res, nil
		}
		if c == '\\' || c < 0x20 {
			break
		}
		p.pos++
	}
	var b strings.Builder
	b.WriteString(p.s[start:p.pos])
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '"' {
			p.pos++
			return b.String(), nil
		}
		if c < 0x20 {
			return "", p.err("unescaped control character in string")
		}
		if c != '\\' {
			b.WriteByte(c)
			p.pos++
			continue
		}
		p.pos++
		if p.pos >= len(p.s) {
			return "", p.err("unterminated escape sequence")
		}
		esc := p.s[p.pos]
		p.pos++
		switch esc {
		case '"', '\\', '/':
			b.WriteByte(esc)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			if p.pos+4 > len(p.s) {
				return "", p.err("incomplete \\u escape")
			}
			v1, err := strconv.ParseUint(p.s[p.pos:p.pos+4], 16, 16)
			if err != nil {
				return "", p.err("invalid \\u hex escape")
			}
			p.pos += 4
			r := rune(v1)
			if utf16.IsSurrogate(r) && p.pos+6 <= len(p.s) && p.s[p.pos] == '\\' && p.s[p.pos+1] == 'u' {
				if v2, err2 := strconv.ParseUint(p.s[p.pos+2:p.pos+6], 16, 16); err2 == nil {
					dec := utf16.DecodeRune(r, rune(v2))
					if dec != utf8.RuneError {
						p.pos += 6
						r = dec
					}
				}
			}
			b.WriteRune(r)
		default:
			return "", p.err(fmt.Sprintf("invalid escape \\%c", esc))
		}
	}
	return "", p.err("unterminated string")
}

func (p *jsonParser) parseNumber() (oruby.Value, error) {
	start := p.pos
	if p.s[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.s) {
		return p.mrb.NilValue(), p.err("invalid number")
	}
	if p.s[p.pos] == '0' {
		p.pos++
	} else if p.s[p.pos] >= '1' && p.s[p.pos] <= '9' {
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return p.mrb.NilValue(), p.err("invalid number")
	}
	isFloat := false
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		isFloat = true
		p.pos++
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			return p.mrb.NilValue(), p.err("invalid fraction in number")
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.s) && (p.s[p.pos] == 'e' || p.s[p.pos] == 'E') {
		isFloat = true
		p.pos++
		if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			return p.mrb.NilValue(), p.err("invalid exponent in number")
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	raw := p.s[start:p.pos]
	if !isFloat {
		if iv, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return p.mrb.Int64Value(iv), nil
		}
		// Big integer fallback via Ruby String#to_i
		if bi, err := p.mrb.Funcall(p.mrb.StrNew(raw), oruby.Sym_to_i); err == nil {
			return bi, nil
		}
	}
	fv, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return p.mrb.NilValue(), p.err("invalid float number")
	}
	return p.mrb.FloatValue(fv), nil
}

// ---------------------------------------------------------------------------
// JSON Encoder (directly from oruby.Value)
// ---------------------------------------------------------------------------

type jsonEncoder struct {
	mrb  *oruby.MrbState
	buf  bytes.Buffer
	opts ojOpts
}

func (e *jsonEncoder) writeIndent(depth int) {
	if e.opts.indent == "" {
		return
	}
	e.buf.WriteByte('\n')
	for i := 0; i < depth; i++ {
		e.buf.WriteString(e.opts.indent)
	}
}

func (e *jsonEncoder) writeString(s string) {
	e.buf.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x80 {
			switch c {
			case '"':
				e.buf.WriteString(`\"`)
			case '\\':
				e.buf.WriteString(`\\`)
			case '\b':
				e.buf.WriteString(`\b`)
			case '\f':
				e.buf.WriteString(`\f`)
			case '\n':
				e.buf.WriteString(`\n`)
			case '\r':
				e.buf.WriteString(`\r`)
			case '\t':
				e.buf.WriteString(`\t`)
			case '<':
				if e.opts.escape == escXSSSafe {
					e.buf.WriteString(`\u003c`)
				} else {
					e.buf.WriteByte(c)
				}
			case '>':
				if e.opts.escape == escXSSSafe {
					e.buf.WriteString(`\u003e`)
				} else {
					e.buf.WriteByte(c)
				}
			case '&':
				if e.opts.escape == escXSSSafe {
					e.buf.WriteString(`\u0026`)
				} else {
					e.buf.WriteByte(c)
				}
			default:
				if c < 0x20 {
					fmt.Fprintf(&e.buf, `\u%04x`, c)
				} else {
					e.buf.WriteByte(c)
				}
			}
			i++
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		if e.opts.escape == escASCII || (e.opts.escape == escXSSSafe && (r == '\u2028' || r == '\u2029')) {
			if r <= 0xFFFF {
				fmt.Fprintf(&e.buf, `\u%04x`, r)
			} else {
				r1, r2 := utf16.EncodeRune(r)
				fmt.Fprintf(&e.buf, `\u%04x\u%04x`, r1, r2)
			}
		} else {
			e.buf.WriteString(s[i : i+sz])
		}
		i += sz
	}
	e.buf.WriteByte('"')
}

func (e *jsonEncoder) encode(v oruby.Value, depth int) error {
	if depth > e.opts.maxDepth {
		return fmt.Errorf("nesting too deep")
	}
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			e.buf.WriteString("null")
		} else {
			e.buf.WriteString("false")
		}
		return nil
	case oruby.MrbTTTrue:
		e.buf.WriteString("true")
		return nil
	case oruby.MrbTTInteger:
		e.buf.WriteString(strconv.FormatInt(v.Int64(), 10))
		return nil
	case oruby.MrbTTFloat:
		f := v.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			if e.opts.mode == modeStrict {
				return fmt.Errorf("cannot dump NaN or Infinity in strict mode")
			}
			e.buf.WriteString("null")
			return nil
		}
		e.buf.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		return nil
	case oruby.MrbTTString:
		e.writeString(v.String())
		return nil
	case oruby.MrbTTSymbol:
		if e.opts.mode == modeStrict {
			return fmt.Errorf("cannot dump Symbol in strict mode")
		}
		e.writeString(e.mrb.SymName(v.Symbol()))
		return nil
	case oruby.MrbTTArray:
		n := oruby.RArrayLen(v)
		if n == 0 {
			e.buf.WriteString("[]")
			return nil
		}
		e.buf.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				e.buf.WriteByte(',')
			}
			e.writeIndent(depth + 1)
			if err := e.encode(e.mrb.AryRef(v, i), depth+1); err != nil {
				return err
			}
		}
		e.writeIndent(depth)
		e.buf.WriteByte(']')
		return nil
	case oruby.MrbTTHash:
		keys := e.mrb.HashKeys(v)
		n := oruby.RArrayLen(keys)
		if n == 0 {
			e.buf.WriteString("{}")
			return nil
		}
		e.buf.WriteByte('{')
		for i := 0; i < n; i++ {
			if i > 0 {
				e.buf.WriteByte(',')
			}
			e.writeIndent(depth + 1)
			k := e.mrb.AryRef(keys, i)
			if e.opts.mode == modeStrict && !k.IsString() {
				return fmt.Errorf("non-string Hash key not allowed in strict mode")
			}
			var ks string
			if k.IsString() {
				ks = k.String()
			} else if k.IsSymbol() {
				ks = e.mrb.SymName(k.Symbol())
			} else {
				ks = e.mrb.String(k)
			}
			e.writeString(ks)
			if e.opts.indent != "" {
				e.buf.WriteString(": ")
			} else {
				e.buf.WriteByte(':')
			}
			val, err := e.mrb.Funcall(v, e.mrb.Intern("[]"), k)
			if err != nil {
				return err
			}
			if err := e.encode(val, depth+1); err != nil {
				return err
			}
		}
		e.writeIndent(depth)
		e.buf.WriteByte('}')
		return nil
	default:
		if e.opts.mode == modeStrict {
			return fmt.Errorf("cannot dump %s in strict mode", e.mrb.ClassOf(v).Name())
		}
		if e.opts.mode == modeNull {
			e.buf.WriteString("null")
			return nil
		}
		clsName := e.mrb.ClassOf(v).Name()
		if clsName == "BigInt" || clsName == "Integer" {
			if sv, err := e.mrb.Funcall(v, oruby.Sym_to_s); err == nil {
				e.buf.WriteString(sv.String())
				return nil
			}
		}
		if clsName == "Time" {
			switch e.opts.timeFormat {
			case "unix", "unix_zone":
				if fv, err := e.mrb.Funcall(v, e.mrb.Intern("to_f")); err == nil {
					e.buf.WriteString(strconv.FormatFloat(fv.Float64(), 'f', -1, 64))
					return nil
				}
			case "xmlschema":
				if e.mrb.RespondTo(v, e.mrb.Intern("iso8601")) {
					if sv, err := e.mrb.Funcall(v, e.mrb.Intern("iso8601")); err == nil {
						e.writeString(sv.String())
						return nil
					}
				}
			}
			if sv, err := e.mrb.Funcall(v, oruby.Sym_to_s); err == nil {
				e.writeString(sv.String())
				return nil
			}
		}
		if e.opts.useAsJSON && e.mrb.RespondTo(v, e.mrb.Intern("as_json")) {
			if aj, err := e.mrb.Funcall(v, e.mrb.Intern("as_json")); err == nil && aj != v {
				return e.encode(aj, depth+1)
			}
		}
		if e.opts.useToJSON && e.mrb.RespondTo(v, e.mrb.Intern("to_json")) {
			if tj, err := e.mrb.Funcall(v, e.mrb.Intern("to_json")); err == nil && tj.IsString() {
				e.buf.WriteString(tj.String())
				return nil
			}
		}
		if e.mrb.RespondTo(v, e.mrb.Intern("to_h")) {
			if hv, err := e.mrb.Funcall(v, e.mrb.Intern("to_h")); err == nil && hv.IsHash() {
				return e.encode(hv, depth+1)
			}
		}
		if sv, err := e.mrb.Funcall(v, oruby.Sym_to_s); err == nil {
			e.writeString(sv.String())
			return nil
		}
		e.buf.WriteString("null")
		return nil
	}
}

func initOj(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Oj")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("3.16.7"))

	stdErr := mrb.ExcGet("StandardError")
	parseErr := mrb.DefineClassUnder(mod, "ParseError", stdErr)
	mrb.DefineClassUnder(mod, "Error", stdErr)
	mrb.DefineClassUnder(mod, "DepthError", stdErr)

	// Default options hash
	defH := mrb.HashNew()
	mrb.HashSet(defH, mrb.Intern("mode").Value(), mrb.Intern("compat").Value())
	mrb.HashSet(defH, mrb.Intern("symbol_keys").Value(), mrb.FalseValue())
	mrb.HashSet(defH, mrb.Intern("indent").Value(), mrb.FixnumValue(0))
	mrb.IVSet(mod, mrb.Intern("@default_options"), defH.Value())

	mod.DefineClassMethod("default_options", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		h := mrb.IVGet(mod, mrb.Intern("@default_options"))
		if cp, err := mrb.Funcall(h, mrb.Intern("dup")); err == nil {
			return cp
		}
		return h
	}, mrb.ArgsNone())

	mod.DefineClassMethod("default_options=", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		if !arg.IsHash() {
			return mrb.Raise(mrb.ETypeError(), "expected Hash")
		}
		h := mrb.IVGet(mod, mrb.Intern("@default_options"))
		_, _ = mrb.Funcall(h, mrb.Intern("merge!"), arg)
		return arg
	}, mrb.ArgsReq(1))

	doLoad := func(mrb *oruby.MrbState, srcVal oruby.Value, o ojOpts, blk oruby.RProc) oruby.MrbValue {
		if srcVal.IsNil() {
			if o.allowNil {
				return mrb.NilValue()
			}
			return mrb.Raise(mrb.ETypeError(), "expected String or IO, got nil")
		}
		var s string
		if srcVal.IsString() {
			s = srcVal.String()
		} else if mrb.RespondTo(srcVal, mrb.Intern("read")) {
			rv, err := mrb.Funcall(srcVal, mrb.Intern("read"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !rv.IsNil() {
				s = rv.String()
			}
		} else {
			return mrb.Raise(mrb.ETypeError(), "expected String or IO")
		}
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			if o.emptyString {
				return mrb.Raise(parseErr, "empty input")
			}
			return mrb.NilValue()
		}
		p := &jsonParser{mrb: mrb, s: s, opts: o}
		if !blk.IsNil() {
			for {
				p.skipWS()
				if p.pos >= len(p.s) {
					break
				}
				v, err := p.parseValue(0)
				if err != nil {
					return mrb.Raise(parseErr, err.Error())
				}
				if _, err := mrb.YieldE(blk, v); err != nil {
					return mrb.RaiseError(err)
				}
			}
			return mrb.NilValue()
		}
		v, err := p.parseValue(0)
		if err != nil {
			return mrb.Raise(parseErr, err.Error())
		}
		p.skipWS()
		if p.pos < len(p.s) {
			return mrb.Raise(parseErr, p.err("unexpected trailing characters").Error())
		}
		return v
	}

	mod.DefineClassMethod("load", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		o := collectOpts(mrb, mod, currentDefaultOpts(mrb, mod), args, 1)
		return doLoad(mrb, args.Item(0), o, blk)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())

	mod.DefineClassMethod("strict_load", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		base := currentDefaultOpts(mrb, mod)
		base.mode = modeStrict
		o := collectOpts(mrb, mod, base, args, 1)
		return doLoad(mrb, args.Item(0), o, blk)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())

	mod.DefineClassMethod("compat_load", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		base := currentDefaultOpts(mrb, mod)
		base.mode = modeCompat
		o := collectOpts(mrb, mod, base, args, 1)
		return doLoad(mrb, args.Item(0), o, blk)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())
	mod.DefineClassMethod("safe_load", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		base := currentDefaultOpts(mrb, mod)
		base.mode = modeStrict
		o := collectOpts(mrb, mod, base, args, 1)
		return doLoad(mrb, args.Item(0), o, blk)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())

	mod.DefineClassMethod("load_file", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		data, err := os.ReadFile(args.Item(0).String())
		if err != nil {
			return mrb.RaiseError(err)
		}
		o := collectOpts(mrb, mod, currentDefaultOpts(mrb, mod), args, 1)
		return doLoad(mrb, mrb.StrNew(string(data)), o, blk)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())

	dumpFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		o := collectOpts(mrb, mod, currentDefaultOpts(mrb, mod), args, 1)
		enc := &jsonEncoder{mrb: mrb, opts: o}
		if err := enc.encode(args.Item(0), 0); err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		return mrb.StrNew(enc.buf.String())
	}
	mod.DefineClassMethod("dump", dumpFn, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))
	mod.DefineClassMethod("generate", dumpFn, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))
	mod.DefineClassMethod("to_json", dumpFn, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	mod.DefineClassMethod("to_file", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		o := collectOpts(mrb, mod, currentDefaultOpts(mrb, mod), args, 2)
		enc := &jsonEncoder{mrb: mrb, opts: o}
		if err := enc.encode(args.Item(1), 0); err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		if err := os.WriteFile(args.Item(0).String(), enc.buf.Bytes(), 0o644); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsArg(2, 1)|mrb.ArgsKey(0, 1))

	mod.DefineClassMethod("mimic_JSON", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		_, _ = mrb.Resolve("json")
		return mrb.ModuleGet("JSON").Value()
	}, mrb.ArgsAny())

	mod.DefineClassMethod("optimize_rails", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.NilValue()
	}, mrb.ArgsNone())

	return nil
}
