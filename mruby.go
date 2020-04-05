package oruby

// #cgo CFLAGS: -I${SRCDIR}/vendor/mruby/include
// #cgo LDFLAGS: -L${SRCDIR}/vendor/mruby/build/host/lib
// #cgo amd64   CFLAGS:  -DMRB_INT64 -DMRB_DEBUG -DMRB_ENABLE_DEBUG_HOOK -DMRB_HIGH_PROFILE -DMRB_METHOD_T_STRUCT
// #cgo linux   LDFLAGS: -lmruby -lm -lreadline -lncurses
// #cgo darwin  LDFLAGS: -lmruby -lm -lreadline -lncurses
////#cgo windows LDFLAGS: -lmruby -lm -lmingwex -static
// #cgo windows LDFLAGS: ${SRCDIR}/mruby.dll
// #include "go-mrb.h"
import "C"
import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type (
	// MrbCode alias for mrb_code
	MrbCode = byte
	// MrbAspec alias for mrb_aspec which specifies arguments of a function
	MrbAspec uint32
)

// FiberState enum
const (
	FiberCreated = iota
	FiberRunning
	FiberResumed
	FiberSuspender
	FiberTransfered
	FiberTerminated
)

// MrbFuncT is default oruby function type mrb_func_t
type MrbFuncT func(mrb *MrbState, self Value) MrbValue

// MrbAtexitFunc is function run at oruby state exit
type MrbAtexitFunc func(mrb *MrbState)

// MrbInjectT is function run at oruby state Inject chan
type MrbInjectT func(mrb *MrbState)

// MrbState is main oruby context for running code
type MrbState struct {
	p *C.mrb_state

	sync.Mutex
	stack          int32
	WaitGroup      sync.WaitGroup
	mrbProcs       map[unsafe.Pointer]MrbFuncT
	classmap       map[reflect.Type]unsafe.Pointer
	hooks          map[unsafe.Pointer]interface{}
	funcs          []interface{}
	matrix         [][]interface{}
	exitChan       chan struct{}
	injectVMChan   chan RProc
	injectMainChan chan RProc
	features       map[string]interface{} // features stash
	nilValue       Value                  // cached nil Value
	afterInitSym   MrbSym                 // cached mrb.Intern("after_init")

}

// NewCore create state is MrbState without gems,
// leaving user to init subset of available gems
func NewCore() (*MrbState, error) {
	cmrb := C.mrb_open()
	if cmrb == nil {
		return nil, errors.New("error creating oruby state")
	}

	mrb := &MrbState{
		cmrb,
		sync.Mutex{},
		0,
		sync.WaitGroup{},
		make(map[unsafe.Pointer]MrbFuncT),
		make(map[reflect.Type]unsafe.Pointer),
		make(map[unsafe.Pointer]interface{}),
		make([]interface{}, 0, 255),
		make([][]interface{}, 500),
		make(chan struct{}),
		nil,
		nil,
		make(map[string]interface{}),
		Value{C.mrb_nil_value()},
		0,
	}

	mrb.matrix[0] = make([]interface{}, 500)
	mrb.afterInitSym = mrb.Intern("after_init")

	// Store *MrbState pointer so it can be retrieved it from C callbacks
	registerState(mrb)

	// SystemCallError exception
	// mruby code have SystemCallError::_sys_fail method, but it is not used
	mrb.DefineClass("SystemCallError", mrb.EStandardErrorClass())

	runtime.SetFinalizer(mrb, mrbFinalizer)
	return mrb, nil
}

func mrbFinalizer(mrb *MrbState) {
	mrb.Close()
}

// MrbValue is interface type which can return oruby value
type MrbValue interface {
	Value() Value
	Type() int
	IsNil() bool
}

// Value encapsulates oruby value type
type Value struct{ v C.mrb_value }

// Value implements MrbValue interface for oruby Value
func (v Value) Value() Value { return v }

// Type returns oruby TT type of value
func (v Value) Type() int { return int(C._mrb_type(v.v)) }

// IsNil Checks if oruby value is nil value
func (v Value) IsNil() bool { return C._mrb_is_nil(v.v) != 0 }

// IsHash Checks if oruby value is hash value
func (v Value) IsHash() bool { return C._mrb_type(v.v) == MrbTTHash }

// IsArray Checks if oruby value is array value
func (v Value) IsArray() bool { return C._mrb_type(v.v) == MrbTTArray }

// IsFixnum Checks if oruby value is Fixnum / int value
func (v Value) IsFixnum() bool { return C._mrb_type(v.v) == MrbTTFixnum }

// IsInt Checks if oruby value is Int (Fixnum) value
func (v Value) IsInt() bool { return C._mrb_type(v.v) == MrbTTFixnum }

// IsFloat Checks if oruby value is float value
func (v Value) IsFloat() bool { return C._mrb_type(v.v) == MrbTTFloat }

// IsSymbol checks if oruby value is Symbol value
func (v Value) IsSymbol() bool { return C._mrb_type(v.v) == MrbTTSymbol }

// IsString checks if oruby value is Symbol value
func (v Value) IsString() bool { return C._mrb_type(v.v) == MrbTTString }

// IsData checks if oruby value is RData value
func (v Value) IsData() bool { return C._mrb_type(v.v) == MrbTTData }

// IsProc checks if oruby value is oruby RProc value
func (v Value) IsProc() bool { return C._mrb_type(v.v) == MrbTTProc }

// IsClass Checks if oruby value is class value
func (v Value) IsClass() bool { return C._mrb_type(v.v) == MrbTTClass }

// IsSingletonClass checks if oruby value is singleton class
func (v Value) IsSingletonClass() bool { return C._mrb_type(v.v) == MrbTTSClass }

// IsModule Checks if oruby value is module value
func (v Value) IsModule() bool { return C._mrb_type(v.v) == MrbTTModule }

// IsBool checks if oruby value is bool value
func (v Value) IsBool() bool {
	t := C._mrb_type(v.v)
	return t == MrbTTTrue || (t == MrbTTFalse && C._mrb_nil_p(v.v) == 0)
}

// Flags returns object flags or 0 for simple values
func (v Value) Flags() int { return int(C._mrb_value_flags(v.v)) }

// MigrateTo implements ValueMigrator interfacs for Value
func (v Value) MigrateTo(mrb2 *MrbState) Value {
	if v.HasBasic() {
		panic("Value must be migrated directly")
	}
	return Value{v.v}
}

// Interface implements convertingvalue to interfaces
func (v Value) Interface(mrb *MrbState) interface{} { return mrb.Intf(v) }

// Convert oruby value to interface
func (v Value) Convert(mrb *MrbState, obj MrbValue) (interface{}, error) {
	return mrb.Intf(obj), nil
}

// Len returns length of oruby array, string, 0 for other types
func (v Value) Len() int {
	switch v.Type() {
	case MrbTTArray:
		return RArrayLen(v)
	case MrbTTString:
		return RStringLen(v)
	default:
		return 0
	}
}

// ValueMigrator interface to create oruby world value
type ValueMigrator interface {
	MigrateTo(*MrbState) Value
}

// Converter interface to retreive Go interface from ruby world value
type Converter interface {
	Convert(*MrbState, MrbValue) (interface{}, error)
}

// MrbSym direct alias to mrb_sym uint32 value
type MrbSym uint

// MrbCallInfo call information
type MrbCallInfo struct{ p *C.struct_mrb_call_info }

// MrbContext call
type MrbContext struct{ p *C.struct_mrb_context }

// ExitChan is signaled on MrbState close
// goroutines that depend on mrb should return on ExitChan closing
func (mrb *MrbState) ExitChan() chan struct{} {
	return mrb.exitChan
}

//export inject_run
func inject_run(idx C.mrb_int) {
	mrb := getMrbStateIndex(int(idx))

	select {
	case <-mrb.exitChan:
	case proc := <-mrb.injectVMChan:
		_, _ = mrb.FuncallWithBlock(proc, mrb.Intern("call"))
	default:
	}
}

// Inject code to be executed in mrb
func (mrb *MrbState) Inject(proc RProc) {
	if atomic.LoadInt32(&mrb.stack) == 0 {
		mrb.startInjector()
	}

	select {
	case mrb.injectVMChan <- proc:
		return
	case <-mrb.exitChan:
		return
	default:
	}

	select {
	case mrb.injectMainChan <- proc:
	case <-mrb.exitChan:
	}
}

// startInjector for code to be executed from gorputines in main mrb
func (mrb *MrbState) startInjector() {
	mrb.Lock()
	mrb.injectMainChan = make(chan RProc)
	mrb.injectVMChan = make(chan RProc)
	C.set_mrb_injector(mrb.p)
	atomic.StoreInt32(&mrb.stack, 1)
	mrb.Unlock()

	var injectorLock sync.Mutex

	go func() {
		for proc := range mrb.injectMainChan {
			injectorLock.Lock()
			_, _ = mrb.FuncallWithBlock(proc, mrb.Intern("call"))
			injectorLock.Unlock()
		}
	}()
}

// Close oruby state
// before closing mruby state, mrb.ExitChan() is closed
// so all goroutines are signaled to close
//
// Go routines from Gems that have exit procs should
// send proc via mrb.InjectChan() and then signal mrb.WaitGroup.Done()
// After mrb.WaitGroup.Wait() finishes, mrb.InjectChan is closed.
func (mrb *MrbState) Close() {
	if mrb.p != nil {
		runtime.SetFinalizer(mrb, nil)

		// Signal all mrb goroutines that we are closing
		// and Wait all well-behaved goroutines to finish
		close(mrb.exitChan)

		// Goroutines from Gems that send exit procs should
		// send proc to mrb.InjectChan() and then signal mrb.WaitGroup.Done()
		mrb.WaitGroup.Wait()

		if mrb.injectVMChan != nil {
			close(mrb.injectVMChan)
		}
		if mrb.injectMainChan != nil {
			close(mrb.injectMainChan)
		}

		idx := int(C._mrb_get_idx(mrb.p))
		C.mrb_close(mrb.p)

		mu.Lock()
		states[idx] = nil
		mu.Unlock()

		mrb.p = nil

		mrb.mrbProcs = nil
		mrb.classmap = nil
		mrb.hooks = nil
		mrb.funcs = nil
		mrb.features = nil
	}
}

// New oruby state with all gems
func New() (*MrbState, error) {
	mrb, err := NewCore()
	if err != nil {
		return mrb, err
	}

	// Init all Go gems
	for k, geminit := range gems {
		mrb.features[k] = geminit(mrb)
	}

	if !GemExists("print") {
		mrb.features["print"] = initPrint(mrb)
	}

	return mrb, nil
}

