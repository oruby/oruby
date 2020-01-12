package oruby

import (
	"fmt"
	"testing"
)

func setupClass(t *testing.T, name string) (RClass, func()) {
	t.Helper()
	mrb, closer := setupState(t, name)
	return mrb.ClassGet(name), closer
}

func setupState(t *testing.T, name string) (*MrbState, func()) {
	t.Helper()
	mrb := MrbOpen()
	_, _ = mrb.Eval(fmt.Sprintf(`
		class %v
			def method_one
				"result"
			end
		end
	`, name))
	return mrb, func() {
		mrb.Close()
		if r := recover(); r != nil {
			t.Helper()
			t.Fatal(r)
		}
	}
}

func TestRClass_Alias(t *testing.T) {
	mrb, closer := setupState(t, "Test")
	defer closer()

	class := mrb.ClassGet("Test")
	class.Alias(mrb.Intern("method_two"), mrb.Intern("method_one"))
	object, _ := class.New()

	v1 := mrb.Call(object, "method_one")
	ExpectNilError(t, mrb.Err())
	v2 := mrb.Call(object, "method_two")
	ExpectNilError(t, mrb.Err())

	ExpectEql(t, mrb.Intf(v1), "result")
	ExpectEql(t, mrb.Intf(v2), "result")
}

func TestRClass_ClassDef(t *testing.T) {
}

func TestRClass_ClassMethod(t *testing.T) {
}

func TestRClass_ClassPath(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	ExpectEql(t, class.mrb.Intf(class.ClassPath()), "Test")

	class2 := class.mrb.DefineClass("Test2", class)
	ExpectEql(t, class2.mrb.Intf(class2.ClassPath()), "Test2")

	mod := class.mrb.DefineModule("Mod")
	class3 := class.mrb.DefineClassUnder(mod, "Test3", class2)
	ExpectEql(t, class3.mrb.Intf(class3.ClassPath()), "Mod::Test3")
}

func TestRClass_Const(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	class.Const("TEST", class.mrb.Value(123456))
	v, _ := class.mrb.Eval("Test::TEST")
	ExpectEql(t, class.mrb.Intf(v), 123456)
}

func TestRClass_Def(t *testing.T) {
	mrb, closer := setupState(t, "Test")
	defer closer()

	class := mrb.ClassGet("Test")
	class.Def("name1", func(mrb *MrbState, self Value) MrbValue {
		return mrb.Value("name1_ok")
	}, ArgsNone())

	class.Def("name2", func() string { return "name2_ok" })

	obj, _ := class.New()
	v2 := mrb.Call(obj, "name2")
	v1 := mrb.Call(obj, "name1")

	ExpectEql(t, mrb.Intf(v1), "name1_ok")
	ExpectEql(t, mrb.Intf(v2), "name2_ok")
}

func TestRClass_DefF(t *testing.T) {
}

func TestRClass_DefineAlias(t *testing.T) {
	mrb, closer := setupState(t, "Test")
	defer closer()

	class := mrb.ClassGet("Test")
	class.DefineAlias("method_two", "method_one")
	object, _ := class.New()

	v1 := mrb.Call(object, "method_one")
	v2 := mrb.Call(object, "method_two")

	ExpectEql(t, mrb.Intf(v1), "result")
	ExpectEql(t, mrb.Intf(v2), "result")
}

func TestRClass_Error(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	ExpectEql(t, class.Error(), nil)

	exc := class.mrb.ExcGet("Exception")
	ExpectErr(t, exc.Error(), "Expected error from exception")
}

func TestRClass_Include(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	mod := class.mrb.DefineModule("Mod")
	mod.DefineMethod("fx", func(mrb *MrbState, self Value) MrbValue {
		return mrb.Value("name1_ok")
	}, ArgsNone())

	class.Include(mod)

	obj, _ := class.New()
	v1 := class.mrb.Call(obj, "fx")
	ExpectEql(t, class.mrb.Intf(v1).(string), "name1_ok")
}

func TestRClass_Name(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	ExpectEql(t, class.Name(), "Test")
}

func TestRClass_New(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	obj, _ := class.New()
	v := class.mrb.Call(obj, "method_one")

	ExpectEql(t, class.mrb.Intf(v), "result")
}

func TestRClass_Prepend(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	mod := class.mrb.DefineModule("Mod")
	class.mrb.DefineMethod(mod, "fx", func(mrb *MrbState, self Value) MrbValue {
		return mrb.Value("name1_ok")
	}, ArgsNone())

	class.Prepend(mod)
	obj, _ := class.New()
	v1 := class.mrb.Call(obj, "fx")
	ExpectEql(t, class.mrb.Intf(v1).(string), "name1_ok")
}

func TestRClass_Real(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	ExpectEql(t, class.Real().Name(), "Test")
	//TODO: more
}

func TestRClass_Super(t *testing.T) {
	class, closer := setupClass(t, "Test")
	defer closer()

	ExpectEql(t, class.Super().Name(), "Object")
}
