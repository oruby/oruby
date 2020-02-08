package oruby

import (
	"errors"
	"fmt"
	"testing"
	"time"
	"unsafe"
)

// Test data
var sand = uint(21000)

type TestStruct struct {
	PublicValue *uint
	SecondValue int
}

var testStruct = TestStruct{&sand, 1}

//func NewInnerTestStruct() *TestStruct { return &TestStruct{&sand, Inner{}} }
func NewTestStruct() *TestStruct                 { return &TestStruct{&sand, 2} }
func (m *TestStruct) PublicMethod() uint         { return *m.PublicValue }
func (m *TestStruct) PublicWriterMethod(s uint)  { *m.PublicValue += s }
func (m *TestStruct) SomeMethod(s string) string { return fmt.Sprintf("%s-%v", s, *m.PublicValue) }
func (m *TestStruct) ErrRaiserMethod() error     { return errors.New("ErrorRaised") }

//type Inner struct{}
//var testStruct = TestStruct{&sand, Inner{}}

func TestNewState(t *testing.T) {
	mrb, err := New()
	if err != nil {
		t.Errorf("Create state failed with: %v", err)
	}
	Expect(t, (mrb != nil) && (err == nil), "Failed to create oruby state")
	mrb.Close()
}

func TestEval(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	s := "1 + 1"

	o, err := mrb.Eval(s)
	ExpectNil(t, err, "Eval error: %v", err)
	ExpectEql(t, mrb.Intf(o), 2)
}

func TestValueSimple(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	//Nils
	Expect(t, mrb.Value(nil).IsNil(), "Value(nil) should be Nil")
	Expect(t, MrbNilP(Value{}), "Value{} should be Nil")
	n := Value{}
	Expect(t, n.IsNil(), "Value{} should be Nil")
	Expect(t, !n.IsFixnum(), "Value{} should not be Fixnum")

	// Fixed numeric
	ExpectEql(t, MrbFixnum(mrb.Value(1)), 1)
	ExpectEql(t, MrbFixnum(mrb.Value(int8(8))), 8)
	ExpectEql(t, MrbFixnum(mrb.Value(int16(16))), 16)
	ExpectEql(t, MrbFixnum(mrb.Value(int32(32))), 32)
	ExpectEql(t, MrbFixnum(mrb.Value(int64(64))), 64)

	ExpectEql(t, MrbFixnum(mrb.Value(uint(1))), 1)
	ExpectEql(t, MrbFixnum(mrb.Value(uint8(8))), 8)
	ExpectEql(t, MrbFixnum(mrb.Value(uint16(16))), 16)
	ExpectEql(t, MrbFixnum(mrb.Value(uint32(32))), 32)
	ExpectEql(t, MrbFixnum(mrb.Value(uint64(64))), 64)

	// Floats
	ExpectEql(t, MrbFloat(mrb.Value(float32(123.456))), float64(float32(123.456)))
	ExpectEql(t, MrbFloat(mrb.Value(float64(123.456))), 123.456)

	// Pointers
	i := uint(2)
	x := &i

	ExpectEql(t, MrbCptr(mrb.Value(unsafe.Pointer(x))), uintptr(unsafe.Pointer(x)))
	ExpectEql(t, MrbCptr(mrb.Value(uintptr(unsafe.Pointer(x)))), uintptr(unsafe.Pointer(x)))

	// Stings
	s := "Добро jutro"
	Expect(t, mrb.Value(s).IsString(), "Expecting string mrb value, got MRB_TT %d", MrbType(mrb.Value(s)))
	ExpectEql(t, mrb.Value(s).String(), s)

	// Bytes
	b := []byte("Dobar dan")
	Expect(t, mrb.Value(b).IsString(), "Expecting string mrb value, got MRB_TT %d", mrb.Value(s).Type())
	ExpectEql(t, mrb.Value(b).Bytes(), b)

	// Booleans
	ExpectEql(t, mrb.Value(true).Bool(), true)
	ExpectEql(t, mrb.Value(false).Bool(), false)
}