// Value converts Go interface to oruby value
func (mrb *MrbState) Value(o interface{}) Value {
	switch v := o.(type) {
	case nil:
		return nilValue
	case bool:
		return Bool(v)
	case int:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case *int:
		if v == nil {
			return nilValue
		}
		return Value{C.mrb_fixnum_value(C.mrb_int(*v))}
	case int32:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case int8:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case int16:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case int64:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case uint:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case uint32:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case uint64:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case uint8:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case uint16:
		return Value{C.mrb_fixnum_value(C.mrb_int(v))}
	case float32:
		return mrb.FloatValue(float64(v))
	case float64:
		return mrb.FloatValue(v)
	case string:
		return mrb.StrNew(v)
	case uintptr:
		return mrb.CPtrValue(v)
	case unsafe.Pointer:
		return Value{C.mrb_cptr_value(mrb.p, v)}
	case []byte:
		return mrb.BytesValue(v)
	case map[string]interface{}:
		hash := mrb.HashNewCapa(32)
		for key, val := range v {
			hash.Set(mrb.Value(key), mrb.Value(val))
		}
		return hash.Value()
	case map[interface{}]interface{}:
		hash := mrb.HashNewCapa(32)
		for key, val := range v {
			hash.Set(mrb.Value(key), mrb.Value(val))
		}
		return hash.Value()
	case []string:
		ary := mrb.AryNewCapa(len(v))
		for i := 0; i < len(v); i++ {
			ary.Push(mrb.StrNew(v[i]))
		}
		return ary.Value()
	case []int:
		ary := mrb.AryNewCapa(len(v))
		for i := 0; i < len(v); i++ {
			ary.Push(mrb.FixnumValue(v[i]))
		}
		return ary.Value()
	case []float64:
		ary := mrb.AryNewCapa(len(v))
		for i := 0; i < len(v); i++ {
			ary.Push(mrb.FloatValue(v[i]))
		}
		return ary.Value()
	case Value:
		return v
	case MrbFuncT:
		return mrb.ProcNewCFunc(v).Value()
	case complex64:
		return mrb.NewInstance("Complex", real(v), imag(v)).Value()
	case complex128:
		return mrb.NewInstance("Complex", real(v), imag(v)).Value()
	case MrbValue:
		return v.Value()
	case ValueMigrator:
		return v.MigrateTo(mrb)
	default:
		rv := reflect.ValueOf(o)
		return mrb.valueValue(rv)
	}
}

func (mrb *MrbState) valueValue(v reflect.Value) Value {
	switch v.Kind() {
	case reflect.Invalid:
		return Value{C.mrb_nil_value()}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return Value{C.mrb_fixnum_value(C.mrb_int(v.Int()))}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return Value{C.mrb_fixnum_value(C.mrb_int(v.Uint()))}
	case reflect.Int64:
		return Value{C.mrb_fixnum_value(C.mrb_int(v.Int()))}
	case reflect.Uint64:
		return Value{C.mrb_fixnum_value(C.mrb_int(v.Uint()))}
	case reflect.Uintptr:
		return Value{C._mrb_uintptr_value(mrb.p, (C.uintptr_t)(v.Interface().(uintptr)))}
	case reflect.UnsafePointer:
		return Value{C._mrb_uintptr_value(mrb.p, C.uintptr_t(v.Pointer()))}
	case reflect.Float32, reflect.Float64:
		return mrb.FloatValue(v.Float())
	case reflect.Complex64, reflect.Complex128:
		vv := v.Complex()
		return mrb.NewInstance("Complex", real(vv), imag(vv)).Value()
	case reflect.String:
		return mrb.StrNew(v.String())
	case reflect.Bool:
		return Bool(v.Bool())
	case reflect.Array, reflect.Slice:
		ary := mrb.AryNewCapa(v.Len())
		for i := 0; i < v.Len(); i++ {
			ary.Push(mrb.Value(v.Index(i).Interface()))
		}
		return ary.Value()
	case reflect.Map:
		hash := mrb.HashNewCapa(len(v.MapKeys()))
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			hash.Set(mrb.Value(key.Interface()), mrb.Value(val.Interface()))
		}
		return hash.Value()
	case reflect.Interface:
		if t, ok := v.Interface().(MrbValue); ok {
			return t.Value()
		}
		if t, ok := v.Interface().(ValueMigrator); ok {
			return t.MigrateTo(mrb)
		}

		// TODO: Test interface to value
		return mrb.DataValue(v.Interface())

	case reflect.Ptr:
		return mrb.DataValue(v.Interface())

	case reflect.Struct:
		return mrb.DataValue(v.Interface())

	case reflect.Func:
		switch f := v.Interface().(type) {
		case MrbFuncT:
			return mrb.ProcNewCFunc(f).Value()
		default:
			return mrb.ProcNewGofunc(v.Interface()).Value()
		}

	case reflect.Chan:
		panic("chan type not supported as ruby Value")
	}
	return Value{C.mrb_nil_value()}
}

func (mrb *MrbState) String(v MrbValue) string {
	switch v.Type() {
	case C.MRB_TT_FALSE:
		if v.IsNil() {
			return ""
		}
		return "false"
	case C.MRB_TT_FREE:
		return ""
	case C.MRB_TT_TRUE:
		return "true"
	case C.MRB_TT_FIXNUM:
		return fmt.Sprint(MrbFixnum(v))
	case C.MRB_TT_SYMBOL:
		return mrb.SymString(MrbSymbol(v))
	case C.MRB_TT_UNDEF:
		return "UNDEFINED"
	case C.MRB_TT_FLOAT:
		return fmt.Sprintf("%v", MrbFloat(v))
	case C.MRB_TT_CPTR:
		return fmt.Sprintf("%v", MrbCptr(v))
	case C.MRB_TT_ARRAY:
		return fmt.Sprintf("%v", mrb.Intf(v))
	case C.MRB_TT_HASH:
		return fmt.Sprintf("%v", mrb.Intf(v))
	case C.MRB_TT_STRING:
		return mrb.StrToCstr(v)
	case C.MRB_TT_DATA:
		return fmt.Sprintf("%v", mrb.Intf(v))
	case C.MRB_TT_OBJECT:
		return mrb.StrToCstr(mrb.AnyToS(v))
	case C.MRB_TT_CLASS, C.MRB_TT_MODULE, C.MRB_TT_ICLASS, C.MRB_TT_SCLASS:
		return mrb.StrToCstr(mrb.AnyToS(v))
	case C.MRB_TT_PROC, C.MRB_TT_RANGE:
		return mrb.StrToCstr(mrb.AnyToS(v))
	case C.MRB_TT_ENV:
		return mrb.StrToCstr(mrb.AnyToS(v))
	case C.MRB_TT_EXCEPTION:
		return fmt.Sprintf("%v %v", mrb.StrToCstr(mrb.AnyToS(v)), mrb.IVGet(v, mrb.Intern("mesg")))
	case C.MRB_TT_FILE, C.MRB_TT_FIBER:
		return mrb.StrToCstr(mrb.AnyToS(v))
	default:
		return mrb.StrToCstr(mrb.AnyToS(v))
	}
}

// Intf converts oruby value to Go interface
func (mrb *MrbState) Intf(o MrbValue) interface{} {
	switch o.Type() {
	case C.MRB_TT_FALSE:
		if C._mrb_fixnum(o.Value().v) == 0 {
			return nil
		}
		return false
	case C.MRB_TT_TRUE:
		return true
	case C.MRB_TT_FLOAT:
		return float64(C._mrb_float(o.Value().v))
	case C.MRB_TT_FIXNUM:
		return int(C._mrb_fixnum(o.Value().v))
	case C.MRB_TT_SYMBOL:
		return MrbSym(C._mrb_symbol(o.Value().v))
	case C.MRB_TT_UNDEF:
		return nil
	case C.MRB_TT_CPTR:
		return uintptr(C._mrb_cptr(o.Value().v))
	case C.MRB_TT_FREE:
		return nil // is this ok? for MRB_TT_FREE
	case C.MRB_TT_OBJECT:
		// Generic oruby object : @ivar=value -> map[string]:interface{}
		vars := mrb.ObjInstanceVariables(o)
		kcnt := RArrayLen(vars)
		hash := make(map[string]interface{}, kcnt)
		for i := 0; i < kcnt; i++ {
			key := mrb.AryRef(vars, i)
			val := mrb.IVGet(o, mrb.ObjToSym(key))
			hash[mrb.String(key)] = mrb.Intf(val)
		}
		return hash
	case C.MRB_TT_CLASS, C.MRB_TT_MODULE, C.MRB_TT_SCLASS:
		return RClass{MrbClassPtr(o).p, mrb}
	case C.MRB_TT_PROC:
		if C._mrb_proc_has_env(mrb.p, MrbProcPtr(o).p) != 0 {
			fv := C._mrb_proc_env_get(mrb.p, MrbProcPtr(o).p, C.mrb_int(0))
			if f, _ := mrb.getFunc(uint(C._mrb_fixnum(fv))); f != nil {
				return f
			}
		}

		return func(args ...interface{}) (interface{}, error) {
			ret, err := mrb.Funcall(o, mrb.Intern("call"), args...)
			return mrb.Intf(ret), err
		}
	case C.MRB_TT_ARRAY:
		arry := make([]interface{}, RArrayLen(o))
		for i := range arry {
			arry[i] = mrb.Intf(mrb.AryRef(o, i))
		}
		return arry
	case C.MRB_TT_HASH:
		keys := mrb.HashKeys(o)
		kcnt := RArrayLen(keys)
		keysSymbol := true
		keysString := true
		for i := 0; i < kcnt; i++ {
			key := mrb.AryRef(keys, i)
			keysSymbol = keysSymbol && (key.Type() == MrbTTSymbol)
			keysString = keysString && (key.Type() == MrbTTString)
			if !keysSymbol && !keysString {
				break
			}
		}
		if keysSymbol || keysString {
			hash := make(map[string]interface{}, kcnt)
			for i := 0; i < kcnt; i++ {
				key := mrb.AryRef(keys, i)
				val := mrb.HashGet(o, key)
				hash[mrb.String(key)] = mrb.Intf(val)
			}
			return hash
		}

		hash := make(map[interface{}]interface{}, kcnt)
		for i := 0; i < kcnt; i++ {
			key := mrb.AryRef(keys, i)
			val := mrb.HashGet(o, key)
			hash[mrb.Intf(key)] = mrb.Intf(val)
		}
		return hash

	case C.MRB_TT_STRING:
		s := o.Value().v
		return C.GoStringN(C._RSTRING_PTR(s), C.int(C._RSTRING_LEN(s)))
	case C.MRB_TT_RANGE:
		return MrbRangePtr(o)
	case C.MRB_TT_EXCEPTION:
		return errors.New(mrb.IVGet(o, mrb.Intern("mesg")).String())
	case C.MRB_TT_ENV:
		return REnv{(*C.struct_REnv)(C._mrb_ptr(o.Value().v)), mrb}
	case C.MRB_TT_FIBER:
		return RFiber{(*C.struct_RFiber)(C._mrb_ptr(o.Value().v))}
	case C.MRB_TT_DATA:
		return mrb.DataCheckGetInterface(o)
	case C.MRB_TT_ISTRUCT:
		// TODO: return IStruct interfaces (ratiolnal)
		if mrb.ObjIsKindOf(o, mrb.ClassGet("Complex")) {
			r := mrb.Call(o, "real")
			i := mrb.Call(o, "imaginary")
			return complex(r.Value().Float64(), i.Value().Float64())
		}

		// MRB_TT_ICLASS,      /* 11 */  Internal mrb use
		// MRB_TT_FILE,        /* 19 */  not supported
		// MRB_TT_BREAK,       /* 24 */
	}
	return nil
}

