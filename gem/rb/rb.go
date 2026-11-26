// Package rb contains helpers shared by the Go implemented standard library
// gems (digest, uri, date, ...).
package rb

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/oruby/oruby"
)

//go:embed precompiled.orb
var precompiledOrbBytes []byte

var (
	precompiledOrbOnce sync.Once
	precompiledOrbVal  *oruby.OrbFile
)

func getPrecompiledOrb() *oruby.OrbFile {
	precompiledOrbOnce.Do(func() {
		if len(precompiledOrbBytes) == 0 {
			return
		}
		orb, err := oruby.ParseOrb("<internal:rb_precompiled.orb>", precompiledOrbBytes)
		if err != nil {
			return
		}
		precompiledOrbVal = orb
	})
	return precompiledOrbVal
}

// LoadRuby evaluates embedded Ruby glue code of a gem. Errors are fatal:
// the source is part of the binary.
func LoadRuby(mrb *oruby.MrbState, name string, src []byte) {
	if err := LoadRubyE(mrb, name, src); err != nil {
		panic(fmt.Sprintf("%s: %v", name, err))
	}
}

// LoadRubyE evaluates embedded Ruby glue code of a gem and returns any error.
func LoadRubyE(mrb *oruby.MrbState, name string, src []byte) error {
	c := mrb.MrbCcontextNew()
	defer c.Free()
	c.InternalFilename(name)
	if orb := getPrecompiledOrb(); orb != nil {
		sum := fmt.Sprintf("%x", sha256.Sum256(src))
		if e, ok := orb.Lookup(sum); ok {
			if _, err := mrb.LoadStaticIrepCxt(orb.EntryData(e), c); err == nil {
				return nil
			}
			mrb.ExcClear()
		}
	}
	_, err := mrb.LoadBytesCxt(src, c)
	return err
}

// DataClass makes instances of c carry Go data (see Get/Set)
func DataClass(c oruby.RClass) oruby.RClass {
	oruby.MrbSetInstanceTT(c, oruby.MrbTTCData)
	return c
}

// Set attaches Go value to object created from DataClass
func Set(mrb *oruby.MrbState, self oruby.Value, v interface{}) {
	mrb.DataSetInterface(self, v)
}

// Get returns Go value attached to object
func Get[T any](mrb *oruby.MrbState, self oruby.Value) (T, bool) {
	var zero T
	if self.Type() != oruby.MrbTTCData {
		return zero, false
	}
	v, ok := mrb.DataGetInterface(self).(T)
	return v, ok
}

// TypeName returns class name for error messages
func TypeName(mrb *oruby.MrbState, v oruby.Value) string {
	switch {
	case v.IsNil():
		return "nil"
	case v.Type() == oruby.MrbTTTrue:
		return "true"
	case v.Type() == oruby.MrbTTFalse:
		return "false"
	}
	return mrb.ClassOf(v).Name()
}

// Bytes converts String (or object responding to to_str) to bytes
func Bytes(mrb *oruby.MrbState, v oruby.Value) ([]byte, error) {
	if v.Type() == oruby.MrbTTString {
		return v.Bytes(), nil
	}
	if !v.IsNil() && mrb.RespondTo(v, oruby.Sym_to_str) {
		s, err := mrb.Funcall(v, oruby.Sym_to_str)
		if err != nil {
			return nil, err
		}
		if s.Type() == oruby.MrbTTString {
			return s.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("no implicit conversion of %s into String", TypeName(mrb, v))
}

// Str converts String (or object responding to to_str) to Go string
func Str(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	b, err := Bytes(mrb, v)
	return string(b), err
}

// Int converts Integer (or object responding to to_int) to int64
func Int(mrb *oruby.MrbState, v oruby.Value) (int64, error) {
	switch v.Type() {
	case oruby.MrbTTInteger:
		return v.Int64(), nil
	case oruby.MrbTTFloat:
		return int64(v.Float64()), nil
	}
	if !v.IsNil() && mrb.RespondTo(v, oruby.Sym_to_int) {
		i, err := mrb.Funcall(v, oruby.Sym_to_int)
		if err != nil {
			return 0, err
		}
		if i.Type() == oruby.MrbTTInteger {
			return i.Int64(), nil
		}
	}
	return 0, fmt.Errorf("no implicit conversion of %s into Integer", TypeName(mrb, v))
}

// TypeError raises TypeError with err message
func TypeError(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if mrb.HasExc() {
		return mrb.Exc().Value()
	}
	return mrb.Raise(mrb.ETypeError(), err.Error())
}

// Raise raises exception of class name (e.g. "ArgumentError", "URI::InvalidURIError")
func Raise(mrb *oruby.MrbState, class string, format string, args ...interface{}) oruby.MrbValue {
	cls := ClassPath(mrb, class)
	if cls.IsNil() {
		cls = mrb.ERuntimeError()
	}
	return mrb.Raise(cls, fmt.Sprintf(format, args...))
}

// ClassPath resolves constant path like "URI::Generic" (nil RClass if missing).
// Lookup goes through const_get, so autoloaded constants (Errno::EXXX) work.
func ClassPath(mrb *oruby.MrbState, path string) oruby.RClass {
	cur := mrb.ObjectClass()
	for _, name := range strings.Split(strings.TrimPrefix(path, "::"), "::") {
		var v oruby.Value
		if cur.ConstDefinedAt(name) {
			v = cur.ConstGet(name)
		} else {
			var err error
			v, err = mrb.Funcall(cur.Value(), oruby.Sym_const_get, mrb.SymbolValue(mrb.Intern(name)))
			if err != nil {
				mrb.ExcClear()
				return oruby.RClass{}
			}
		}
		if v.Type() != oruby.MrbTTClass && v.Type() != oruby.MrbTTModule {
			return oruby.RClass{}
		}
		cur = mrb.ClassPtr(v)
	}
	return cur
}

// Args returns arguments of current call
func Args(mrb *oruby.MrbState) []oruby.Value {
	return mrb.GetArgs().Slice()
}

// Arg returns argument i or nil
func Arg(args []oruby.Value, i int) oruby.Value {
	if i < len(args) {
		return args[i]
	}
	return oruby.Nil
}
