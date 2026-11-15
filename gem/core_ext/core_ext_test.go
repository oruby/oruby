package core_ext

import (
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	_ "github.com/oruby/oruby/gem/process"
	_ "github.com/oruby/oruby/gem/thread"
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

func TestThreadLocalsAndIdentity(t *testing.T) {
	first := oruby.MrbOpen()
	defer first.Close()
	second := oruby.MrbOpen()
	defer second.Close()

	check := func(mrb *oruby.MrbState, code string) {
		t.Helper()
		v, err := mrb.Eval(code)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Bool() {
			t.Fatalf("Ruby assertion failed: %s", code)
		}
	}

	check(first, `
		Thread.current.equal?(Thread.main) &&
		  Thread.current.equal?(Thread.current) &&
		  Thread.current.is_a?(Thread) &&
		  Thread.current[:isolated_test_key].nil?
	`)
	check(first, `
		Thread.current[:isolated_test_key] = "first"
		Thread.main["isolated_test_key"] == "first" &&
		  Thread.main.key?(:isolated_test_key) &&
		  Thread.main.keys.include?(:isolated_test_key)
	`)
	check(second, `
		Thread.current.equal?(Thread.main) &&
		  Thread.current[:isolated_test_key].nil? &&
		  !Thread.current.key?(:isolated_test_key)
	`)
	check(second, `
		Thread.main[:isolated_test_key] = "second"
		Thread.current[:isolated_test_key] == "second"
	`)
	check(first, `Thread.current[:isolated_test_key] == "first"`)
}

func TestUnsupportedThreadAndDetach(t *testing.T) {
	for _, expression := range []string{
		`Thread.new { raise "block ran" }`,
		`Thread.start { raise "block ran" }`,
		`Thread.go { raise "block ran" }`,
		`Process.detach(2147483647)`,
	} {
		t.Run(expression, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()

			v, err := mrb.Eval(`
				begin
				  ` + expression + `
				  false
				rescue NotImplementedError
				  true
				end
			`)
			if err != nil {
				t.Fatal(err)
			}
			if !v.Bool() {
				t.Fatalf("%s must raise NotImplementedError", expression)
			}
		})
	}
}
