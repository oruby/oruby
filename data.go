package oruby

// #include "go-mrb.h"
import "C"
import (
	"fmt"
	"unsafe"
)

// RData struct
type RData struct{ p *C.struct_RData }

// data
//type FreeRDataProc = procedure(mrb mrb_state, data Pointer)

// Value implements MrbValue interface
func (d RData) Value() Value { return mrbObjValue(unsafe.Pointer(d.p)) }

// Type for MrbValue interface
func (d RData) Type() int { return d.Value().Type() }

// IsNil check for MrbValue interface
func (d RData) IsNil() bool { return d.p == nil }

// MrbDataType struct
type MrbDataType struct{ p *C.mrb_data_type }

// DataObjectAlloc stores data in RData
func (mrb *MrbState) DataObjectAlloc(klass RClass, datap interface{}, dtype MrbDataType) RData {
	data := RData{
		C.mrb_data_object_alloc(
			mrb.p,
			klass.p,
			unsafe.Pointer(klass.p),
			dtype.p,
		),
	}

	mrb.setHook(unsafe.Pointer(data.p), datap)

	return data
}

// DataWrapStruct wraps struct in data, alias for DataObjectAlloc
func (mrb *MrbState) DataWrapStruct(klass RClass, dtype MrbDataType, datap uintptr) RData {
	return mrb.DataObjectAlloc(klass, datap, dtype)
}

// func Data_Make_Struct(mrb mrb_state, klass RClass, size int, dtype MrbDataType, sval uintptr, data RData) {
//   C._Data_Make_Struct(mrb.p, klass.p, C.int(size), dtype.p, unsafe.Pointer(sval), data.p )
// }

// RDATA returns RData from MrbValue
func RDATA(obj MrbValue) RData { return RData{(*C.struct_RData)(C._mrb_ptr(obj.Value().v))} }

// DataPtr returns direct pointer to data
func DataPtr(d RData) uintptr { return uintptr(d.p.data) }

// DataType returns direct pointer to type
func DataType(d RData) uintptr { return uintptr(unsafe.Pointer(d.p._type)) }

// StructName returns name of stored struct
func (d RData) StructName() string { return C.GoString(d.p._type.struct_name) }

// Ptr  returns direct pointer to data
func (d RData) Ptr() uintptr { return uintptr(d.p.data) }

// IsInterface returns true if data stored is go onterface value
func (d RData) IsInterface() bool {
	return unsafe.Pointer(d.p._type) == unsafe.Pointer(C.mrb_interface_data_type())
}

// Interface returns RData interface from state
func (d RData) Interface(mrb *MrbState) interface{} {
	return mrb.getHook(unsafe.Pointer(d.p))
}

// DataCheckType checks if obj is RData ind is of dtype
func (mrb *MrbState) DataCheckType(obj MrbValue, dtype MrbDataType) {
	C.mrb_data_check_type(mrb.p, obj.Value().v, dtype.p)
}

// DataGetPtr retreives pointer from RData
func (mrb *MrbState) DataGetPtr(obj MrbValue, dtype MrbDataType) (uintptr, error) {
	if obj.Type() != MrbTTData {
		return uintptr(0), fmt.Errorf("")
	}

	ret := mrb.DataCheckGetPtr(obj, dtype)
	if ret == uintptr(0) {
		return uintptr(0), fmt.Errorf("wrong argument type %v", dtype)
	}

	return ret, nil
	// C.mrb_data_get_ptr() is never called
}

//func (mrb *MrbState) DATA_GET_PTR(obj MrbValue, dtype MrbDataType, atype uintptr) uintptr {
// (type*)mrb_data_get_ptr(mrb,obj,dtype) }

// DataCheckGetPtr returns pointer to data
func (mrb *MrbState) DataCheckGetPtr(obj MrbValue, dtype MrbDataType) uintptr {
	return uintptr(C.mrb_data_check_get_ptr(mrb.p, obj.Value().v, dtype.p))
}

//func (mrb *MrbState) DATA_CHECK_GET_PTR(obj MrbValue, dtype MrbDataType, atype uintptr) uintptr {
// (type*)mrb_data_check_get_ptr(mrb,obj,dtype) }

// DataWrapInterface wraps interface value into RData oruby value
func (mrb *MrbState) DataWrapInterface(klass RClass, datap interface{}) RData {
	data := RData{
		C.mrb_data_object_alloc(
			mrb.p,
			klass.p,
			unsafe.Pointer(klass.p),
			C.mrb_interface_data_type(),
		),
	}

	mrb.setHook(unsafe.Pointer(data.p), datap)

	return data
}

// DataCheckInterface checks if value is RData holding go interface value
func (mrb *MrbState) DataCheckInterface(obj MrbValue) {
	C.mrb_data_check_type(mrb.p, obj.Value().v, C.mrb_interface_data_type())
}

// DataGetInterface retreives interface from RData value without check
func (mrb *MrbState) DataGetInterface(obj MrbValue) interface{} {
	ret := C.mrb_data_check_get_ptr(mrb.p, obj.Value().v, C.mrb_interface_data_type())
	if ret == nil {
		return nil
	}

	return mrb.getHook(unsafe.Pointer(RDATA(obj).p))
}

// DataCheckGetInterface retreives interface value from RData value
func (mrb *MrbState) DataCheckGetInterface(obj MrbValue) interface{} {
	ret := C.mrb_data_check_get_ptr(mrb.p, obj.Value().v, C.mrb_interface_data_type())

	if ret == nil {
		return nil
	}

	return mrb.getHook(unsafe.Pointer(RDATA(obj).p))
}

// DataSetInterface sets interface value to RData value
func (mrb *MrbState) DataSetInterface(obj Value, datap interface{}) {
	// Check type
	if !MrbDataP(obj) {
		panic("object is not RData type")
	}

	p := unsafe.Pointer(RDATA(obj).p)

	// Release existing data
	if RDATA(obj).p.data != nil {
		mrb_free_goref(mrb.p, p)
	}

	// Set reference to avoid GC
	mrb.setHook(p, datap)

	// Set RData
	C.mrb_data_init(obj.v, p, C.mrb_interface_data_type())
}
