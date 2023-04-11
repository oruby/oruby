package io

import (
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
)

func Test_sioOpen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("StringIO.open('some_string')")
	assert.NilError(t, err)
	assert.Equal(t, ret.Call("string"), "some_string")

}

func Test_sioInit(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	mrb.SetGV("$s", mrb.StrNew("some_string"))

	ret, err := mrb.Eval("StringIO.new($s)")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTCData)
	assert.Equal(t, ret.Call("string"), "some_string")

	ret.Call("write", "_add")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, mrb.GetGV("$s"), "_add_string")

	ret.Call("write", "_add")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, mrb.GetGV("$s"), "_add_adding")

	ret.Call("write", "_add")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, mrb.GetGV("$s"), "_add_add_add")
}

func Test_sioTty(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("StringIO.new.tty?")
	assert.NilError(t, err)
	assert.False(t, ret)
}

func Test_sioFileno(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("StringIO.new.fileno")
	assert.NilError(t, err)
	assert.True(t, ret.IsNil())
}

func Test_sioSize(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("StringIO.new('asdasd').size")
	assert.NilError(t, err)
	assert.Equal(t, ret, len("asdasd"))
}

func Test_sioTruncate(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("$s = StringIO.new('asdasd')")
	assert.NilError(t, err)

	ret, err = mrb.Eval("$s.truncate")
	assert.NilError(t, err)
	assert.Equal(t, ret, 0)

	ret, err = mrb.Eval("$s.string")
	assert.NilError(t, err)
	assert.Equal(t, ret, "")
}

func Test_sioSetString(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

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
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("$s = StringIO.new('asdasd')")
	assert.NilError(t, err)

	ret.Call("string=", "new")
	assert.NilError(t, mrb.Err())

	ret, err = mrb.Eval("$s.string")
	assert.NilError(t, err)
	assert.Equal(t, ret, "new")
}
