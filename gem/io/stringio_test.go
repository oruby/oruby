package io_test

import (
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func openStringIOState(t *testing.T) *oruby.MrbState {
	t.Helper()
	mrb := oruby.MrbOpen()
	t.Cleanup(mrb.Close)
	if _, err := mrb.Resolve("stringio"); err != nil {
		t.Fatal(err)
	}
	return mrb
}

func Test_sioLazyActivation(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("defined?(StringIO)")
	assert.NilError(t, err)
	assert.True(t, ret.IsNil())

	loaded, err := mrb.Resolve("stringio")
	assert.NilError(t, err)
	assert.True(t, loaded)

	loaded, err = mrb.Resolve("stringio")
	assert.NilError(t, err)
	assert.False(t, loaded)

	ret, err = mrb.Eval("StringIO.new.class == StringIO")
	assert.NilError(t, err)
	assert.True(t, ret)
}

func Test_sioOpen(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("StringIO.open('some_string').string")
	assert.NilError(t, err)
	assert.Equal(t, ret, "some_string")
}

func Test_sioInit(t *testing.T) {
	mrb := openStringIOState(t)

	mrb.SetGV("$s", mrb.StrNew("some_string"))

	ret, err := mrb.Eval("$io = StringIO.new($s); $io.class == StringIO")
	assert.NilError(t, err)
	assert.True(t, ret)

	ret, err = mrb.Eval("$io.write('_add'); $s")
	assert.NilError(t, err)
	assert.Equal(t, ret, "_add_string")

	ret, err = mrb.Eval("$io.write('_add'); $s")
	assert.NilError(t, err)
	assert.Equal(t, ret, "_add_adding")

	ret, err = mrb.Eval("$io.write('_add'); $s")
	assert.NilError(t, err)
	assert.Equal(t, ret, "_add_add_add")
}

func Test_sioTty(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("StringIO.new.tty?")
	assert.NilError(t, err)
	assert.False(t, ret)
}

func Test_sioFileno(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("StringIO.new.fileno")
	assert.NilError(t, err)
	assert.True(t, ret.IsNil())
}

func Test_sioSize(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("StringIO.new('asdasd').size")
	assert.NilError(t, err)
	assert.Equal(t, ret, len("asdasd"))
}

func Test_sioTruncate(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("$s = StringIO.new('asdasd')")
	assert.NilError(t, err)

	ret, err = mrb.Eval("$s.truncate(0)")
	assert.NilError(t, err)
	assert.Equal(t, ret, 0)

	ret, err = mrb.Eval("$s.string")
	assert.NilError(t, err)
	assert.Equal(t, ret, "")
}

func Test_sioSetString(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("$s = StringIO.new('asdasd')")
	assert.NilError(t, err)

	ret.Call("string=", "new")
	assert.NilError(t, mrb.Err())

	s, err := mrb.Eval("$s.string")
	assert.NilError(t, err)
	assert.Equal(t, s, "new")

	ret.Call("write", "_add")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Call("string"), "_add")
}

func Test_sioString(t *testing.T) {
	mrb := openStringIOState(t)

	ret, err := mrb.Eval("$s = StringIO.new('asdasd')")
	assert.NilError(t, err)

	ret.Call("string=", "new")
	assert.NilError(t, mrb.Err())

	ret, err = mrb.Eval("$s.string")
	assert.NilError(t, err)
	assert.Equal(t, ret, "new")
}

func Test_sioIndependentStates(t *testing.T) {
	first := openStringIOState(t)
	second := openStringIOState(t)

	ret, err := first.Eval("$s = 'first'; $io = StringIO.new($s); $io.write('!'); $s")
	assert.NilError(t, err)
	assert.Equal(t, ret, "!irst")

	ret, err = second.Eval("StringIO.new('second').string")
	assert.NilError(t, err)
	assert.Equal(t, ret, "second")
}
