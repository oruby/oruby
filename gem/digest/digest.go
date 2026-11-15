// Package digest implements Ruby's digest library (Digest::MD5, SHA1, SHA256,
// SHA384, SHA512, SHA2) on top of Go crypto packages.
package digest

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding"
	"hash"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed digest.rb
var digestRb []byte

var algorithms = map[string]func() hash.Hash{
	"MD5":    md5.New,
	"SHA1":   sha1.New,
	"SHA256": sha256.New,
	"SHA384": sha512.New384,
	"SHA512": sha512.New,
}

type state struct {
	name string
	h    hash.Hash
}

func init() {
	oruby.LazyGem("digest", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Digest")
		mrb.DefineModuleUnder(mod, "Instance")
		cls := mrb.DefineClassUnder(mod, "Class", mrb.ObjectClass())
		base := rb.DataClass(mrb.DefineClassUnder(mod, "Base", cls))
		base.DefineMethod("__setup", digestSetup, mrb.ArgsReq(1))
		base.DefineMethod("initialize_copy", digestCopy, mrb.ArgsReq(1))
		base.DefineMethod("update", digestUpdate, mrb.ArgsReq(1))
		base.DefineAlias("<<", "update")
		base.DefineMethod("finish", digestFinish, mrb.ArgsNone())
		base.DefineMethod("reset", digestReset, mrb.ArgsNone())
		base.DefineMethod("digest_length", digestLength, mrb.ArgsNone())
		base.DefineMethod("block_length", digestBlockLength, mrb.ArgsNone())
		rb.LoadRuby(mrb, "digest.rb", digestRb)
		return nil
	})
	for _, name := range []string{"digest/md5", "digest/sha1", "digest/sha2", "digest/sha256",
		"digest/sha384", "digest/sha512", "digest/bubblebabble"} {
		oruby.GemAlias(name, "digest")
	}
}

func get(mrb *oruby.MrbState, self oruby.Value) (*state, oruby.MrbValue) {
	st, ok := rb.Get[*state](mrb, self)
	if !ok || st.h == nil {
		return nil, rb.Raise(mrb, "RuntimeError", "Digest::Base cannot be directly inherited in Ruby")
	}
	return st, nil
}

func digestSetup(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst().String()
	newFn := algorithms[name]
	if newFn == nil {
		return rb.Raise(mrb, "NotImplementedError", "Digest algorithm %s is not supported", name)
	}
	rb.Set(mrb, self, &state{name: name, h: newFn()})
	return self
}

func digestCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	other, ok := rb.Get[*state](mrb, mrb.GetArgsFirst())
	if !ok || other.h == nil {
		return rb.Raise(mrb, "TypeError", "copy of different digest class")
	}
	h := algorithms[other.name]()
	if m, ok := other.h.(encoding.BinaryMarshaler); ok {
		data, err := m.MarshalBinary()
		if err == nil {
			err = h.(encoding.BinaryUnmarshaler).UnmarshalBinary(data)
		}
		if err != nil {
			return mrb.Raise(mrb.ERuntimeError(), err.Error())
		}
	}
	rb.Set(mrb, self, &state{name: other.name, h: h})
	return self
}

func digestUpdate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	st, exc := get(mrb, self)
	if st == nil {
		return exc
	}
	b, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	st.h.Write(b)
	return self
}

func digestFinish(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	st, exc := get(mrb, self)
	if st == nil {
		return exc
	}
	return binary(mrb, st.h.Sum(nil))
}

func digestReset(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	st, exc := get(mrb, self)
	if st == nil {
		return exc
	}
	st.h.Reset()
	return self
}

func digestLength(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	st, exc := get(mrb, self)
	if st == nil {
		return exc
	}
	return mrb.FixnumValue(st.h.Size())
}

func digestBlockLength(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	st, exc := get(mrb, self)
	if st == nil {
		return exc
	}
	return mrb.FixnumValue(st.h.BlockSize())
}

// binary returns ASCII-8BIT String
func binary(mrb *oruby.MrbState, b []byte) oruby.Value {
	v := mrb.BytesValue(b)
	return mrb.Call(v, "force_encoding", mrb.StrNew("ASCII-8BIT"))
}