func TestValue(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	data := mrb.Value(&testStruct)
	intf, ok := mrb.Data(data).(*TestStruct)
	Expect(t, ok, "Data does not containt given pointer")
	ExpectEql(t, intf, &testStruct)

	// Arrays
	a := [10]string{"1", "3", "a", "-", "q", "ß", "sbn", "888", "9", "11"}
	Expect(t, mrb.Value(a).IsArray(), "Expecting Array value, got MRB_TT %d", MrbType(mrb.Value(a)))
	av := mrb.Intf(mrb.Value(a)).([]interface{})
	for i := range a {
		ExpectEql(t, a[i], av[i])
	}

	// Hash maps
	h := map[string]int{"x": 1, "y": 2, "z": 333}
	Expect(t, mrb.Value(h).IsHash(), "Expecting Hash value, got MRB_TT %d", MrbType(mrb.Value(h)))
	hv := mrb.Intf(mrb.Value(h)).(map[string]interface{})
	for k := range h {
		ExpectEql(t, h[k], hv[k])
	}

	// Structs implementing MrbValuer interface
	//v2 := mrb.StringValue("TestTest")
	//vv := &v2.(ValueMigrator)
	//Expect(t, MrbStringP(mrb.Value(vv)), "Expecting string mrb value, got MRB_TT %d", MrbType(mrb.Value(vv)))
	//ExpectEql(t, mrb.StringValueCstr(vv.MigrateTo(mrb)), mrb.StringValueCstr(v))

	// Structs - other
	mv := mrb.Value(testStruct)
	Expect(t, MrbType(mv) == MrbTTData, "Expecting Data mrb value, got MRB_TT %d", MrbType(mv))
	Expect(t, mrb.ObjIsKindOf(mv, mrb.ObjectClass()), "Expecting Object class, got %v", mrb.ClassName(mrb.ClassOf(mv)))
	ExpectEql(t, mrb.String(mrb.Call(mrb.ClassOf(mv), "to_s")), "Object")
}

func TestFuncs(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	f := func(s string) string { return "Prefix-" + s }
	fv := mrb.Value(f)
	Expect(t, MrbType(fv) == MrbTTProc, "Expecting Proc mrb value, got MRB_TT %d", MrbType(mrb.Value(f)))
	fvv := mrb.Call(fv, "call", "X")

	Expect(t, mrb.ClassOf(fvv).Name() != "TypeError", " method call returned TypeError: "+mrb.StrToCstr(mrb.Call(fvv, "to_s")))
	ExpectEql(t, mrb.String(fvv), "Prefix-X")
}

func TestChans(t *testing.T) {
	// Chans
	Pending(t, "reflect.Chan")
}

func TestTime(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	// Structs - time.Time
	tt := time.Now()
	tv := mrb.Value(tt)
	Expect(t, MrbType(tv) == MrbTTData, "Expecting Time as DATA mrb value, got MRB_TT %d", MrbType(tv))
	Expect(t, mrb.ObjClassname(tv) == "Time", "Expecting Time class name, got %v", mrb.ObjClassname(tv))

	tvc := mrb.Intf(tv).(time.Time)

	ExpectEql(t, tvc.Day(), tt.Day())
	ExpectEql(t, tvc.Month(), tt.Month())
	ExpectEql(t, tvc.Year(), tt.Year())
	ExpectEql(t, tvc.Hour(), tt.Hour())
	ExpectEql(t, tvc.Minute(), tt.Minute())
	ExpectEql(t, tvc.Second(), tt.Second())
}

func TestComplex(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	// Test is run only if native C Complex gem exists
	if mrb.ClassDefined("Complex") {
		v := mrb.Value(complex(11.11, 88.88))
		ExpectEql(t, mrb.Value(v), v)
	}
}

