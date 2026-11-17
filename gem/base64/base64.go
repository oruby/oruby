package base64

import (
	"encoding/base64"
	"strings"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("base64", func(mrb *oruby.MrbState) interface{} {
		bas64 := mrb.DefineModule("Base64")

		bas64.DefineClassMethod("decode64", base64Decode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("encode64", base64Encode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("strict_decode64", base64StrictDecode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("strict_encode64", base64StrictEncode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("urlsafe_decode64", base64UrlSafeDecode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("urlsafe_encode64", base64UrlSafeEncode, mrb.ArgsReq(1)|mrb.ArgsKey(0, 1))

		return nil
	})
}

// base64Decode returns the Base64-decoded version of str. This method complies with RFC 2045.
// Characters outside the base alphabet are ignored.
func base64Decode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data := decodeBase64Permissive(str)
	return stringWithEncoding(mrb, mrb.BytesValue(data), "ASCII-8BIT")
}

// base64Encode returns the Base64-encoded version of bin. This method complies with RFC 2045.
// Line feeds are added to every 60 encoded characters.
func base64Encode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data := base64.StdEncoding.EncodeToString([]byte(str))
	var wrapped strings.Builder
	for len(data) > 60 {
		wrapped.WriteString(data[:60])
		wrapped.WriteByte('\n')
		data = data[60:]
	}
	wrapped.WriteString(data)
	wrapped.WriteByte('\n')
	return stringWithEncoding(mrb, mrb.StringValue(wrapped.String()), "US-ASCII")
}

func stringWithEncoding(mrb *oruby.MrbState, value oruby.MrbValue, encoding string) oruby.MrbValue {
	if mrb.RespondTo(value.Value(), oruby.Sym_force_encoding) {
		return mrb.Call(value, "force_encoding", encoding)
	}
	return value
}

func decodeBase64Permissive(str string) []byte {
	var result []byte
	var bits uint32
	bitCount := uint(0)
	validChars := 0

	for i := 0; i < len(str); i++ {
		if str[i] == '=' {
			if validChars >= 2 {
				break
			}
			continue
		}
		var value byte
		switch c := str[i]; {
		case c >= 'A' && c <= 'Z':
			value = c - 'A'
		case c >= 'a' && c <= 'z':
			value = c - 'a' + 26
		case c >= '0' && c <= '9':
			value = c - '0' + 52
		case c == '+':
			value = 62
		case c == '/':
			value = 63
		default:
			continue
		}
		validChars++

		bits = (bits << 6) | uint32(value)
		bitCount += 6
		if bitCount >= 8 {
			bitCount -= 8
			result = append(result, byte(bits>>bitCount))
			bits &= (1 << bitCount) - 1
		}
	}

	return result
}

// base64StrictDecode returns the Base64-decoded version of str. This method complies with RFC 4648.
// ArgumentError is raised if str is incorrectly padded or contains non-alphabet characters.
// Note that CR or LF are also rejected.
func base64StrictDecode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	if strings.ContainsAny(str, "\r\n") {
		return mrb.Raise(mrb.EArgumentError(), "invalid base64")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(str)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return stringWithEncoding(mrb, mrb.BytesValue(data), "ASCII-8BIT")
}

// Returns the Base64-encoded version of bin. This method complies with RFC 4648. No line feeds are added.
func base64StrictEncode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data := base64.StdEncoding.Strict().EncodeToString([]byte(str))
	return stringWithEncoding(mrb, mrb.StringValue(data), "US-ASCII")
}

// Returns the Base64-decoded version of str. This method complies with “Base 64 Encoding with URL and Filename
//
//	Safe Alphabet'' in RFC 4648. The alphabet uses '-' instead of '+' and '_' instead of '/' The padding character
//
// is optional. This method accepts both correctly-padded and unpadded input.
// Note that it still rejects incorrectly-padded input.
func base64UrlSafeDecode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	if strings.ContainsAny(str, "\r\n") {
		return mrb.Raise(mrb.EArgumentError(), "invalid base64")
	}
	b64 := base64.RawURLEncoding
	if strings.Contains(str, "=") {
		b64 = base64.URLEncoding
	}

	data, err := b64.DecodeString(str)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return stringWithEncoding(mrb, mrb.BytesValue(data), "ASCII-8BIT")
}

// Returns the Base64-encoded version of bin. This method complies with “Base 64 Encoding with URL and Filename
// Safe Alphabet” in RFC 4648. The alphabet uses '-' instead of '+' and '_' instead of '/'.
// Note that the result can still contain '='. You can remove the padding by setting padding as false.
func base64UrlSafeEncode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if mrb.GetArgsCount() != 1 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments")
	}
	str := mrb.String(mrb.GetArgsFirst())
	kwargs := mrb.KeywordArgs()
	paddingKey := mrb.SymbolValue(oruby.Sym_padding)
	if kwargs.Size(kwargs.Value()) > 1 || (!kwargs.EmptyP() && !kwargs.KeyP(paddingKey)) {
		return mrb.Raise(mrb.EArgumentError(), "unknown keyword")
	}
	padding := !kwargs.KeyP(paddingKey) || kwargs.Get(paddingKey).Bool()
	var b64 *base64.Encoding

	if !padding {
		b64 = base64.RawURLEncoding
	} else {
		b64 = base64.URLEncoding
	}

	data := b64.EncodeToString([]byte(str))
	return stringWithEncoding(mrb, mrb.StringValue(data), "US-ASCII")
}