func errorHandler(err *error) {
	if r := recover(); r != nil {
		switch x := r.(type) {
		case string:
			*err = errors.New(x)
		case error:
			*err = x
		default:
			*err = errors.New("unknown error")
		}
	}
}

// RunCode executes oruby code string
func (mrb *MrbState) RunCode(code string, args ...interface{}) error {
	if len(args) > 0 {
		mrb.DefineGlobalConst("ARGV", mrb.Value(args))
	}
	_, err := mrb.Eval(code)
	return err
}

// Eval evaluates code string and returns calculated result
func (mrb *MrbState) Eval(code string) (result RObject, err error) {
	//	defer errorHandler(&err)
	mrb.ExcClear()

	cxt := mrb.MrbcContextNew()
	cxt.SetCaptureErrors(true)
	defer cxt.Free()

	p, err := mrb.ParseString(code, cxt)
	if err != nil {
		return RObject{nilValue.v, mrb}, err
	}
	defer p.Free()

	// Check parse errors
	if p.NErr() > 0 {
		estr := ""
		for i := 0; i < p.NErr(); i++ {
			e := p.ErrorBuffer(i)
			estr += fmt.Sprintf("%s:%d:%d: %s\n", mrb.SymString(p.Filename()), e.LineNo, e.Column, e.Message)
		}
		return RObject{nilValue.v, mrb}, errors.New(estr)
	}

	proc := C.mrb_generate_code(mrb.p, p.p)
	if proc == nil {
		return RObject{nilValue.v, mrb}, mrb.Err()
	}

	result = RObject{C.mrb_run(mrb.p, proc, C.mrb_top_self(mrb.p)), mrb}

	return result, mrb.Err()
}

// procedure mrb_float_to_str(buf PChar, i mrb_float);
// function  str_to_mrb_float(buf PChar) mrb_float;

//mrb_allocf = function(mrb *mrb_state, Buffer unsafe.Pointer, size size_t, ud unsafe.Pointer) Pointer

// Exc returns oruby error
func (mrb *MrbState) Exc() *RObject {
	if mrb.p.exc == nil {
		return nil
	}

	return &RObject{C.mrb_obj_value(unsafe.Pointer(mrb.p.exc)), mrb}
}

// ExcClear clear last exception
func (mrb *MrbState) ExcClear() {
	mrb.p.exc = nil
}

// Unwrap returns only Go error from oruby state
func (mrb *MrbState) Unwrap() error {
	return mrb.Err()
}

// Err returns only Go error from oruby state
func (mrb *MrbState) Err() error {
	exc := mrb.Exc()
	if exc == nil {
		return nil
	}

	r := mrb.Inspect(exc)
	v := mrb.String(mrb.ExcBacktrace(mrb.Exc()))
	fmt.Println(v)
	return errors.New(mrb.StrToCstr(r))
}

// ObjectClass in state
func (mrb *MrbState) ObjectClass() RClass { return RClass{mrb.p.object_class, mrb} }

// ClassClass in state
func (mrb *MrbState) ClassClass() RClass { return RClass{mrb.p.class_class, mrb} }

// ModuleClass in state
func (mrb *MrbState) ModuleClass() RClass { return RClass{mrb.p.module_class, mrb} }

// ProcClass in state
func (mrb *MrbState) ProcClass() RClass { return RClass{mrb.p.proc_class, mrb} }

// StringClass in state
func (mrb *MrbState) StringClass() RClass { return RClass{mrb.p.string_class, mrb} }

// ArrayClass in state
func (mrb *MrbState) ArrayClass() RClass { return RClass{mrb.p.array_class, mrb} }

// HashClass in state
func (mrb *MrbState) HashClass() RClass { return RClass{mrb.p.hash_class, mrb} }

// FloatClass in state
func (mrb *MrbState) FloatClass() RClass { return RClass{mrb.p.float_class, mrb} }

// FixnumClass in state
func (mrb *MrbState) FixnumClass() RClass { return RClass{mrb.p.fixnum_class, mrb} }

// TrueClass in state
func (mrb *MrbState) TrueClass() RClass { return RClass{mrb.p.true_class, mrb} }

// FalseClass in state
func (mrb *MrbState) FalseClass() RClass { return RClass{mrb.p.false_class, mrb} }

// NilClass in state
func (mrb *MrbState) NilClass() RClass { return RClass{mrb.p.nil_class, mrb} }

// SymbolClass in state
func (mrb *MrbState) SymbolClass() RClass { return RClass{mrb.p.symbol_class, mrb} }

// KernelModule class in state
func (mrb *MrbState) KernelModule() RClass { return RClass{mrb.p.kernel_module, mrb} }

// EExceptionClass in state
func (mrb *MrbState) EExceptionClass() RClass {
	return RClass{mrb.p.eException_class, mrb}
}

// EStandardErrorClass in state
func (mrb *MrbState) EStandardErrorClass() RClass {
	return mrb.ExcGet("StandardError")
	// return RClass{mrb.p.eStandardError_class, mrb}
}

// DefineClass defines new oruby class
func (mrb *MrbState) DefineClass(name string, parent RClass) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_define_class(mrb.p, cname, parent.p), mrb}
}

// DefineModule defines new oruby module
func (mrb *MrbState) DefineModule(name string) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_define_module(mrb.p, cname), mrb}
}

// SingletonClass creation in oruby state
func (mrb *MrbState) SingletonClass(obj MrbValue) RClass {
	v := C.mrb_singleton_class(mrb.p, obj.Value().v)
	return RClass{(*C.struct_RClass)(C._mrb_ptr(v)), mrb}
}

// IncludeModule Include a module in another class or module.
func (mrb *MrbState) IncludeModule(Parent1, Parent2 RClass) {
	C.mrb_include_module(mrb.p, Parent1.p, Parent2.p)
}

// PrependModule prepends a module in another class or module.
func (mrb *MrbState) PrependModule(cla, prepend RClass) {
	C.mrb_prepend_module(mrb.p, cla.p, prepend.p)
}

//export go_mrb_func_env_callback
func go_mrb_func_env_callback(mrbidx C.mrb_int, self C.mrb_value, idx C.int) C.mrb_value {
	mrb := states[int(mrbidx)]

	fx := mrb.getMrbFuncT(uint(idx))
	if fx == nil {
		method := mrb.SymString(mrb.GetMID())
		return mrb.Raisef(mrb.ERuntimeError(), "go_mrb_func_env_callback: Function '%v' reference not found.", method).v
	}

	return fx(mrb, Value{self}).Value().v
}

//export go_mrb_proc_callback
func go_mrb_proc_callback(mrbidx C.mrb_int, self C.mrb_value) C.mrb_value {
	mrb := states[int(mrbidx)]

	mrb.Lock()
	f := mrb.mrbProcs[C._mrb_ptr(self)]
	mrb.Unlock()

	if f == nil {
		method := mrb.SymString(mrb.GetMID())
		return mrb.Raisef(mrb.ETypeError(), "go_mrb_proc_callback: Function '%v' reference not found.", method).v
	}

	return f(mrb, Value{self}).Value().v
}

// DefineMethod for class
func (mrb *MrbState) DefineMethod(klass RClass, name string, f MrbFuncT, aspec MrbAspec) {
	// function reference is set as oruby function env
	idx := mrb.registerFuncIndex(f)
	C._mrb_method_new_cfunc(mrb.p, klass.p, C.mrb_sym(mrb.Intern(name)), C.int(idx), C.mrb_aspec(aspec))
}

// DefineClassMethod creates new oruby class method
func (mrb *MrbState) DefineClassMethod(klass RClass, name string, f MrbFuncT, aspec MrbAspec) {
	idx := mrb.registerFuncIndex(f)
	C._define_class_method(mrb.p, klass.p, C.mrb_sym(mrb.Intern(name)), C.int(idx), C.mrb_aspec(aspec))
}

// DefineSingletonMethod creates new  method for oruby singleton object
func (mrb *MrbState) DefineSingletonMethod(obj RObject, name string, f MrbFuncT, aspec MrbAspec) {
	if !obj.Value().HasBasic() {
		panic("value does not have RBasic object structure")
	}
	klass := MrbClassPtr(obj)
	idx := mrb.registerFuncIndex(f)
	C._define_class_method(mrb.p, klass.p, C.mrb_sym(mrb.Intern(name)), C.int(idx), C.mrb_aspec(aspec))
}

// DefineModuleFunction creates new oruby module function
func (mrb *MrbState) DefineModuleFunction(klass RClass, name string, f MrbFuncT, aspec MrbAspec) {
	mrb.DefineClassMethod(klass, name, f, aspec)
	mrb.DefineMethod(klass, name, f, aspec)
}

// DefineConst creates new oruby class const
func (mrb *MrbState) DefineConst(klass RClass, name string, value MrbValue) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.mrb_define_const(mrb.p, klass.p, cname, value.Value().v)
}

// UndefMethod removes method from oruby class
// note: function reference stays in matrix
func (mrb *MrbState) UndefMethod(klass RClass, name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	C.mrb_undef_method(mrb.p, klass.p, cname)
}

// UndefClassMethod removes method from oruby class
// note: function reference stays in matrix
func (mrb *MrbState) UndefClassMethod(klass RClass, name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	C.mrb_undef_class_method(mrb.p, klass.p, cname)
}

// ObjNew creates new oruby object
func (mrb *MrbState) ObjNew(c RClass, args ...interface{}) (RObject, error) {
	argv := make([]C.mrb_value, len(args)+1)
	for i := range args {
		argv[i] = mrb.Value(args[i]).v
	}

	v, err := mrb.try(func() C.mrb_value {
		return C.mrb_obj_new(
			mrb.p,
			c.p,
			C.mrb_int(len(args)),
			&argv[0],
		)
	})

	if err != nil {
		return RObject{}, err
	}

	runtime.KeepAlive(argv)
	return RObject{v.v, mrb}, err
}

// ClassNewInstance creates new oruby object, alias for ObjNew
func (mrb *MrbState) ClassNewInstance(c RClass, args ...interface{}) RObject {
	result, err := mrb.ObjNew(c, args...)
	if err != nil {
		panic(err)
	}
	return result
}