func TestConvert(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	ExpectEql(t, mrb.Intf(mrb.Value(nil)), nil)
	ExpectEql(t, mrb.Intf(mrb.Value(true)), true)
	ExpectEql(t, mrb.Intf(mrb.Value(false)), false)

	// Fixnums
	ExpectEql(t, mrb.Intf(mrb.Value(int8(12))), 12)
	ExpectEql(t, mrb.Intf(mrb.Value(int16(12345))), 12345)
	ExpectEql(t, mrb.Intf(mrb.Value(int32(-12345678))), -12345678)
	ExpectEql(t, mrb.Intf(mrb.Value(uint8(12))), 12)
	ExpectEql(t, mrb.Intf(mrb.Value(uint16(12345))), 12345)
	ExpectEql(t, mrb.Intf(mrb.Value(uint32(12345678))), 12345678)

	// Floats
	ExpectEql(t, mrb.Intf(mrb.Value(float32(12345.56))), float64(float32(12345.56)))
	ExpectEql(t, mrb.Intf(mrb.Value(float64(12345.987654321))), 12345.987654321)

	ExpectEql(t, unsafe.Sizeof(int(11)), unsafe.Sizeof(TestMrbInt(11)))

	if Go64bit() {
		//64 bit ints (not supported on 32bit oruby)
		ExpectEql(t, int64(mrb.Intf(mrb.Value(int64(-1234567890123))).(int)), int64(-1234567890123))
		ExpectEql(t, uint64(mrb.Intf(mrb.Value(uint64(1234567890123))).(int)), uint64(1234567890123))
	}

	// Pointers
	i := uint(2)
	x := &i
	ExpectEql(t, mrb.Intf(mrb.Value(x)), x)
	ExpectEql(t, mrb.Intf(mrb.Value(x)), &i)
	ExpectEql(t, mrb.Intf(mrb.Value(unsafe.Pointer(x))), uintptr(unsafe.Pointer(x)))
	ExpectEql(t, mrb.Intf(mrb.Value(&testStruct)), &testStruct)

	// Strings
	ExpectEql(t, mrb.Intf(mrb.Value("ßene")), "ßene")

	// Symbols
	sym := mrb.InternLit("clear")
	ExpectEql(t, mrb.Intf(MrbSymbolValue(sym)), sym)

	// Arrays
	a := [5]string{"1", "set", "Ђура", "ŠĐ", "-"}
	acon := mrb.Intf(mrb.Value(a)).([]interface{})
	for i := range a {
		ExpectEql(t, a[i], acon[i])
	}

	// Hash maps
	h := map[string]int{"x": 1, "y": 2, "z": 333}
	hcon := mrb.Intf(mrb.Value(h)).(map[string]interface{})
	for k := range h {
		ExpectEql(t, h[k], hcon[k])
	}

	// Proc
	f := func(s string) string { return "Usul - " + s }
	f2 := mrb.Intf(mrb.Value(f)).(func(string) string)
	ExpectEql(t, f("X"), f2("X"))

	// time.Time object
	tt := time.Now()
	tvc := mrb.Intf(mrb.Value(tt)).(time.Time)
	ExpectEql(t, tvc.Day(), tt.Day())
	ExpectEql(t, tvc.Month(), tt.Month())
	ExpectEql(t, tvc.Year(), tt.Year())
	ExpectEql(t, tvc.Hour(), tt.Hour())
	ExpectEql(t, tvc.Minute(), tt.Minute())
	ExpectEql(t, tvc.Second(), tt.Second())

	// Struct::TestStruct
	//mv := mrb.Intf(mrb.MigrateTo(testStruct)).(*TestStruct)
	//ExpectEql(t, mv, testStruct)

	// Struct anonymous
	//str := struct {
	//	X1 int
	//	X2 string
	//}{111, "Menthats"}
	//strconv := mrb.Intf(mrb.MigrateTo(str)).(struct {
	//	X1 int
	//	X2 string
	//})
	//ExpectEql(t, str, strconv)

	// Object anonymous -> map[string]interface
	clt := mrb.DefineClass("Test", mrb.ObjectClass())
	obj, _ := mrb.ObjNew(clt)
	_ = mrb.IVSet(obj, mrb.Intern("@x"), mrb.Value(2))
	_ = mrb.IVSet(obj, mrb.Intern("@y"), mrb.Value("Moon"))

	oo := map[string]interface{}{"@x": 2, "@y": "Moon"}
	ocnv := mrb.Intf(obj).(map[string]interface{})

	ExpectEql(t, oo["@x"], ocnv["@x"])
	ExpectEql(t, oo["@y"], ocnv["@y"])
}

func TestConvertGoClass(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	mcls := mrb.DefineGoClass("TestStruct", NewTestStruct)
	ExpectNil(t, mrb.Err(), "Error creating TestStruct class: %v", mrb.Err())

	mobj, _ := mrb.ObjNew(mcls)

	// Object should point to global var
	v := mrb.Call(mobj, "public_method")
	spice := mrb.Intf(v)
	ExpectEql(t, spice, int(sand))

	// Calling ruby method -> should call Go method -> should change Go global
	mrb.Call(mobj, "public_writer_method", uint(10))
	ExpectEql(t, mrb.Intf(mrb.Call(mobj, "public_method")), int(sand))

	sfx := mrb.Call(mobj, "some_method", "pfx")
	ExpectEql(t, mrb.Intf(sfx), fmt.Sprintf("pfx-%v", sand))

	// Call via ruby script
	mrb.GVSet(mrb.Intern("$o"), mobj)

	o, err := mrb.Eval("$o.public_method()")
	ExpectNil(t, err, "Eval error: %v", err)
	ExpectEql(t, mrb.Intf(o), int(sand))

	// oruby RClass structs
	ExpectEql(t, mcls, mrb.Intf(mrb.Value(mcls)))
	ExpectEql(t, mrb.ObjectClass(), mrb.Intf(mrb.Value(mrb.ObjectClass())))

	Pending(t, "MRB_TT_SCLASS: Singleton class")
}

