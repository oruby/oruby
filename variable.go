package oruby

// #include "go-mrb.h"
import "C"

//variable

// ConstGet get const
func (mrb *MrbState) ConstGet(v MrbValue, id MrbSym) Value {
	return Value{C.mrb_const_get(mrb.p, v.Value().v, C.mrb_sym(id))}
}

// ConstSet set constant
func (mrb *MrbState) ConstSet(v MrbValue, id MrbSym, v2 MrbValue) {
	C.mrb_const_set(mrb.p, v.Value().v, C.mrb_sym(id), v2.Value().v)
}

// ConstDefined checks if const is defined
func (mrb *MrbState) ConstDefined(v MrbValue, id MrbSym) bool {
	return C.mrb_const_defined(mrb.p, v.Value().v, C.mrb_sym(id)) != false
}

// ConstRemove removes const
func (mrb *MrbState) ConstRemove(v MrbValue, id MrbSym) {
	C.mrb_const_remove(mrb.p, v.Value().v, C.mrb_sym(id))
}

// IVNameSymP check
func (mrb *MrbState) IVNameSymP(sym MrbSym) bool {
	return C.mrb_iv_name_sym_p(mrb.p, C.mrb_sym(sym)) != false
}

// IVNameSymCheck check
func (mrb *MrbState) IVNameSymCheck(sym MrbSym) {
	C.mrb_iv_name_sym_check(mrb.p, C.mrb_sym(sym))
}

// ObjIVGet returns instance vaiable
func (mrb *MrbState) ObjIVGet(obj RValue, sym MrbSym) Value {
	return Value{C.mrb_obj_iv_get(mrb.p, obj.RObject().p, C.mrb_sym(sym))}
}

// ObjIVSet set object instance variable
func (mrb *MrbState) ObjIVSet(obj RValue, sym MrbSym, v MrbValue) {
	C.mrb_obj_iv_set(mrb.p, obj.RObject().p, C.mrb_sym(sym), v.Value().v)
}

// ObjIVDefined is object instance variable defined
func (mrb *MrbState) ObjIVDefined(obj RValue, sym MrbSym) bool {
	return C.mrb_obj_iv_defined(mrb.p, obj.RObject().p, C.mrb_sym(sym)) != false
}

// GetIV get instance variable
func (mrb *MrbState) GetIV(obj MrbValue, name string) Value {
	return Value{C.mrb_iv_get(mrb.p, obj.Value().v, C.mrb_sym(mrb.Sym(name)))}
}

// SetIV get instance variable
func (mrb *MrbState) SetIV(obj MrbValue, name string, v interface{}) {
	err := mrb.IVSet(obj, mrb.Sym(name), mrb.Value(v))
	if err != nil {
		panic(err)
	}
}

// IVGet get instance variable
func (mrb *MrbState) IVGet(obj MrbValue, sym MrbSym) Value {
	return Value{C.mrb_iv_get(mrb.p, obj.Value().v, C.mrb_sym(sym))}
}

func (mrb *MrbState) HasIV(obj MrbValue) bool {
	switch obj.Type() {
	case MrbTTObject,
		MrbTTClass,
		MrbTTModule,
		MrbTTSClass,
		MrbTTHash,
		MrbTTCData,
		MrbTTException:
		return true
	default:
		return false
	}
	// C.obj_iv_p() ported
}

// IVSet set instance variable
func (mrb *MrbState) IVSet(obj MrbValue, sym MrbSym, v MrbValue) error {
	if !mrb.HasIV(obj) {
		return EArgumentError("cannot set instance variable")
	}

	o := obj.Value()

	if o.RBasic().IsFrozen() {
		return EFrozenError("can't modify frozen %v", mrb.TypeName(o))
	}

	C.mrb_iv_set(mrb.p, o.v, C.mrb_sym(sym), v.Value().v)
	return nil
}

// IVDefined instance variable defined
func (mrb *MrbState) IVDefined(v MrbValue, sym MrbSym) bool {
	return C.mrb_iv_defined(mrb.p, v.Value().v, C.mrb_sym(sym)) != false
}

// IVRemove remove instance variable
func (mrb *MrbState) IVRemove(obj MrbValue, sym MrbSym) Value {
	return Value{C.mrb_iv_remove(mrb.p, obj.Value().v, C.mrb_sym(sym))}
}