// NewInstance creates new oruby object, alias for ObjNew
func (mrb *MrbState) NewInstance(className string, args ...interface{}) RObject {
	result, err := mrb.ObjNew(mrb.ClassGet(className), args...)
	if err != nil {
		panic(err)
	}
	return result
}

// ClassNew creates new class
func (mrb *MrbState) ClassNew(super RClass) (RClass, error) {
	return mrb.tryC(func() *C.struct_RClass {
		return C.mrb_class_new(mrb.p, super.p)
	})
}

// ModuleNew creates new module
func (mrb *MrbState) ModuleNew() RClass {
	return RClass{C.mrb_module_new(mrb.p), mrb}
}

// ClassDefined checks if oruby class is defined
func (mrb *MrbState) ClassDefined(name string) bool {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return C.mrb_class_defined(mrb.p, cname) != 0
}

// ClassGet returns class by name
func (mrb *MrbState) ClassGet(name string) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	if C.mrb_class_defined(mrb.p, cname) == 0 {
		panic("Unknown class: " + name)
	}

	return RClass{C.mrb_class_get(mrb.p, cname), mrb}
}

// ExcGet returns exception class by name
func (mrb *MrbState) ExcGet(name string) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	exc, _ := mrb.tryC(func() *C.struct_RClass {
		return C.mrb_exc_get(mrb.p, cname)
	})
	return exc
}

// ClassDefinedUnder  Returns true if inner class was defined, and false if the inner class was not defined
func (mrb *MrbState) ClassDefinedUnder(outer RClass, name string) bool {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return C.mrb_class_defined_under(mrb.p, outer.p, cname) != 0
}

// ClassGetUnder fiinds class by name under outer class
func (mrb *MrbState) ClassGetUnder(outer RClass, name string) RClass {
	if !mrb.ClassDefinedUnder(outer, name) {
		panic("Unknown class: " + name)
	}

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_class_get_under(mrb.p, outer.p, cname), mrb}
}

// ModuleGet returns module by name
func (mrb *MrbState) ModuleGet(name string) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_module_get(mrb.p, cname), mrb}
}

// ModuleGetUnder returns module by name under outer class
func (mrb *MrbState) ModuleGetUnder(outer RClass, name string) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_module_get_under(mrb.p, outer.p, cname), mrb}
}

// NotImplemented function to raise NotImplementedError with current method name
func (mrb *MrbState) NotImplemented(*MrbState, Value) MrbValue {
	return mrb.ENotImplementedError().Raise("not implemented")
}

// NotImplementM a function to be replacement of unimplemented method - Go version
func NotImplementedM(mrb *MrbState, self Value) MrbValue {
	return mrb.NotImplemented(mrb, self)
}

// ObjDup duplicates MrbValue object
func (mrb *MrbState) ObjDup(obj MrbValue) Value {
	return Value{C.mrb_obj_dup(mrb.p, obj.Value().v)}
}

// ObjRespondTo checks if object responds to method
func (mrb *MrbState) ObjRespondTo(c RClass, mid MrbSym) bool {
	return C.mrb_obj_respond_to(mrb.p, c.p, C.mrb_sym(mid)) != 0
}

// DefineClassUnder defines a class under the namespace of outer.
//
// param outer is a class which contains the new class.
// param name  is a name of the new class
// param super is a class from which the new class will derive.
//               NULL means Object class.
// return the created class
//
// throw TypeError if the constant name name is already taken but
//                  the constant is not a Class.
// throw NameError if the class is already defined but the class can not
//                  be reopened because its superclass is not super.
// post top-level constant named 'name' refers the returned class.
//
// note if class named 'name' is already defined and its superclass is
//       super, the function just returns the defined class.
func (mrb *MrbState) DefineClassUnder(outer RClass, name string, super RClass) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_define_class_under(mrb.p, outer.p, cname, super.p), mrb}
}

// DefineModuleUnder defines module under class
func (mrb *MrbState) DefineModuleUnder(outer RClass, name string) RClass {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return RClass{C.mrb_define_module_under(mrb.p, outer.p, cname), mrb}
}

// ArgsReq required arguments
func ArgsReq(n int) MrbAspec { return MrbAspec((uint32(n) & 0x1f) << 18) }

// ArgsOpt optional arguments
func ArgsOpt(n int) MrbAspec { return MrbAspec((uint32(n) & 0x1f) << 13) }

// ArgsArg mandatory and optinal arguments
func ArgsArg(req, opt int) MrbAspec { return ArgsReq(req) | ArgsOpt(opt) }

// ArgsRest rest argument
func ArgsRest() MrbAspec { return MrbAspec(1 << 12) }

// ArgsPost required arguments after rest
func ArgsPost(n int) MrbAspec { return MrbAspec((uint32(n) & 0x1f) << 7) }

// ArgsKey  arguments (n of keys, kdict)
func ArgsKey(n1, n2 int) MrbAspec {
	if n2 != 0 {
		return MrbAspec(((uint32(n1) & 0x1f) << 2) | (1 << 1))
	}
	return MrbAspec(((uint32(n1) & 0x1f) << 2) | 0)
}

// ArgsBlock block argument
func ArgsBlock() MrbAspec { return MrbAspec(1) }

// ArgsAny accept any number of arguments
func ArgsAny() MrbAspec { return ArgsRest() }

// ArgsNone accept no arguments
func ArgsNone() MrbAspec { return MrbAspec(0) }

// ArgsReq number of required arguments
func (mrb *MrbState) ArgsReq(n int) MrbAspec { return MrbAspec((uint32(n) & 0x1f) << 18) }

// ArgsOpt number of optional arguments
func (mrb *MrbState) ArgsOpt(n int) MrbAspec { return MrbAspec((uint32(n) & 0x1f) << 13) }

// ArgsArg number of required and optional arguments
func (mrb *MrbState) ArgsArg(req, opt int) MrbAspec { return ArgsReq(req) | ArgsOpt(opt) }

// ArgsRest rest arguments
func (mrb *MrbState) ArgsRest() MrbAspec { return MrbAspec(1 << 12) }

// ArgsPost number of post arguments
func (mrb *MrbState) ArgsPost(n int) MrbAspec { return MrbAspec((uint32(n) & 0x1f) << 7) }

// ArgsKey number of key arguments
func (mrb *MrbState) ArgsKey(n1, n2 int) MrbAspec { return ArgsKey(n1, n2) }

// ArgsBlock block argument
func (mrb *MrbState) ArgsBlock() MrbAspec { return MrbAspec(1) }

// ArgsAny any number and type of arguments
func (mrb *MrbState) ArgsAny() MrbAspec { return MrbAspec(1 << 12) }

// ArgsNone no arguments
func (mrb *MrbState) ArgsNone() MrbAspec { return MrbAspec(0) }

// GetMID get method symbol
func (mrb *MrbState) GetMID() MrbSym {
	return MrbSym(mrb.p.c.ci.mid)
}

// Call oruby function, return Go interface,
// in case of error, Call returns nil and the error is in mrb.Err()
func (mrb *MrbState) Call(self MrbValue, name string, args ...interface{}) Value {
	result, _ := mrb.Funcall(self, mrb.Intern(name), args...)
	return result
}

// Funcall call oruby function
func (mrb *MrbState) Funcall(self MrbValue, nameSym MrbSym, args ...interface{}) (Value, error) {
	var err error
	var f reflect.Value
	l := len(args)

	//print("funcall ", mrb.ClassOf(self).Name(), ":", mrb.String(name), "(")

	if (self.Type() == C.MRB_TT_PROC) && mrb.RProc(self).IsCFunc() && (nameSym == mrb.Intern("call")) {
		if C._mrb_proc_has_env(mrb.p, MrbProcPtr(self).p) != 0 {
			fv := C._mrb_proc_env_get(mrb.p, MrbProcPtr(self).p, C.mrb_int(0))
			ff, _ := mrb.getFunc(uint(C._mrb_fixnum(fv)))
			f = reflect.ValueOf(ff)
		}

		if f.IsValid() {
			result := mrb.callFunc(f, RInterfaceArgs{args})
			return mrb.handleResults(result)
		}
	}

	a := make([]C.mrb_value, l+1)
	for i := range args {
		a[i] = mrb.Value(args[i]).v
	}

	v := Value{C._mrb_funcall_with_block(
		mrb.p,
		self.Value().v,
		C.mrb_sym(nameSym),
		C.mrb_int(l),
		(*C.mrb_value)(&a[0]),
		C.mrb_nil_value(),
	)}

	if mrb.ObjIsKindOf(v, mrb.EExceptionClass()) {
		desc := mrb.Call(v, "to_s")
		err = fmt.Errorf("%v: %v - %v", mrb.ClassOf(v).Name(), mrb.String(v), mrb.String(desc))
	}

	runtime.KeepAlive(a)
	return v, err
	// Do not delete comment - function names are used for static API check
	// pure C.mrb_funcall() and C.mrb_funcall_argv() are never called
}

// FuncallWithBlock call function with arguments. Last argument passed should be block
// Valid values for block are RProc types, or Go functions which get converted to RProc value
func (mrb *MrbState) FuncallWithBlock(self MrbValue, nameSym MrbSym, args ...interface{}) (Value, error) {
	block := nilValue
	argc := len(args)

	if argc > 0 {
		block = mrb.Value(args[len(args)-1])
		if block.IsProc() {
			argc--
		} else {
			block = nilValue
		}
	}

	a := make([]C.mrb_value, argc+1)
	for i := range args[:argc] {
		a[i] = mrb.Value(args[i]).v
	}

	var err error
	v := Value{C.mrb_funcall_with_block(
		mrb.p,
		self.Value().v,
		C.mrb_sym(nameSym),
		C.mrb_int(argc),
		(*C.mrb_value)(&a[0]),
		block.v,
	)}

	if mrb.ObjIsKindOf(v, mrb.EExceptionClass()) {
		desc := mrb.Call(v, "to_s")
		err = fmt.Errorf("%v.%v -> %v (%v)",
			mrb.ClassOf(self).Name(),mrb.SymName(nameSym), // ->
			mrb.String(desc), mrb.ClassOf(v).Name(),
		)
	}

	runtime.KeepAlive(a)
	return v, err
}

// Intern converts string to oruby symbol
func (mrb *MrbState) Intern(name string) MrbSym {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	sym := C.mrb_intern(mrb.p, cname, C.size_t(len(name)))
	return MrbSym(sym)
}

// InternStr converts string oruby value to symbol
func (mrb *MrbState) InternStr(val MrbValue) MrbSym {
	return MrbSym(C.mrb_intern_str(mrb.p, val.Value().v))
}

// CheckIntern go string as oruby value
func (mrb *MrbState) CheckIntern(name string) (Value, error) {
	cname := C.CString(name)
	size := len(name)
	defer C.free(unsafe.Pointer(cname))
	return mrb.try(func() C.mrb_value {
		return C.mrb_check_intern(mrb.p, cname, C.size_t(size))
	})
}

