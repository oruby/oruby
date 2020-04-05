package popen

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"testing"
)

func Test_ioPopen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`$p = IO.popen("/bin/echo test")`)
	assert.NilError(t, err)

	ret := mrb.Call(v, "readlines")
	assert.NilError(t, mrb.Err())
	assert.Expect(t, ret.IsArray(), "array expected, got %v", mrb.String(ret))
	assert.Expect(t, ret.Len() > 0,  "array len expected")
	println(ret.Len())

	r := mrb.AryPop(ret)
	assert.Expect(t, r.IsString(), "string item expected")
	assert.Expect(t, r.String() == "test\n", "array expected")

	_,err = mrb.Eval("Process.wait $pid")
	assert.NilError(t, mrb.Err())
}

func Test_ioPopenFork(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`$p = IO.popen("-")`)
	assert.Error(t, err, "popen Fork via ('-') is not supported")
}




