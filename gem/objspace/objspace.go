// Package objspace implements Ruby's objspace standard library extension.
package objspace

import (
	_ "embed"
	"fmt"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed objspace.rb
var objspaceRb []byte

//go:embed trace.rb
var traceRb []byte

const (
	// Approximate mruby RVALUE slot size and value size on 64-bit MRB_NO_BOXING.
	rvalueSlotSize = 48
	mrbValueSize   = 16
	embedStrMax    = 23
)

func init() {
	oruby.LazyGem("objspace", initObjSpace)
	oruby.LazyGem("objspace/trace", func(mrb *oruby.MrbState) any {
		mrb.Resolve("objspace")
		rb.LoadRuby(mrb, "<internal:objspace/trace>", traceRb)
		return nil
	})
}

func initObjSpace(mrb *oruby.MrbState) any {
	mod := mrb.ModuleGet("ObjectSpace")

	wrapCls := mrb.DefineClassUnder(mod, "InternalObjectWrapper", mrb.ObjectClass())
	rb.DataClass(wrapCls)
	wrapCls.DefineMethod("type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		w, _ := rb.Get[*internalObjWrap](mrb, self)
		return mrb.Intern(typeNameSym(w.val)).Value()
	}, mrb.ArgsNone())
	wrapCls.DefineMethod("internal_object_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		w, _ := rb.Get[*internalObjWrap](mrb, self)
		return mrb.Uint64Value(uint64(w.val.Pointer()))
	}, mrb.ArgsNone())
	wrapCls.DefineMethod("inspect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		w, _ := rb.Get[*internalObjWrap](mrb, self)
		return mrb.StrNew(fmt.Sprintf("#<InternalObject:0x%x %s>", w.val.Pointer(), typeNameSym(w.val)))
	}, mrb.ArgsNone())

	mod.DefineClassMethod("memsize_of", memsizeOf, mrb.ArgsReq(1))
	mod.DefineClassMethod("__type_sym", objTypeSym, mrb.ArgsReq(1))
	mod.DefineClassMethod("__obj_address", objAddress, mrb.ArgsReq(1))
	mod.DefineClassMethod("__is_immediate", objIsImmediate, mrb.ArgsReq(1))
	mod.DefineClassMethod("__is_cdata", objIsCData, mrb.ArgsReq(1))
	mod.DefineClassMethod("internal_class_of", internalClassOf, mrb.ArgsReq(1))
	mod.DefineClassMethod("internal_super_of", internalSuperOf, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:objspace>", objspaceRb)
	return nil
}

type internalObjWrap struct {
	val oruby.Value
	rc  oruby.RClass
}

func isImmediate(v oruby.Value) bool {
	switch v.Type() {
	case oruby.MrbTTFalse, oruby.MrbTTTrue, oruby.MrbTTInteger, oruby.MrbTTSymbol, oruby.MrbTTUndef:
		return true
	case oruby.MrbTTFloat:
		// In mruby MRB_NO_BOXING, floats are stored inline in mrb_value (no heap slot).
		return true
	default:
		return false
	}
}

func calcMemsize(mrb *oruby.MrbState, v oruby.Value) int {
	if isImmediate(v) {
		return 0
	}
	sz := rvalueSlotSize
	switch v.Type() {
	case oruby.MrbTTString:
		n := oruby.RStringLen(v)
		capa := oruby.RStringCapa(v)
		if capa > embedStrMax {
			sz += capa + 1
		} else if n > embedStrMax {
			sz += n + 1
		}
	case oruby.MrbTTArray:
		n := oruby.RArrayLen(v)
		sz += n * mrbValueSize
	case oruby.MrbTTHash:
		keys := mrb.HashKeys(v)
		n := oruby.RArrayLen(keys)
		sz += n * (mrbValueSize * 2)
		if n > 0 {
			sz += 48 // hash table header
		}
	case oruby.MrbTTBigInt:
		if b, _, err := mrb.IntegerToBytes(v); err == nil {
			sz += len(b)
		}
	case oruby.MrbTTObject, oruby.MrbTTClass, oruby.MrbTTModule, oruby.MrbTTSClass, oruby.MrbTTException:
		if ivs, err := mrb.Funcall(v, mrb.Intern("instance_variables")); err == nil && ivs.IsArray() {
			sz += oruby.RArrayLen(ivs) * (mrbValueSize + 8)
		} else if err != nil {
			mrb.ExcClear()
		}
	case oruby.MrbTTCData:
		if ms, ok := mrb.Data(v).(interface{ Memsize() int }); ok {
			if extra := ms.Memsize(); extra > 0 {
				sz += extra
			}
		}
	}
	return sz
}

func memsizeOf(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	return mrb.FixnumValue(calcMemsize(mrb, v))
}

func typeNameSym(v oruby.Value) string {
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return "T_NIL"
		}
		return "T_FALSE"
	case oruby.MrbTTTrue:
		return "T_TRUE"
	case oruby.MrbTTInteger:
		return "T_FIXNUM"
	case oruby.MrbTTBigInt:
		return "T_BIGNUM"
	case oruby.MrbTTFloat:
		return "T_FLOAT"
	case oruby.MrbTTSymbol:
		return "T_SYMBOL"
	case oruby.MrbTTString:
		return "T_STRING"
	case oruby.MrbTTArray:
		return "T_ARRAY"
	case oruby.MrbTTHash:
		return "T_HASH"
	case oruby.MrbTTObject:
		return "T_OBJECT"
	case oruby.MrbTTClass, oruby.MrbTTSClass:
		return "T_CLASS"
	case oruby.MrbTTModule:
		return "T_MODULE"
	case oruby.MrbTTIClass:
		return "T_ICLASS"
	case oruby.MrbTTProc:
		return "T_DATA"
	case oruby.MrbTTRange:
		return "T_STRUCT"
	case oruby.MrbTTException:
		return "T_OBJECT"
	case oruby.MrbTTCData:
		return "T_DATA"
	case oruby.MrbTTFiber:
		return "T_DATA"
	default:
		return "T_OBJECT"
	}
}

func objTypeSym(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Intern(typeNameSym(mrb.GetArgsFirst())).Value()
}

func objAddress(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	return mrb.StrNew(fmt.Sprintf("0x%x", v.Pointer()))
}

func objIsImmediate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.BoolValue(isImmediate(mrb.GetArgsFirst()))
}

func objIsCData(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.BoolValue(mrb.GetArgsFirst().Type() == oruby.MrbTTCData)
}

func internalClassOf(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	return mrb.ClassOf(v).Value()
}

func internalSuperOf(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	var rc oruby.RClass
	if w, ok := rb.Get[*internalObjWrap](mrb, v); ok {
		rc = w.rc
	} else if v.IsClass() || v.IsModule() {
		rc = mrb.ClassPtr(v)
	} else {
		return mrb.ETypeError().Raise("class or module required")
	}
	sup := rc.Super()
	if sup.IsNil() {
		return mrb.NilValue()
	}
	sv := sup.Value()
	if sv.Type() == oruby.MrbTTIClass {
		wrapCls := rb.ClassPath(mrb, "ObjectSpace::InternalObjectWrapper")
		return mrb.DataWrapInterface(wrapCls, &internalObjWrap{val: sv, rc: sup}).Value()
	}
	return sv
}