// CheckInternStr oruby string to oruby symbol
func (mrb *MrbState) CheckInternStr(val MrbValue) (Value, error) {
	return mrb.try(func() C.mrb_value {
		return C.mrb_check_intern_str(mrb.p, val.Value().v)
	})
}

// SymName returns name of oruby symbol
func (mrb *MrbState) SymName(sym MrbSym) string {
	return C.GoString(C.mrb_sym_name(mrb.p, C.mrb_sym(sym)))
}

// SymNameLen symbol to string
func (mrb *MrbState) SymNameLen(sym MrbSym, size uint) string {
	var s C.mrb_int = C.mrb_int(size)
	cs := C.mrb_sym_name_len(mrb.p, C.mrb_sym(sym), (*C.mrb_int)(&s))
	return C.GoStringN(cs, C.int(s))
}

// SymString symbol to string
func (mrb *MrbState) SymString(sym MrbSym) string {
	var s C.mrb_int
	cs := C.mrb_sym_name_len(mrb.p, C.mrb_sym(sym), (*C.mrb_int)(&s))
	return C.GoStringN(cs, C.int(s))
}

// SymStr symbol to string value
func (mrb *MrbState) SymStr(sym MrbSym) Value {
	return Value{C.mrb_sym_str(mrb.p, C.mrb_sym(sym))}
}

// InternLit in Go does the same as Intern
func (mrb *MrbState) InternLit(lit string) MrbSym { return mrb.Intern(lit) }

// Symbol from oruby value
func (mrb *MrbState) Symbol(v MrbValue) MrbSym {
	return v.Value().Symbol()
}

// InternStatic in Go does the same as Intern
func (mrb *MrbState) InternStatic(str string) MrbSym {
	return mrb.Intern(str)
	// C.mrb_intern_static() is not supported, as CGo guides advise string copy
}

// SymIdx returns current symbol index from MrbState
func (mrb *MrbState) SymIdx() int {
	return int(mrb.p.symidx)
}

// Buff represents memory alocated by mruby C API
type Buff struct {
	p unsafe.Pointer
}

// Uintptr value of underlaying Buff pointer
func (b Buff) Uintptr() uintptr { return uintptr(b.p) }

// Malloc allocates C side memory using oruby allocator
func (mrb *MrbState) Malloc(size uint) Buff {
	return Buff{C.mrb_malloc(mrb.p, C.size_t(size))}
}

// Calloc allocates C side memory using oruby allocator
func (mrb *MrbState) Calloc(num, size uint) Buff {
	return Buff{C.mrb_calloc(mrb.p, C.size_t(num), C.size_t(size))}
}

// Realloc reallocates C side memory using oruby allocator
func (mrb *MrbState) Realloc(buffer Buff, size uint) Buff {
	return Buff{C.mrb_realloc(mrb.p, buffer.p, C.size_t(size))}
}

// ReallocSimple simple version return NULL if no memory available
func (mrb *MrbState) ReallocSimple(buffer Buff, size uint) Buff {
	return Buff{C.mrb_realloc_simple(mrb.p, buffer.p, C.size_t(size))}
}

// MallocSimple simple version return NULL if no memory available
func (mrb *MrbState) MallocSimple(size uint) Buff {
	return Buff{C.mrb_malloc_simple(mrb.p, C.size_t(size))}
}

// ObjAlloc allocate memory for oruby basic object
func (mrb *MrbState) ObjAlloc(vtype int, klass RClass) RBasic {
	return RBasic{C.mrb_obj_alloc(mrb.p, uint32(vtype), klass.p)}
}

// Free calls oruby free to release C side memory
func (mrb *MrbState) Free(buffer Buff) {
	p := buffer.p
	buffer.p = nil
	C.mrb_free(mrb.p, p)
}

// StrNew Allocates new C string from go string
func (mrb *MrbState) StrNew(s string) Value {
	cs := C.CString(s)
	size := len(s)
	defer C.free(unsafe.Pointer(cs))
	return Value{C.mrb_str_new(mrb.p, cs, C.size_t(size))}
}

// StrNewStatic is an alias for StrNew
func (mrb *MrbState) StrNewStatic(s string) Value {
	cs := C.CString(s)
	size := len(s)
	defer C.free(unsafe.Pointer(cs))

	return Value{C.mrb_str_new(mrb.p, cs, C.size_t(size))}
	// C.mrb_str_new_static is unsupported in Go
}

// ObjFreeze freeze value
func (mrb *MrbState) ObjFreeze(v MrbValue) Value {
	return Value{C.mrb_obj_freeze(mrb.p, v.Value().v)}
}

// StrNewFrozen create frozen string value
func (mrb *MrbState) StrNewFrozen(s string) RString {
	return RString{RObject{
		mrb.ObjFreeze(mrb.StrNew(s)).v,
		mrb,
	}}
}

// MrbOpen opens new oruby state, internaly it calls New,
// in case of error - nil state is returned
func MrbOpen() *MrbState {
	mrb, err := New()
	if err != nil {
		return nil
	}
	return mrb
}

//func C.mrb_open_core(mrb_allocf, void *ud) MrbState is unsupportedd
//func C.mrb_open_allocf(allocf mrb_allocf, ud Pointer) mrb_state is unsupported.
//func C.mrb_default_allocf(mrb_state*, void*, size_t, void*) is unsupported

// TopSelf value
func (mrb *MrbState) TopSelf() Value { return Value{C.mrb_top_self(mrb.p)} }

// TopAdjustStackLength of toplevel environment. Used in imrb
func (mrb *MrbState) TopAdjustStackLength(nlocals int) {
	e := REnv{mrb.p.c.cibase.env, mrb}
	e.AdjustStackLength(nlocals)
}

// Run proc with value
func (mrb *MrbState) Run(proc RProc, val MrbValue) Value {
	return Value{C.mrb_run(mrb.p, proc.p, val.Value().v)}
}

// TopRun execution
func (mrb *MrbState) TopRun(proc RProc, self MrbValue, stackKeep int) Value {
	return Value{C.mrb_top_run(mrb.p, proc.p, self.Value().v, C.uint(stackKeep))}
}

// VMRun run proc in VM
func (mrb *MrbState) VMRun(proc RProc, self MrbValue, stackKeep int) Value {
	return Value{C.mrb_vm_run(mrb.p, proc.p, self.Value().v, C.uint(stackKeep))}
}

// VMExec executes ISeq bytecode in mruby VM
// NOTE: this does not pass slice pointer to C. As in
//
//    C.mrb_vm_exec(mrb.p, proc.p, (*C.mrb_code)(&iseq[0]))
//
// It probably can be that way since:
//
//    * mrb_vm_exec() does not change bytecode, iseq is readonly
//    * iseq passed to mrb_vm_exec() is not stored on C side.
//      ISeq bytcode is processed byte by byte, and executed
//    * After exit from mrb_vm_exec(), iseq can be handled by Go GC
//
// This should pass CGO checks, but mrb_vm_exec() can be a long running;
// long as in days, months, years. It feels natural to
// make C copy of bytecode, and let C run in its own way
func (mrb *MrbState) VMExec(proc RProc, iseq []MrbCode) Value {
	if len(iseq) == 0 {
		return nilValue
	}
	ciseq := C.CBytes(iseq)
	defer C.free(ciseq)

	return Value{C.mrb_vm_exec(mrb.p, proc.p, (*C.mrb_code)(ciseq))}
}

// ContextRun proc is an alias for VMRun()
func (mrb *MrbState) ContextRun(p RProc, v MrbValue, stackKeep int) Value {
	return mrb.VMRun(p, v, stackKeep)
}

// P kernel#p print function
func (mrb *MrbState) P(v MrbValue) { C.mrb_p(mrb.p, v.Value().v) }

// ObjID returns oruby value id
func (mrb *MrbState) ObjID(obj MrbValue) int { return int(C.mrb_obj_id(obj.Value().v)) }

// ObjToSym get oruby symbol value
func (mrb *MrbState) ObjToSym(obj MrbValue) MrbSym {
	return MrbSym(C.mrb_obj_to_sym(mrb.p, obj.Value().v))
}

// ObjEq checks if objects are equal
func (mrb *MrbState) ObjEq(v1, v2 MrbValue) bool {
	return C.mrb_obj_eq(mrb.p, v1.Value().v, v2.Value().v) != 0
}

// ObjEqual checks if objects are equal
func (mrb *MrbState) ObjEqual(v1, v2 MrbValue) bool {
	return C.mrb_obj_equal(mrb.p, v1.Value().v, v2.Value().v) != 0
}

// Equal check if values are equal
func (mrb *MrbState) Equal(v1, v2 MrbValue) bool {
	return C.mrb_equal(mrb.p, v1.Value().v, v2.Value().v) != 0
}

// ConvertToInteger using base
func (mrb *MrbState) ConvertToInteger(val MrbValue, base int) (Value, error) {
	return mrb.try(func() C.mrb_value {
		return C.mrb_convert_to_integer(mrb.p, val.Value().v, C.mrb_int(base))
	})
}

// Integer returns integer from value
func (mrb *MrbState) Integer(val MrbValue) (Value, error) {
	return mrb.try(func() C.mrb_value {
		return C.mrb_Integer(mrb.p, val.Value().v)
	})
}

// Float returns float from value
func (mrb *MrbState) Float(val MrbValue) (Value, error) {
	return mrb.try(func() C.mrb_value {
		return C.mrb_Float(mrb.p, val.Value().v)
	})
}

// Inspect returns object info
func (mrb *MrbState) Inspect(obj MrbValue) Value {
	return Value{C.mrb_inspect(mrb.p, obj.Value().v)}
}

// Eql checks if values are equal
func (mrb *MrbState) Eql(obj1, obj2 MrbValue) bool {
	return C.mrb_eql(mrb.p, obj1.Value().v, obj2.Value().v) != 0
}

// Cmp compares oruby object values
func (mrb *MrbState) Cmp(obj1, obj2 MrbValue) int {
	return int(C.mrb_cmp(mrb.p, obj1.Value().v, obj2.Value().v))
}

// GarbageCollect collect garbage
func (mrb *MrbState) GarbageCollect() { C.mrb_garbage_collect(mrb.p) }

// FullGC Full garbage collection
func (mrb *MrbState) FullGC() {
	C.mrb_full_gc(mrb.p)
}

// IncrementalGC incremental garbage collection
func (mrb *MrbState) IncrementalGC() { C.mrb_incremental_gc(mrb.p) }

// GCArenaSave save GC arena
func (mrb *MrbState) GCArenaSave() int32 { return int32(C.mrb_gc_arena_save(mrb.p)) }

// GCArenaRestore restore GC arena
func (mrb *MrbState) GCArenaRestore(n int32) { C.mrb_gc_arena_restore(mrb.p, C.int(n)) }

