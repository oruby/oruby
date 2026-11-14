package json

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"strconv"
	"unsafe"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("json", func(mrb *oruby.MrbState) interface{} {
		jsonModule := mrb.DefineModule("JSON")

		jsonModule.DefineClassMethod("stringify", mrbJSONStringify, mrb.ArgsArg(1, 1))
		jsonModule.DefineClassMethod("generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))              // generate(obj, opts = nil)
		jsonModule.DefineClassMethod("pretty_generate", mrbJSONPrettyGenerate, mrb.ArgsArg(1, 1)) // pretty_generate(obj, opts = nil)
		jsonModule.DefineClassMethod("fast_generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))         // fast_generate(obj, opts = nil)

		jsonModule.DefineClassMethod("parse", mrbJSONParse, mrb.ArgsArg(1, 1))  // parse(source, opts = {})
		jsonModule.DefineClassMethod("parse!", mrbJSONParse, mrb.ArgsArg(1, 1)) // parse(source, opts = {})
		return nil
	})
}

func mrbJSONStringify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrbJSONGenerate(mrb, self)
}

func appendJSONString(buf []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x80 || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			out, err := json.Marshal(s, jsontext.EscapeForHTML(true))
			if err == nil {
				return append(buf, out...)
			}
			break
		}
	}
	buf = append(buf, '"')
	buf = append(buf, s...)
	buf = append(buf, '"')
	return buf
}

type kvPair struct {
	k string
	v oruby.Value
}

func appendJSONValue(mrb *oruby.MrbState, buf []byte, val oruby.Value) ([]byte, error) {
	if val.IsNil() {
		return append(buf, "null"...), nil
	}
	switch val.Type() {
	case oruby.MrbTTFalse:
		return append(buf, "false"...), nil
	case oruby.MrbTTTrue:
		return append(buf, "true"...), nil
	case oruby.MrbTTInteger:
		return strconv.AppendInt(buf, val.Int64(), 10), nil
	case oruby.MrbTTFloat:
		return strconv.AppendFloat(buf, val.Float64(), 'g', -1, 64), nil
	case oruby.MrbTTString:
		ptr, n := oruby.RStringMem(val)
		if n <= 0 || ptr == nil {
			return append(buf, `""`...), nil
		}
		return appendJSONString(buf, unsafe.String(ptr, n)), nil
	case oruby.MrbTTSymbol:
		return appendJSONString(buf, mrb.SymName(val.Symbol())), nil
	case oruby.MrbTTArray:
		elems := oruby.RArraySlice(val)
		buf = append(buf, '[')
		for i, elem := range elems {
			if i > 0 {
				buf = append(buf, ',')
			}
			var err error
			buf, err = appendJSONValue(mrb, buf, elem)
			if err != nil {
				return nil, err
			}
		}
		buf = append(buf, ']')
		return buf, nil
	case oruby.MrbTTHash:
		entries := oruby.RHashEntries(val)
		var stackKVs [16]kvPair
		var kvs []kvPair
		if len(entries) <= len(stackKVs) {
			kvs = stackKVs[:0]
		} else {
			kvs = make([]kvPair, 0, len(entries))
		}
		for i := range entries {
			key := entries[i][0]
			if key.Type() == oruby.MrbTTUndef {
				continue
			}
			var keyStr string
			switch key.Type() {
			case oruby.MrbTTString:
				ptr, n := oruby.RStringMem(key)
				if n > 0 && ptr != nil {
					keyStr = unsafe.String(ptr, n)
				}
			case oruby.MrbTTSymbol:
				keyStr = mrb.SymName(key.Symbol())
			default:
				keyStr = mrb.ObjAsString(key).String()
			}
			kvs = append(kvs, kvPair{k: keyStr, v: entries[i][1]})
		}
		// Insertion sort by key name for deterministic output
		for i := 1; i < len(kvs); i++ {
			cur := kvs[i]
			j := i - 1
			for j >= 0 && kvs[j].k > cur.k {
				kvs[j+1] = kvs[j]
				j--
			}
			kvs[j+1] = cur
		}
		buf = append(buf, '{')
		for i, kv := range kvs {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSONString(buf, kv.k)
			buf = append(buf, ':')
			var err error
			buf, err = appendJSONValue(mrb, buf, kv.v)
			if err != nil {
				return nil, err
			}
		}
		buf = append(buf, '}')
		return buf, nil
	default:
		out, err := json.Marshal(mrb.Intf(val), json.Deterministic(true), jsontext.EscapeForHTML(true))
		if err != nil {
			return nil, err
		}
		return append(buf, out...), nil
	}
}