func TestRDataIterface(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	c := mrb.ObjectClass()
	v := mrb.ObjectClass().Value()

	ExpectEql(t, c, mrb.Intf(v))
}

func TestCallGoProcFromScript(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	// Funcs
	f := func(s string) string { return "Usul - " + s }
	fv := mrb.Value(f)
	Expect(t, MrbType(fv) == MrbTTProc, "Expecting Proc mrb value, got MRB_TT %d", MrbType(mrb.Value(f)))

	mrb.GVSet(mrb.Intern("$gofn"), fv)

	o, err := mrb.Eval("$gofn.call('X')")
	ExpectNil(t, err, "Eval error: %v", err)
	ExpectEql(t, mrb.Intf(o), "Usul - X")
}

func TestMrbFuncT(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	x := mrb.Intern("@x")

	cls := mrb.DefineClass("TestMrb", mrb.ObjectClass())

	// Initialize object with one instance variable
	mrb.DefineMethod(cls, "initialize", func(mrb *MrbState, self Value) MrbValue {

		// Note: x is outer variable
		_ = mrb.IVSet(self, x, mrb.Value(0))
		return self

	}, ArgsNone())

	mrb.DefineMethod(cls, "x", func(mrb *MrbState, self Value) MrbValue {
		return mrb.IVGet(self, x)
	}, ArgsNone())

	mrb.DefineMethod(cls, "x=", func(mrb *MrbState, self Value) MrbValue {
		v := mrb.GetArgsFirst()

		_ = mrb.IVSet(self, x, v)
		return v

	}, ArgsReq(1))

	mrb.DefineMethod(cls, "incr", func(mrb *MrbState, self Value) MrbValue {
		incr := 1
		args := mrb.Args()
		v := mrb.IVGet(self, x)

		if len(args) > 0 {
			incr = args[0].Fixnum()
		}

		nv := mrb.NumPlus(v, mrb.Value(incr))
		_ = mrb.IVSet(self, x, nv)

		return nv

	}, ArgsOpt(1))

	// Create object from defined class
	obj, _ := mrb.ObjNew(cls)

	// obj.@x = 0
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), 0)
	// obj.@x = obj.x
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), mrb.Intf(mrb.Call(obj, "x")))

	mrb.Call(obj, "x=", 11)
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), 11)
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), mrb.Intf(mrb.Call(obj, "x")))

	mrb.Call(obj, "incr")
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), 12)
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), mrb.Intf(mrb.Call(obj, "x")))

	mrb.Call(obj, "incr", 11)
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), 23)
	ExpectEql(t, mrb.Intf(mrb.IVGet(obj, x)), mrb.Intf(mrb.Call(obj, "x")))

}

func TestErrorTransformation(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	var fn = func() (int, error) { return 10, nil }

	// Call without error
	mrb.GVSet(mrb.Intern("$fn"), mrb.Value(fn))
	err := mrb.RunCode("$x = $fn.call(); p $x; ")
	//Output:
	//10

	Expect(t, err == nil, "Error '%v' raised from fn.call()", err)
	ExpectEql(t, mrb.Intf(mrb.GVGet(mrb.Intern("$x"))), 10)

	// Error raising call
	errMessage := "you are not the one"
	var fe = func() (int, error) { return 11, errors.New(errMessage) }

	mrb.GVSet(mrb.Intern("$fe"), mrb.Value(fe))
	err = mrb.RunCode("$fe.call()")
	ExpectErr(t, err, "Error not raised from call")
	ExpectEql(t, err.Error(), errMessage+" (StandardError)")
}

func TestMrbState_Scan(t *testing.T) {
	mrb, _ := New()
	defer mrb.Close()

	v, _ := mrb.Eval(`{foo: nil}`)
	i := make(map[string]interface{})
	err := mrb.Scan(v, &i)

	ExpectNil(t, err, "scan must be ok")
	if val, ok := i["foo"]; !ok {
		t.Error("key foo must exist")
	} else {
		Expect(t, val == nil, `"foo" value should be nil`)
	}
}

func ExampleMrbState_RunCode() {
	mrb, _ := New()
	defer mrb.Close()

	_ = mrb.RunCode(`p ARGV[0]['foo']; p ARGV[0]['bar'];`, map[string]interface{}{
		"foo": "bar",
		"bar": "baz",
	})
	//Output:
	//"bar"
	//"baz"
}