// IVCopy copy instance variable
func (mrb *MrbState) IVCopy(dst, src MrbValue) { C.mrb_iv_copy(mrb.p, dst.Value().v, src.Value().v) }

// ConstDefinedAt checks const definition
func (mrb *MrbState) ConstDefinedAt(mod MrbValue, id MrbSym) bool {
	return C.mrb_const_defined_at(mrb.p, mod.Value().v, C.mrb_sym(id)) != false
}

// ModConstants get mod constants
func (mrb *MrbState) ModConstants(mod MrbValue) RArray {
	return ary(C.mrb_mod_constants(mrb.p, mod.Value().v), mrb)
}

// FGlobalVariables list
func (mrb *MrbState) FGlobalVariables() RArray {
	return ary(C.mrb_f_global_variables(mrb.p, nilValue.v), mrb)
}

// GVGet get global variable
func (mrb *MrbState) GVGet(sym MrbSym) Value {
	return Value{C.mrb_gv_get(mrb.p, C.mrb_sym(sym))}
}

// GVGetObj get global variable as RArray
func (mrb *MrbState) GVGetObj(sym MrbSym) RValue {
	return RValue{C.mrb_gv_get(mrb.p, C.mrb_sym(sym)), mrb}
}

// GVSet set global variable
func (mrb *MrbState) GVSet(sym MrbSym, val MrbValue) {
	C.mrb_gv_set(mrb.p, C.mrb_sym(sym), val.Value().v)
}

// GVRemove global variable
func (mrb *MrbState) GVRemove(sym MrbSym) { C.mrb_gv_remove(mrb.p, C.mrb_sym(sym)) }

// SetGV set global variable with name string
func (mrb *MrbState) SetGV(name string, val interface{}) {
	mrb.GVSet(mrb.Sym(name), mrb.Value(val))
}

// GetGV get global variable with name
func (mrb *MrbState) GetGV(name string) Value {
	return mrb.GVGet(mrb.Sym(name))
}

// GetObjGV get global variable with name, as RValue
func (mrb *MrbState) GetObjGV(name string) RValue {
	return RValue{mrb.GVGet(mrb.Sym(name)).v, mrb}
}

// ModClassVariables list module class variables
func (mrb *MrbState) ModClassVariables(v MrbValue) RArray {
	return ary(C.mrb_mod_class_variables(mrb.p, v.Value().v), mrb)
}

// ModCVGet module get class variable
func (mrb *MrbState) ModCVGet(c RClass, sym MrbSym) Value {
	return Value{C.mrb_mod_cv_get(mrb.p, c.p, C.mrb_sym(sym))}
}

// CVGet get class variable
func (mrb *MrbState) CVGet(c MrbValue, sym MrbSym) Value {
	return Value{C.mrb_cv_get(mrb.p, c.Value().v, C.mrb_sym(sym))}
}

// ModCVSet set module class variable
func (mrb *MrbState) ModCVSet(c RClass, sym MrbSym, v MrbValue) {
	C.mrb_mod_cv_set(mrb.p, c.p, C.mrb_sym(sym), v.Value().v)
}

// CVSet set class variable
func (mrb *MrbState) CVSet(mod MrbValue, sym MrbSym, v MrbValue) {
	C.mrb_cv_set(mrb.p, mod.Value().v, C.mrb_sym(sym), v.Value().v)
}

// ModCVDefined module variable defined
func (mrb *MrbState) ModCVDefined(c RClass, sym MrbSym) bool {
	return C.mrb_mod_cv_defined(mrb.p, c.p, C.mrb_sym(sym)) != false
}

// CVDefined class variable defined
func (mrb *MrbState) CVDefined(mod MrbValue, sym MrbSym) bool {
	return C.mrb_cv_defined(mrb.p, mod.Value().v, C.mrb_sym(sym)) != false
}

/* return non-zero to break the loop */
//typedef int (mrb_iv_foreach_func)(mrb_state*,mrb_sym,mrb_value,void*);
//MRB_API void mrb_iv_foreach(mrb_state *mrb, mrb_value obj, mrb_iv_foreach_func *func, void *p);