var genBuf [512]byte

func mrbJSONGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	buf, err := appendJSONValue(mrb, genBuf[:0], v)
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate - %v", err)
	}
	return mrb.BytesValue(buf)
}

func mrbJSONPrettyGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	raw, err := appendJSONValue(mrb, nil, v)
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate - %v", err)
	}
	val := jsontext.Value(raw)
	if err := val.Format(jsontext.WithIndent("  ")); err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate - %v", err)
	}
	return mrb.BytesValue(val)
}

var (
	parseReader bytes.Reader
	parseDec    *jsontext.Decoder
	parseOps    = make([]oruby.BatchOp, 0, 64)
	parseStrBuf = make([]byte, 0, 256)
)

func decodeJSONToBatch(dec *jsontext.Decoder, ops *[]oruby.BatchOp, strBuf *[]byte) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case 'n':
		*ops = append(*ops, oruby.BatchOp{Op: 0})
	case 'f':
		*ops = append(*ops, oruby.BatchOp{Op: 1})
	case 't':
		*ops = append(*ops, oruby.BatchOp{Op: 2})
	case '0':
		if i, err := tok.Int(); err == nil {
			*ops = append(*ops, oruby.BatchOp{Op: 3, I: i})
		} else {
			f, ferr := tok.Float()
			if ferr != nil {
				return ferr
			}
			*ops = append(*ops, oruby.BatchOp{Op: 4, F: f})
		}
	case '"':
		s := tok.String()
		off := uint32(len(*strBuf))
		*strBuf = append(*strBuf, s...)
		*ops = append(*ops, oruby.BatchOp{Op: 5, Off: off, I: int64(len(s))})
	case '[':
		var cnt int64
		for dec.PeekKind() != ']' {
			if err := decodeJSONToBatch(dec, ops, strBuf); err != nil {
				return err
			}
			cnt++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*ops = append(*ops, oruby.BatchOp{Op: 6, I: cnt})
	case '{':
		var pairs int64
		for dec.PeekKind() != '}' {
			ktok, err := dec.ReadToken()
			if err != nil {
				return err
			}
			ks := ktok.String()
			off := uint32(len(*strBuf))
			*strBuf = append(*strBuf, ks...)
			*ops = append(*ops, oruby.BatchOp{Op: 5, Off: off, I: int64(len(ks))})
			if err := decodeJSONToBatch(dec, ops, strBuf); err != nil {
				return err
			}
			pairs++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*ops = append(*ops, oruby.BatchOp{Op: 7, I: pairs})
	}
	return nil
}

func mrbJSONParse(mrb *oruby.MrbState, salf oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if arg.Type() != oruby.MrbTTString {
		return mrb.Raise(mrb.EArgumentError(), "JSON::parse - string argument expected")
	}
	ptr, n := oruby.RStringMem(arg)
	var inBytes []byte
	if n > 0 && ptr != nil {
		inBytes = unsafe.Slice(ptr, n)
	}

	parseReader.Reset(inBytes)
	if parseDec == nil {
		parseDec = jsontext.NewDecoder(&parseReader)
	} else {
		parseDec.Reset(&parseReader)
	}
	parseOps = parseOps[:0]
	parseStrBuf = parseStrBuf[:0]

	if err := decodeJSONToBatch(parseDec, &parseOps, &parseStrBuf); err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::parse - %v", err)
	}
	if _, err := parseDec.ReadToken(); err != io.EOF {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::parse - unexpected trailing data")
	}

	return mrb.ExecBatchOps(parseOps, parseStrBuf)
}