// GCMark mark GC
func (mrb *MrbState) GCMark(o RBasic) { C.mrb_gc_mark(mrb.p, o.p) }

// GCMarkValue marks GC ov values
func (mrb *MrbState) GCMarkValue(val MrbValue) {
	if val.Value().HasBasic() {
		C.mrb_gc_mark(mrb.p, RBASIC(val).p)
	}
}

// FieldWriteBarrier Paint obj(Black) -> value(White) to obj(Black) -> value(Gray).
func (mrb *MrbState) FieldWriteBarrier(obj1, obj2 RBasic) {
	C.mrb_field_write_barrier(mrb.p, obj1.p, obj2.p)
}

// FieldWriteBarrierValue write barrier vale
func (mrb *MrbState) FieldWriteBarrierValue(obj RBasic, val MrbValue) {
	if val.Type() >= MrbTTHasBasic {
		C.mrb_field_write_barrier(mrb.p, obj.p, (*C.struct_RBasic)(MrbBasicPtr(val).p))
	}
}

// WriteBarrier  Paint obj(Black) to obj(Gray).
// The object that is painted gray will be traversed atomically in final
// mark phase. So you use this write barrier if it's frequency written spot.
// e.g. Set element on Array.
func (mrb *MrbState) WriteBarrier(o RBasic) { C.mrb_write_barrier(mrb.p, o.p) }

// CheckConvertType check type conversion
func (mrb *MrbState) CheckConvertType(val MrbValue, mrbtype uint32, tname, method string) (Value, error) {
	ctname := C.CString(tname)
	defer C.free(unsafe.Pointer(ctname))
	cmethod := C.CString(method)
	defer C.free(unsafe.Pointer(cmethod))
	return mrb.try(func() C.mrb_value {
		return C.mrb_check_convert_type(mrb.p, val.Value().v, mrbtype, ctname, cmethod)
	})
}

// AnyToS returns string value of obj
// The default to_s prints the object's class and an encoding of the
//  object id. As a special case, the top-level object that is the
//  initial execution context of Ruby programs returns "main."
func (mrb *MrbState) AnyToS(obj MrbValue) Value {
	return Value{C.mrb_any_to_s(mrb.p, obj.Value().v)}
}

// ObjClassname returns class name of object
func (mrb *MrbState) ObjClassname(obj MrbValue) string {
	return C.GoString(C.mrb_obj_classname(mrb.p, obj.Value().v))
}

// ObjClass returns class of object
func (mrb *MrbState) ObjClass(obj MrbValue) RClass {
	return RClass{C.mrb_obj_class(mrb.p, obj.Value().v), mrb}
}

// ClassPath returns class path
func (mrb *MrbState) ClassPath(c RClass) Value { return Value{C.mrb_class_path(mrb.p, c.p)} }

// ConvertType using method
func (mrb *MrbState) ConvertType(val MrbValue, mrbtype uint32, tname, method string) (Value, error) {
	ctname := C.CString(tname)
	defer C.free(unsafe.Pointer(ctname))
	cmethod := C.CString(method)
	defer C.free(unsafe.Pointer(cmethod))
	return mrb.try(func() C.mrb_value {
		return C.mrb_convert_type(mrb.p, val.Value().v, mrbtype, ctname, cmethod)
	})
}

// ObjIsKindOf check kind of obj
//     obj.is_a?(class)       => true or false
//     obj.kind_of?(class)    => true or false
//
//  Returns <code>true</code> if <i>class</i> is the class of
//  <i>obj</i>, or if <i>class</i> is one of the superclasses of
//  <i>obj</i> or modules included in <i>obj</i>.
//
//     module M;    end
//     class A
//       include M
//     end
//     class B < A; end
//     class C < B; end
//     b = B.new
//     b.instance_of? A   #=> false
//     b.instance_of? B   #=> true
//     b.instance_of? C   #=> false
//     b.instance_of? M   #=> false
//     b.kind_of? A       #=> true
//     b.kind_of? B       #=> true
//     b.kind_of? C       #=> false
//     b.kind_of? M       #=> true
func (mrb *MrbState) ObjIsKindOf(obj MrbValue, c RClass) bool {
	switch c.Type() {
	case C.MRB_TT_MODULE, C.MRB_TT_CLASS, C.MRB_TT_ICLASS, C.MRB_TT_SCLASS:
		return C.mrb_obj_is_kind_of(mrb.p, obj.Value().v, c.p) != 0
	default:
		panic(fmt.Sprintf("class or module required but got %v", c.Name()))
	}
}

// ObjInspect returns object info
// call-seq:
//    obj.inspect   -> string
//
// Returns a string containing a human-readable representation of
// <i>obj</i>. If not overridden and no instance variables, uses the
// <code>to_s</code> method to generate the string.
// <i>obj</i>.  If not overridden, uses the <code>to_s</code> method to
// generate the string.
//
//    [ 1, 2, 3..4, 'five' ].inspect   #=> "[1, 2, 3..4, \"five\"]"
//    Time.new.inspect                 #=> "2008-03-08 19:43:39 +0900"
func (mrb *MrbState) ObjInspect(oself MrbValue) Value {
	return Value{C.mrb_obj_inspect(mrb.p, oself.Value().v)}
}

// ObjClone clones object
// call-seq:
//    obj.clone -> an_object
//
// Produces a shallow copy of <i>obj</i>---the instance variables of
// <i>obj</i> are copied, but not the objects they reference. Copies
// the frozen state of <i>obj</i>. See also the discussion
// under <code>Object#dup</code>.
//
//    class Klass
//       attr_accessor :str
//    end
//    s1 = Klass.new      #=> #<Klass:0x401b3a38>
//    s1.str = "Hello"    #=> "Hello"
//    s2 = s1.clone       #=> #<Klass:0x401b3998 @str="Hello">
//    s2.str[1,4] = "i"   #=> "i"
//    s1.inspect          #=> "#<Klass:0x401b3a38 @str=\"Hi\">"
//    s2.inspect          #=> "#<Klass:0x401b3998 @str=\"Hi\">"
//
// This method may have class-specific behavior.  If so, that
// behavior will be documented under the #+initialize_copy+ method of
// the class.
//
// Some Class(True False Nil Symbol Fixnum Float) Object  cannot clone.
func (mrb *MrbState) ObjClone(oself MrbValue) Value {
	return Value{C.mrb_obj_clone(mrb.p, oself.Value().v)}
}

//* need to include <ctype.h> to use these macros */
//#ifndef ISPRINT
//#define ISASCII(c) isascii((int)(unsigned char)(c))
//#define ISASCII(c) 1
//#undef ISPRINT
//#define ISPRINT(c) (ISASCII(c) && isprint((int)(unsigned char)(c)))
//#define ISSPACE(c) (ISASCII(c) && isspace((int)(unsigned char)(c)))
//#define ISUPPER(c) (ISASCII(c) && isupper((int)(unsigned char)(c)))
//#define ISLOWER(c) (ISASCII(c) && islower((int)(unsigned char)(c)))
//#define ISALNUM(c) (ISASCII(c) && isalnum((int)(unsigned char)(c)))
//#define ISALPHA(c) (ISASCII(c) && isalpha((int)(unsigned char)(c)))
//#define ISDIGIT(c) (ISASCII(c) && isdigit((int)(unsigned char)(c)))
//#define ISXDIGIT(c) (ISASCII(c) && isxdigit((int)(unsigned char)(c)))
//#define TOUPPER(c) (ISASCII(c) ? toupper((int)(unsigned char)(c)) : (c))
//#define TOLOWER(c) (ISASCII(c) ? tolower((int)(unsigned char)(c)) : (c))
//#endif

// ExcNew creates new exception object
func (mrb *MrbState) ExcNew(c RClass, msg string) Value {
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	return Value{C.mrb_exc_new(mrb.p, c.p, cmsg, C.size_t(len(msg)))}
}

// ExcRaise raises Ruby exception. This function is likeley to cause
// panic and program error exit, since Go neither supports exceptions,
// nor C style longjmp across Go stack.
//
//  Instead, consider using this idiom in case of error:
//
//     return mrb.Raise(mrb.StandardError(), "Something went wrong")
//
//  or
//
//     return mrb.StandardError().Raise("Something went wrong")
//
//  This will return Exception from Go, and raise it on C side
func (mrb *MrbState) ExcRaise(exc MrbValue) {
	C.mrb_exc_raise(mrb.p, exc.Value().v)
}

// Raise raises Exception from err class.
// If class is Exception descendant, then itself is raised
// If class is not Exception descendant, then Exception is raised
//
// Error is returned as Exception Value and raised from C proxy function outside
// of current Go stack.
//
// MRubuy API C.mrb_raise() is never called from Go
func (mrb *MrbState) Raise(err RClass, msg string) Value {
	e := err
	for !e.IsNil() {
		if e.p == mrb.p.eException_class {
			e = err
			break
		}
		e = e.Super()
	}

	if e.IsNil() {
		e = mrb.EExceptionClass()
	}

	ret := mrb.ExcNew(e, msg)
	mrb.p.exc = C._mrb_obj_ptr(ret.v)

	return ret
}

// Raisef exception with formated message
func (mrb *MrbState) Raisef(c RClass, format string, args ...interface{}) Value {
	msg := fmt.Sprintf(format, args...)
	return mrb.Raise(c, msg)
	// pure C.mrb_raisef() is never called
}

// RaiseError returns exception from error. If error is one of predefined oruby
// errors then coresponding ruby error is raised. For example:
//
//    err := oruby.EArgumentError("Unknovn argument %v", someArg)
//    return mrb.RaiseError(err) -> oruby.Value <#ArgumentError>
//
func (mrb *MrbState) RaiseError(err error) Value {
	return mrb.Raise(mrb.getErrorKlass(err), err.Error())
}

// NameError error
func (mrb *MrbState) NameError(id MrbSym, format string, args ...interface{}) Value {
	msg := mrb.SymName(id) + ": " + fmt.Sprintf("%v:"+format, args...)
	return mrb.ENameError().Raise(msg)
	// pure C.mrb_name_error() is never called
}

// FrozenError error
func (mrb *MrbState) FrozenError(obj MrbValue) Value {
	return mrb.EFrozenError().Raisef("can't modify frozen %v", mrb.TypeName(obj))
	// C.mrb_frozen_error not called
}

// Warn error
func (mrb *MrbState) Warn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	C._mrb_warn(mrb.p, cmsg)
	// pure C.mrb_warn() is never called
}

// Bug error
func (mrb *MrbState) Bug(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	C._mrb_bug(mrb.p, cmsg)
	// pure C.mrb_bug() is never called
}

// PrintBacktrace print backtrace
func (mrb *MrbState) PrintBacktrace() { C.mrb_print_backtrace(mrb.p) }

// PrintError prints error
func (mrb *MrbState) PrintError() { C.mrb_print_error(mrb.p) }

