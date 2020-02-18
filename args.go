package oruby

// #include "go-mrb.h"
import "C"
import "fmt"

// Arguments interface to simplify RArray and RArgs arguments
type Arguments interface {
	Len() int
	Get(mrb *MrbState, i int) interface{}
}

// RInterfaceArgs implments Arguments interface with generic interface array
type RInterfaceArgs struct {
	items []interface{}
}

// Len returns number of items
func (a RInterfaceArgs) Len() int {
	return len(a.items)
}

// Get returns item from mrb state
func (a RInterfaceArgs) Get(mrb *MrbState, i int) interface{} {
	return a.items[i]
}

// RArgs implments Go struct with args for MrbFuncT
// it implements Arguments interface and other methods simmilar to RArray
type RArgs struct {
	items []Value
}

// RArgsNew constructor for RArgs
func RArgsNew(self Value, items ...Value) RArgs {
	return RArgs{items}
}

// Item returns value item at index, or Nil if index is invalid
func (a RArgs) Item(index int) Value {
	l := len(a.items)

	// negative range check
	if index < 0 {
		index += l
	}
	if index < 0 || index >= l {
		return nilValue
	}

	return a.items[index]
}

// Get returns item from mrb state
func (a RArgs) Get(mrb *MrbState, i int) interface{} {
	return mrb.Intf(a.Item(i))
}

// SetItem returns value item at index, or Nil if index is invalid
func (a RArgs) SetItem(index int, v MrbValue) {
	l := len(a.items)

	// negative range check
	if index < 0 {
		index += l
	}
	if index < 0 || index >= l {
		return
	}

	a.items[index] = v.Value()
}

// ItemDef returns value item at index, or default value if arg is nil
func (a RArgs) ItemDef(index int, def MrbValue) Value {
	if index < 0 || index >= len(a.items) {
		return def.Value()
	}

	return a.Item(index)
}

// ItemDefFunc returns value item at index, or default value from func f
// if arg is nil
func (a RArgs) ItemDefFunc(index int, defF func() MrbValue) Value {
	if index < 0 || index >= len(a.items) {
		return defF().Value()
	}

	return a.Item(index)
}

// ItemDefInt returns value as int at index, or default int value if arg is nil
func (a RArgs) ItemDefInt(index, def int) int {
	if index < 0 || index >= len(a.items) {
		return def
	}

	return a.Item(index).Int()
}

// ItemDefBool returns value as bool at index, or default bool value if arg is nil
func (a RArgs) ItemDefBool(index int, def bool) bool {
	if index < 0 || index >= len(a.items) {
		return def
	}

	return a.Item(index).Bool()
}

// Slice returns Go slice made of Value items
func (a RArgs) Slice() []Value {
	return a.items
}

// Len returns number of args
func (a RArgs) Len() int {
	return len(a.items)
}

// SliceIntf returns Go slice made of interaface{} items
func (a RArgs) SliceIntf() []interface{} {
	ret := make([]interface{}, len(a.items))
	for i, v := range a.items {
		ret[i] = v
	}
	return ret
}

// GetArgs returns all arguments passed to MrbFuncT function
// as RArray type
func (mrb *MrbState) GetArgs(defaults ...interface{}) RArgs {
	args := RArgs{mrb.Args()}
	for i := args.Len(); i < len(defaults); i++ {
		args.items = append(args.items, mrb.Value(defaults[i]))
	}
	return args
}

// GetArgs3 return three function arguments
func (mrb *MrbState) GetArgs3(defaults ...interface{}) (Value, Value, Value) {
	args := mrb.GetArgs(defaults...)
	return args.Item(0).Value(), args.Item(1).Value(), args.Item(2).Value()
}

// Args returns all arguments passed to MrbFuncT function as Go slice
func (mrb *MrbState) selfArgs(self MrbValue) []Value {
	argc := int(C.mrb_get_argc(mrb.p))
	args := C.mrb_get_argv(mrb.p)
	ret := make([]Value, argc+1)
	ret[0] = self.Value()
	for i := 1; i < argc; i++ {
		ret[i] = Value{C._mrb_get_arg(args, C.int(i))}
	}
	return ret
}

// Args returns all arguments passed to MrbFuncT function as Go slice
func (mrb *MrbState) Args() []Value {
	argc := int(C.mrb_get_argc(mrb.p))
	args := C.mrb_get_argv(mrb.p)
	ret := make([]Value, argc)
	for i := 0; i < argc; i++ {
		ret[i] = Value{C._mrb_get_arg(args, C.int(i))}
	}
	return ret
}

// GetArgsWithBlock returns array with all arguments passed to MrbFuncT function
// and block as second value.
func (mrb *MrbState) GetArgsWithBlock(defaults ...interface{}) (RArgs, RProc) {
	return mrb.GetArgs(defaults...), mrb.GetArgsBlock()
}

// GetArgs2 return two function arguments
func (mrb *MrbState) GetArgs2(defaults ...interface{}) (Value, Value) {
	args := mrb.GetArgs(defaults...)
	return args.Item(0).Value(), args.Item(1).Value()
}

// GetArgsFirst returns first argument
func (mrb *MrbState) GetArgsFirst() Value {
	return Value{C._mrb_get_args_first(mrb.p)}
}

