package process

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	_ "github.com/oruby/oruby/gem/assert"
	"os"
	"testing"
)

func TestDaemon(t *testing.T){
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval("Process.daemon")
	assert.Error(t, err, "Process.daemon should raise NotImplemented error")
}

func TestGlobals(t *testing.T){
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval("$$")
	assert.NilError(t, err)
	assert.Equal(t, v.Int(), os.Getpid())

	v, err = mrb.Eval("$?")
	assert.NilError(t, err)
	assert.Expect(t, v.IsNil(), "last status should be nil")

	v, err = mrb.Eval("exec '/bin/echo', 'test'")
	assert.NilError(t, err)

	v, err = mrb.Eval("$?")
	assert.NilError(t, err)
	assert.Expect(t, !v.IsNil(), "last status should not be nil")

}


func TestSpawn(t *testing.T){

}

