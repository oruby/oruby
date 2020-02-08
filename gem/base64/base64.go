package base64

import (
	"encoding/base64"
	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("base64", func(mrb *oruby.MrbState) interface{} {
		bas64 := mrb.DefineModule("Base64")

		bas64.DefineClassMethod("decode64", base64Decode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("encode64", base64Encode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("strict_decode64", base64StrictDecode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("strict_encode64", base64StrictEncode, mrb.ArgsReq(1))
		bas64.DefineClassMethod("urlsafe_decode64", base64UrlSafeEncode, mrb.ArgsArg(1, 1))
		bas64.DefineClassMethod("urlsafe_encode64", base64UrlSafeDecode, mrb.ArgsArg(1, 1))

		return nil
	})
}

// base64Decode returns the Base64-decoded version of str. This method complies with RFC 2045.
// Characters outside the base alphabet are ignored.
func base64Decode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return mrb.BytesValue(data)
}

// base64Encode returns the Base64-encoded version of bin. This method complies with RFC 2045.
// Line feeds are added to every 60 encoded characters.
func base64Encode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data := base64.StdEncoding.EncodeToString([]byte(str))
	return mrb.StringValue(data)
}

// base64StrictDecode returns the Base64-decoded version of str. This method complies with RFC 4648.
// ArgumentError is raised if str is incorrectly padded or contains non-alphabet characters.
// Note that CR or LF are also rejected.
func base64StrictDecode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data, err := base64.StdEncoding.Strict().DecodeString(str)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return mrb.BytesValue(data)
}

// Returns the Base64-encoded version of bin. This method complies with RFC 4648. No line feeds are added.
func base64StrictEncode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.String(mrb.GetArgsFirst())
	data := base64.StdEncoding.Strict().EncodeToString([]byte(str))
	return mrb.StringValue(data)
}

// Returns the Base64-decoded version of str. This method complies with “Base 64 Encoding with URL and Filename
//  Safe Alphabet'' in RFC 4648. The alphabet uses '-' instead of '+' and '_' instead of '/' The padding character
// is optional. This method accepts both correctly-padded and unpadded input.
// Note that it still rejects incorrectly-padded input.
func base64UrlSafeDecode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	str := mrb.String(args.Item(0))
	padding := args.ItemDef(1, mrb.NilValue())
	b64 := base64.RawURLEncoding

	if !padding.IsNil() && padding.Len() > 0 {
		pad := []rune(mrb.String(args.Item(1)))
		b64 = base64.URLEncoding.WithPadding(pad[0])
	}

	data, err := b64.DecodeString(str)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return mrb.BytesValue(data)
}

//  Returns the Base64-encoded version of bin. This method complies with “Base 64 Encoding with URL and Filename
//  Safe Alphabet'' in RFC 4648. The alphabet uses '-' instead of '+' and '_' instead of '/'.
//  Note that the result can still contain '='. You can remove the padding by setting padding as false.
func base64UrlSafeEncode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	str := mrb.String(args.Item(0))
	padding := args.ItemDefBool(1, false)
	var b64 *base64.Encoding

	if !padding {
		b64 = base64.RawURLEncoding
	} else {
		b64 = base64.URLEncoding
	}

	data := b64.EncodeToString([]byte(str))
	return mrb.StringValue(data)
}