// GetArgsCount returns numer of arguments passed to function
func (mrb *MrbState) GetArgsCount() int {
	return int(C.mrb_get_argc(mrb.p))
}

// GetArgsBlock returns block argument
func (mrb *MrbState) GetArgsBlock() RProc {
	v := Value{C._mrb_get_args_block(mrb.p)}
	return mrb.RProc(v)
}

// ScanArgs returns arguments passed to MrbFuncT function
// via Go interface scan patern. Values must be a pointer to:
//
//   bool
//   string
//   int  [8,16,32,64]
//   uint [8,16,32,64]
//   float [32,64]
//   map[string]interface{}
//   map[interfaec{}]interface{}
//   []string
//   []interface
//   []int
//   []float64
//   oruby.Value, MrbSym, RObject, RArray, RHash, RProc
//
// Function returns number of arguments passed to function.
//
// Example:
//
//   v1 := mrb.NilValue()    // oruby.Value
//   v2 := "default string"  // string
//   v3 := 3                 // int
//   argc := mrb.ScanArgs(&v1, &v2, &v3)
func (mrb *MrbState) ScanArgs(args ...interface{}) (int, Value) {
	if len(args) == 0 {
		return 0, Value{C._mrb_get_args_block(mrb.p)}
	}

	argc := int(C.mrb_get_argc(mrb.p))
	rets := C.mrb_get_argv(mrb.p)

	for i, arg := range args {
		if i >= argc {
			continue
		}
		ret := Value{C._mrb_get_arg(rets, C.int(i))}

		switch v := arg.(type) {
		case *bool:
			*v = ret.Bool()
		case **string:
			if ret.IsNil() {
				*v = nil
			}
			**v = mrb.String(ret)
		case *string:
			*v = mrb.String(ret)
		case *int:
			*v = ret.Fixnum()
		case *int64:
			*v = int64(ret.Fixnum())
		case *int16:
			*v = int16(ret.Fixnum())
		case *int32:
			*v = int32(ret.Fixnum())
		case *int8:
			*v = int8(ret.Fixnum())
		case *float32:
			if ret.IsNil() {
				panic("nil value can not be converted to float64")
			}
			f, err := mrb.Float(ret)
			if err != nil {
				panic(err)
			}
			*v = float32(f.Float64())
		case *float64:
			if ret.IsNil() {
				panic("nil value can not be converted to float64")
			}
			f, err := mrb.Float(ret)
			if err != nil {
				panic(err)
			}
			*v = float64(f.Float64())
		case *uint:
			*v = uint(ret.Fixnum())
		case *uint64:
			*v = uint64(ret.Fixnum())
		case *uint16:
			*v = uint16(ret.Fixnum())
		case *uint32:
			*v = uint32(ret.Fixnum())
		case *uint8:
			*v = uint8(ret.Fixnum())
		case *uintptr:
			*v = ret.Uintptr()
		case *map[string]interface{}:
			if ret.IsNil() {
				continue
			}
			*v = toMapSI(mrb, ret)
		case *map[interface{}]interface{}:
			if ret.IsNil() {
				continue
			}
			*v = toMapII(mrb, ret)
		case *[]interface{}:
			*v = make([]interface{}, ret.Len())
			for i := 0; i < ret.Len(); i++ {
				av := mrb.AryRef(ret, i)
				*v = append(*v, mrb.Intf(av))
			}
		case *[]string:
			*v = make([]string, ret.Len())
			for i := 0; i < ret.Len(); i++ {
				av := mrb.AryRef(ret, i)
				*v = append(*v, mrb.String(av))
			}
		case *[]int:
			*v = make([]int, ret.Len())
			for i := 0; i < ret.Len(); i++ {
				av := mrb.AryRef(ret, i)
				*v = append(*v, av.Fixnum())
			}
		case *[]float64:
			*v = make([]float64, ret.Len())
			for i := 0; i < ret.Len(); i++ {
				av := mrb.AryRef(ret, i)
				if av.IsNil() {
					panic("nil value can not be converted to float64")
				}
				f, err := mrb.Float(av)
				if err != nil {
					panic(err)
				}
				*v = append(*v, f.Float64())
			}
		case *MrbSym:
			*v = ret.Symbol()
		case *Value:
			*v = ret
		case *RObject:
			*v = RObject{ret.v, mrb}
		case *RArray:
			if !ret.IsArray() {
				panic("array expected, got " + mrb.TypeName(ret))
			}
			*v = ary(ret.v, mrb)
		case *RHash:
			if !ret.IsHash() {
				panic("hash expected, got " + mrb.TypeName(ret))
			}
			*v = RHash{RObject{ret.v, mrb}}
		case *RProc:
			if ret.IsNil() {
				*v = RProc{}
				continue
			}
			if !ret.IsProc() {
				panic("RProc expected, got " + mrb.TypeName(ret))
			}
			*v = mrb.RProc(v)
		default:
			if err := mrb.Scan(ret, v); err != nil {
				panic(fmt.Sprintf(
					"Unsupported '%v'. Must be pointer to one of: bool, string, "+
						"int[8,16,32,64], uint[8,16,32,64], float[32,64], "+
						" map[string]interface{}, []string, []interface, "+
						"oruby MrbSym, Value, RObject, RArray, RHash, RProc.\n%v", v, err),
				)
			}

		}
	}
	return argc, Value{C._mrb_get_args_block(mrb.p)}
}