// ERuntimeError oruby error
func (mrb *MrbState) ERuntimeError() RClass { return mrb.ExcGet("RuntimeError") }

// ETypeError oruby error
func (mrb *MrbState) ETypeError() RClass { return mrb.ExcGet("TypeError") }

// EArgumentError oruby error
func (mrb *MrbState) EArgumentError() RClass { return mrb.ExcGet("ArgumentError") }

// EIndexError oruby error
func (mrb *MrbState) EIndexError() RClass { return mrb.ExcGet("IndexError") }

// ERangeError oruby error
func (mrb *MrbState) ERangeError() RClass { return mrb.ExcGet("RangeError") }

// ENameError oruby error
func (mrb *MrbState) ENameError() RClass { return mrb.ExcGet("NameError") }

// ENoMethodError oruby error
func (mrb *MrbState) ENoMethodError() RClass { return mrb.ExcGet("NoMethodError") }

// EScriptError oruby error
func (mrb *MrbState) EScriptError() RClass { return mrb.ExcGet("ScriptError") }

// ESyntaxError oruby error
func (mrb *MrbState) ESyntaxError() RClass { return mrb.ExcGet("SyntaxError") }

// ELocalJumpError oruby error
func (mrb *MrbState) ELocalJumpError() RClass { return mrb.ExcGet("LocalJumpError") }

// ERegexpError oruby error
func (mrb *MrbState) ERegexpError() RClass { return mrb.ExcGet("RegexpError") }

// EFrozenError oruby error
func (mrb *MrbState) EFrozenError() RClass { return mrb.ExcGet("FrozenError") }

// ESysStackError oruby error
func (mrb *MrbState) ESysStackError() RClass { return mrb.ExcGet("SystemStackError") }

// EZeroDivisionError oruby error
func (mrb *MrbState) EZeroDivisionError() RClass { return mrb.ExcGet("ZeroDivisionError") }

// ENotImplementedError oruby error
func (mrb *MrbState) ENotImplementedError() RClass { return mrb.ExcGet("NotImplementedError") }

// EFloatDomainError oruby error
func (mrb *MrbState) EFloatDomainError() RClass { return mrb.ExcGet("FloatDomainError") }

// EKeyError oruby error
func (mrb *MrbState) EKeyError() RClass { return mrb.ExcGet("KeyError") }

// EKeyError oruby error
func (mrb *MrbState) ESystemCallError() RClass { return mrb.ExcGet("SystemCallError") }

// Yield block with value
func (mrb *MrbState) Yield(b, arg MrbValue) (Value, error) {
	return mrb.try(func() C.mrb_value {
		return C.mrb_yield(mrb.p, b.Value().v, arg.Value().v)
	})
}

// YieldArgv mrb_value mrb_yield_argv(mrb_state *mrb, mrb_value b, int argc, mrb_value *argv);
func (mrb *MrbState) YieldArgv(b MrbValue, argv ...interface{}) (Value, error) {
	argc := len(argv)

	if argc == 0 {
		return mrb.try(func() C.mrb_value {
			return C.mrb_yield_argv(mrb.p, b.Value().v, 0, nil)
		})
	}

	args := make([]C.mrb_value, argc)
	for i := range args {
		args[i] = mrb.Value(argv[i]).v
	}

	return mrb.try(func() C.mrb_value {
		return C.mrb_yield_argv(mrb.p, b.Value().v, C.mrb_int(argc), (*C.mrb_value)(&args[0]))
	})
}

// YieldWithClass yields with class
func (mrb *MrbState) YieldWithClass(b MrbValue, self MrbValue, c RClass, args ...interface{}) (Value, error) {
	argc := len(args)
	if argc == 0 {
		return mrb.try(func() C.mrb_value {
			return C.mrb_yield_with_class(mrb.p, b.Value().v, 0, nil, self.Value().v, c.p)
		})
	}

	argv := make([]C.mrb_value, argc)
	for i := range args {
		argv[i] = mrb.Value(args[i]).v
	}

	return mrb.try(func() C.mrb_value {
		return C.mrb_yield_with_class(mrb.p, b.Value().v, C.mrb_int(argc), &argv[0], self.Value().v, c.p)
	})
}

// YieldCont continue execution to the proc
// this function should always be called as the last function of a method. e.g.:
//
//      return mrb,YieldCont(proc, self, args...)
func (mrb *MrbState) YieldCont(b RProc, self MrbValue, args ...interface{}) Value {
	//mrb_yield_cont(mrb_state*mrb, mrb_value b, mrb_value self, mrb_int argc, const mrb_value *argv);
	if b.IsNil() {
		return mrb.EArgumentError().Raise("no block given")
	}

	argc := len(args)
	if argc == 0 {
		ret, err := mrb.try(func() C.mrb_value {
			return C.mrb_yield_cont(mrb.p, b.Value().v, self.Value().v, 0, nil)
		})
		if err != nil {
			return mrb.RaiseError(err)
		}

		return ret
	}

	argv := make([]C.mrb_value, argc)
	for i := range args {
		argv[i] = mrb.Value(args[i]).v
	}

	ret, err := mrb.try(func() C.mrb_value {
		return C.mrb_yield_cont(mrb.p, b.Value().v, self.Value().v, C.mrb_int(argc), &argv[0])
	})

	if err != nil {
		return mrb.RaiseError(err)
	}

	return ret
}

// GCProtect protect value from GC
func (mrb *MrbState) GCProtect(obj MrbValue) { C.mrb_gc_protect(mrb.p, obj.Value().v) }

// GCRegister keeps the object from GC. */
func (mrb *MrbState) GCRegister(obj MrbValue) {
	C.mrb_gc_register(mrb.p, obj.Value().v)
}

// GCUnregister removes the object from GC root. */
func (mrb *MrbState) GCUnregister(obj MrbValue) {
	C.mrb_gc_unregister(mrb.p, obj.Value().v)
}

// ToInt converts value to integer oruby value
func (mrb *MrbState) ToInt(val MrbValue) Value {
	return Value{C.mrb_to_int(mrb.p, val.Value().v)}
}

// ToStr converts value to string oruby value
func (mrb *MrbState) ToStr(val MrbValue) RString {
	s := mrb.Call(val, "to_s")
	return RString{RObject{C.mrb_to_str(mrb.p, s.v), mrb}}
}

// CheckType check type and raise error on mismatch
func (mrb *MrbState) CheckType(x MrbValue, ttype int) error {
	return mrb.tryE(func() {
		C.mrb_check_type(mrb.p, x.Value().v, uint32(ttype))
	})
}

// CheckFrozen raise exception if object is frozen
func (mrb *MrbState) CheckFrozen(o MrbValue) error {
	if o.Value().HasBasic() && MrbFrozenP(o) {
		return mrb.tryE(func() {
			C.mrb_frozen_error(mrb.p, unsafe.Pointer(RBASIC(o).p))
		})
	}
	return nil
}

// call_type enum
const (
	CallPublic = iota
	CallFCall
	CallVCall
	CallTypeMax
)

// DefineAlias defines an alias of a method.
// \param mrb    the oruby state
// \param klass  the class which the original method belongs to
// \param name1  a new name for the method
// \param name2  the original name of the method
func (mrb *MrbState) DefineAlias(klass RClass, name1, name2 string) {
	cname1 := C.CString(name1)
	defer C.free(unsafe.Pointer(cname1))
	cname2 := C.CString(name2)
	defer C.free(unsafe.Pointer(cname2))
	_ = mrb.tryE(func() {
		C.mrb_define_alias(mrb.p, klass.p, cname1, cname2)
	})
}

// ClassName returns name of oruby class
func (mrb *MrbState) ClassName(klass RClass) string {
	return C.GoString(C.mrb_class_name(mrb.p, klass.p))
}

// DefineGlobalConst defines global const
func (mrb *MrbState) DefineGlobalConst(name string, val MrbValue) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.mrb_define_global_const(mrb.p, cname, val.Value().v)
}

// AttrGet sets attr_get :symbol for attribute getter
func (mrb *MrbState) AttrGet(obj MrbValue, id MrbSym) Value {
	return Value{C.mrb_attr_get(mrb.p, obj.Value().v, C.mrb_sym(id))}
}

// RespondTo checks if object responds to method id
func (mrb *MrbState) RespondTo(obj MrbValue, mid MrbSym) bool {
	return C.mrb_respond_to(mrb.p, obj.Value().v, C.mrb_sym(mid)) != 0
}

// ObjIsInstanceOf checks if oruby object is direct instance of class
func (mrb *MrbState) ObjIsInstanceOf(obj MrbValue, klass RClass) bool {
	return C.mrb_obj_is_instance_of(mrb.p, obj.Value().v, klass.p) != 0
}

// FuncBasicP returns true if function is basic method id
func (mrb *MrbState) FuncBasicP(obj MrbValue, mid MrbSym, f MrbFuncT) bool {
	m := mrb.MethodSearchVM(mrb.ClassOf(obj), mid)

	p := RProc{C._MRB_METHOD_PROC(m.m), mrb}
	if p.IsNil() || !p.IsCFunc() || p.HasEnv() {
		return false
	}

	f2, err := mrb.getFunc(uint(p.EnvGet(0).Int()))
	if err != nil {
		return false
	}

	return reflect.ValueOf(f).Pointer() == reflect.ValueOf(f2).Pointer()
	// C.mrb_func_basic_p() never called
}

// FiberResume resume a Fiber. Implemented in oruby-fiber
func (mrb *MrbState) FiberResume(fib MrbValue, args ...interface{}) Value {
	argc := len(args) + 1
	cargs := make([]C.mrb_value, argc)

	for i := 0; i < argc; i++ {
		cargs[i] = mrb.Value(args[i-1]).v
	}

	v := C.mrb_fiber_resume(mrb.p, fib.Value().v, C.mrb_int(argc), &cargs[0])
	runtime.KeepAlive(cargs)

	return Value{v}
}

// FiberYield yields fiber
func (mrb *MrbState) FiberYield(args ...interface{}) (Value, error) {

	l := len(args)

	if l == 0 {
		return mrb.try(func() C.mrb_value {
			return C.mrb_fiber_yield(mrb.p, 0, nil)
		})
	}

	a := make([]C.mrb_value, l)
	for i := range args {
		a[i] = mrb.Value(args[i]).v
	}

	return mrb.try(func() C.mrb_value {
		return C.mrb_fiber_yield(mrb.p, C.mrb_int(l), (*C.mrb_value)(&a[0]))
	})
}

// FiberAliveP check if fiber is alive
func (mrb *MrbState) FiberAliveP(fib MrbValue) Value {
	return Value{C.mrb_fiber_alive_p(mrb.p, fib.Value().v)}
}

// EFiberError reference. Implemented in oruby-fiber
func (mrb *MrbState) EFiberError() RClass { return mrb.ExcGet("FiberError") }

