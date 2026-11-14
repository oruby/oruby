package core_ext

import (
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
)

func TestCoreExt(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	mrb.Require("core_ext")

	// Test Encoding
	v, err := mrb.Eval(`Encoding.find("UTF-8").name`)
	assert.NilError(t, err)
	assert.Equal(t, v.String(), "UTF-8")

	// Test Float extensions
	v, err = mrb.Eval(`1.0.prev_float < 1.0`)
	assert.NilError(t, err)
	assert.Equal(t, v.Bool(), true)

	v, err = mrb.Eval(`1.0.next_float > 1.0`)
	assert.NilError(t, err)
	assert.Equal(t, v.Bool(), true)

	// Test Array extensions
	v, err = mrb.Eval(`[1, 2, 3].rotate.to_a`)
	assert.NilError(t, err)
	assert.Equal(t, mrb.AryEntry(v, 0).Int(), 2)
	assert.Equal(t, mrb.AryEntry(v, 1).Int(), 3)
	assert.Equal(t, mrb.AryEntry(v, 2).Int(), 1)

	// Test Thread local storage
	v, err = mrb.Eval(`
		Thread.current[:test_key] = "hello"
		Thread.current[:test_key]
	`)
	assert.NilError(t, err)
	assert.Equal(t, v.String(), "hello")

	// Test Hash extensions
	v, err = mrb.Eval(`{ a: 1, b: 2 }.hash.is_a?(Integer)`)
	assert.NilError(t, err)
	assert.Equal(t, v.Bool(), true)
}