// StackExtend extend stack
func (mrb *MrbState) StackExtend(size int) error {
	return mrb.tryE(func() {
		C.mrb_stack_extend(mrb.p, C.mrb_int(size))
	})
}

// MrbPool struct
type MrbPool struct{ p *C.struct_mrb_pool }

// PoolOpen opens new pool
func (mrb *MrbState) PoolOpen() MrbPool { return MrbPool{C.mrb_pool_open(mrb.p)} }

// Close closes pool
func (pool *MrbPool) Close() { C.mrb_pool_close(pool.p) }

// Alloc alocates size memory in pool
func (pool *MrbPool) Alloc(size int) Buff {
	return Buff{C.mrb_pool_alloc(pool.p, C.size_t(size))}
}

// Realloc realocates size memory in pool
func (pool *MrbPool) Realloc(buffer Buff, oldlen, newlen uint) Buff {
	return Buff{C.mrb_pool_realloc(pool.p, buffer.p, C.size_t(oldlen), C.size_t(newlen))}
}

// CanRealloc check if memory can be reallocated
func (pool *MrbPool) CanRealloc(buffer Buff, size uint) bool {
	return C.mrb_pool_can_realloc(pool.p, buffer.p, C.size_t(size)) != 0
}

// Alloca temporary memory allocation, only effective while GC arena is kept
func (mrb *MrbState) Alloca(size uint) Buff {
	return Buff{C.mrb_alloca(mrb.p, C.size_t(size))}
}

// StateAtextit set exis func
func (mrb *MrbState) StateAtextit(f MrbAtexitFunc) {
	// C.mrb_state_atexit(mrb.p, f);
	// Unsupported in go
}

// ShowVersion print oruby version
func (mrb *MrbState) ShowVersion() {
	C.mrb_show_version(mrb.p)
}

// ShowCopyright print oruby copyright
func (mrb *MrbState) ShowCopyright() {
	C.mrb_show_copyright(mrb.p)
}

// C.mrb_format() is unsupported

// mrb_assert(p) assert(p)
//func (mrb *MrbState) GC_mark_mt(cl RClass)           { C.mrb_gc_mark_mt(mrb.p, cl.p) }
//func (mrb *MrbState) GC_mark_mt_size(cl RClass) uint { return uint(C.mrb_gc_mark_mt_size(mrb.p, cl.p)) }
//func (mrb *MrbState) GC_free_mt(cl RClass)           { C.mrb_gc_free_mt(mrb.p, cl.p) }

// GC functions
//func (mrb *MrbState) GC_mark_hash(hash RHash) { C.mrb_gc_mark_hash(mrb.p, hash.p) }
//func (mrb *MrbState) GC_mark_hash_size(hash RHash) int {
//	return int(C.mrb_gc_mark_hash_size(mrb.p, hash.p))
//}
//func (mrb *MrbState) GC_free_hash(hash RHash) { C.mrb_gc_free_hash(mrb.p, hash.p) }

//func calc_crc_16_ccitt(src *uint8, nbytes uint, crc uint16) uint16 {
//	return uint16(C.calc_crc_16_ccitt((*C.uint8_t)(src), C.size_t(nbytes), C.uint16_t(crc)))
//}

// GC
//type each_object_callback = func(mrb mrb_state, obj RBasic)
//func mrb_objspace_each_objects(mrb mrb_state, callback each_object_callback)

// FreeContext free context
func (mrb *MrbState) FreeContext(c MrbContext) { C.mrb_free_context(mrb.p, c.p) }

// Go specific

//export mrb_free_goref
func mrb_free_goref(cmrb *C.mrb_state, p unsafe.Pointer) {
	mrb := states[int(C._mrb_get_idx(cmrb))]
	mrb.setHook(p, nil)
}

//export go_gofunc_callback
func go_gofunc_callback(mrbidx C.mrb_int, self C.mrb_value, idx C.int) C.mrb_value {
	mrb := states[int(mrbidx)]
	var result []reflect.Value
	var err error

	// println("\nCalling", mrb.ClassOf(Value{self}).Name(), mrb.SymString(mrb.GetMID()))

	ff, _ := mrb.getFunc(uint(idx))
	f := reflect.ValueOf(ff)
	if f.Kind() != reflect.Func {
		return mrb.ERuntimeError().Raisef("go_gofunc_callback: '%v' reference invalid", mrb.SymString(mrb.GetMID())).v
	}

	// fetch args
	args := C.mrb_get_argv(mrb.p)
	argc := int(C.mrb_get_argc(mrb.p))
	//argsSlice := (*[1 << 28]C.mrb_value)(unsafe.Pointer(args))[:argc:argc]

	var goself interface{}
	rcvr := 0

	// Check if fn is a method. If it is - receiver is the first argument
	if (C._mrb_type(self) == C.MRB_TT_DATA) || (C._mrb_type(self) == C.MRB_TT_OBJECT) {
		goself = mrb.getHook(C._mrb_ptr(self))
		if goself != nil {
			rcvr = 1
		}
	}

	variadic := 0
	if f.Type().IsVariadic() {
		variadic = 1
	}

	// Check number of params
	if (argc + rcvr) < (f.Type().NumIn() - variadic) {
		//println("Calling", mrb.ClassOf(Value{self}).Name(), mrb.SymString(mrb.GetMID()))
		return mrb.Raisef(mrb.ERuntimeError(), "%v: Expected %d parameters supplied %d.", f.Type().Name(), f.Type().NumIn(), argc+rcvr).v
	}

	in := make([]reflect.Value, rcvr+argc)

	// First argument is receiver, if it is expected
	if rcvr == 1 {
		in[0] = reflect.ValueOf(goself)
	}

	// Others as passed
	for i := rcvr; i < argc+rcvr; i++ {
		arg := mrb.Intf(Value{C._mrb_get_arg(args, C.int(i-rcvr))})

		if variadic == 1 && i >= f.Type().NumIn()-1 {
			in[i] = reflect.ValueOf(arg)
			continue
		}

		inType := f.Type().In(i)
		in[i], err = assignValue(arg, inType)
		if err != nil {
			return mrb.RaiseError(err).v
		}
	}

	// Call
	result = f.Call(in)

	res, err := mrb.handleResults(result)
	if err != nil {
		return mrb.Raise(mrb.getErrorKlass(err), err.Error()).v
	}

	return res.v
}

// DefineModuleFunc define module function
func (mrb *MrbState) DefineModuleFunc(klass RClass, name string, f interface{}) {
	v := reflect.ValueOf(f)

	if v.Kind() != reflect.Func {
		panic(fmt.Sprintf("DefineModuleFunc: Expected func type, got %v", v.Kind()))
	}

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	argc := v.Type().NumIn()
	// function reference is set as oruby function env
	env := mrb.registerFunc(f)

	proc := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_gofunc_callback), C.mrb_int(1), &env)
	m := C._MRB_PROC_CFUNC(proc)
	C.mrb_define_module_function(mrb.p, klass.p, cname, (*[0]byte)(m), C.mrb_aspec(ArgsReq(argc)))
}

// DefineClassFunc define class func
func (mrb *MrbState) DefineClassFunc(klass RClass, name string, f interface{}) {
	v := reflect.ValueOf(f)

	if v.Kind() != reflect.Func {
		panic(fmt.Sprintf("DefineClassFunc: Expected func type, got %v", v.Kind()))
	}

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	argc := v.Type().NumIn()
	env := mrb.registerFunc(f)

	proc := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_gofunc_callback), C.mrb_int(1), &env)
	m := C._MRB_PROC_CFUNC(proc)
	C.mrb_define_class_method(mrb.p, klass.p, cname, (*[0]byte)(m), C.mrb_aspec(ArgsReq(argc)))
}

// DefineSingletonFunc gefine golang singleton func
func (mrb *MrbState) DefineSingletonFunc(obj RObject, name string, f interface{}) {
	v := reflect.ValueOf(f)

	if v.Kind() != reflect.Func {
		panic(fmt.Sprintf("DefineSingletonFunc: Expected func type, got %v", v.Kind()))
	}

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	argc := v.Type().NumIn()
	env := mrb.registerFunc(f)

	proc := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_gofunc_callback), C.mrb_int(1), &env)
	m := C._MRB_PROC_CFUNC(proc)
	C.mrb_define_singleton_method(mrb.p, obj.p(), cname, (*[0]byte)(m), C.mrb_aspec(ArgsReq(argc)))
}

// State returns uintptr of C.mrb_state pointer
func (mrb *MrbState) State() uintptr { return uintptr(unsafe.Pointer(mrb.p)) }

// Context returns context
func (mrb *MrbState) Context() MrbContext { return MrbContext{mrb.p.c} }

// NilValue helper
func (mrb *MrbState) NilValue() Value { return mrb.nilValue }

// FalseValue helper
func (mrb *MrbState) FalseValue() Value { return Value{C.mrb_false_value()} }

// TrueValue helper
func (mrb *MrbState) TrueValue() Value { return Value{C.mrb_true_value()} }

// UndefValue helper
func (mrb *MrbState) UndefValue() Value { return Value{C.mrb_undef_value()} }

// TestMrbInt is helper for size tests since go tests disallow import "C"
func TestMrbInt(i int) C.mrb_int { println(unsafe.Sizeof(C.mrb_int(0))); return C.mrb_int(i) }

// Interface implements Converter interfaces for MrbSym
func (v MrbSym) Interface(*MrbState) interface{} { return int(v) }

// Value implements MrbValue interface for MrbSym
func (v MrbSym) Value() Value { return MrbSymbolValue(v) }

// Type implenets MrbValue interface
func (v MrbSym) Type() int { return MrbTTSymbol }

// IsNil implementes MrbValue interface
func (v MrbSym) IsNil() bool { return false }

func mrbErrorHandler(mrb *MrbState, old *C.struct_mrb_jmpbuf, err *error) {
	mrb.p.jmp = old
	if r := recover(); r != nil {
		switch x := r.(type) {
		case string:
			*err = errors.New(x)
		case error:
			*err = x
		default:
			*err = errors.New("unknown error")
		}
	}

	if *err == nil {
		*err = mrb.Err()
	}
}

func (mrb *MrbState) tryC(f func() *C.struct_RClass) (result RClass, err error) {
	old := mrb.p.jmp
	mrb.p.jmp = nil
	defer mrbErrorHandler(mrb, old, &err)

	result = RClass{f(), mrb}

	return result, err
}

func (mrb *MrbState) try(f func() C.mrb_value) (result Value, err error) {
	old := mrb.p.jmp
	mrb.p.jmp = nil
	defer mrbErrorHandler(mrb, old, &err)

	result = Value{f()}

	return result, err
}

func (mrb *MrbState) tryE(f func()) (err error) {
	old := mrb.p.jmp
	mrb.p.jmp = nil
	defer mrbErrorHandler(mrb, old, &err)

	f()
	return err
}
