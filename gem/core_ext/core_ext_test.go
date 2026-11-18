package core_ext

import (
	"runtime"
	"strings"
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

func TestRbConfigPlatformFields(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("rbconfig"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"arch", oruby.RubyArch()},
		{"host_cpu", strings.SplitN(oruby.RubyArch(), "-", 2)[0]},
		{"host_os", runtime.GOOS},
	} {
		got, err := mrb.Eval("RbConfig::CONFIG[" + `"` + tc.key + `"` + "]")
		if err != nil || mrb.String(got) != tc.want {
			t.Fatalf("RbConfig::CONFIG[%q] = %q, %v; want %q", tc.key, mrb.String(got), err, tc.want)
		}
	}
	got, err := mrb.Eval(`RbConfig::CONFIG["host"].is_a?(String) && !RbConfig::CONFIG["host"].empty?`)
	if err != nil || !got.Bool() {
		t.Fatalf("RbConfig host must be a non-empty String: %v, %v", got, err)
	}
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

func TestThreadAndDetach(t *testing.T) {
	for expression, want := range map[string]string{
		`Thread.new { 1 + 1 }.value`:          "2",
		`Thread.start(3) { |x| x * 2 }.value`: "6",
		`t = Thread.new { Thread.current.report_on_exception = false; raise "x" }; begin; t.join; rescue => e; e.message; end`: `"x"`,
		`q = Queue.new; t = Thread.new { q.pop + 1 }; q << 41; t.value`:                                                        "42",
		`m = Mutex.new; n = 0; 4.times.map { Thread.new { 100.times { m.synchronize { n += 1 } } } }.each(&:join); n`:          "400",
		`t = Thread.new { sleep }; Thread.pass until t.stop?; t.kill; t.join; t.alive?`:                                        "false",
		`Process.detach(2147483647).value`: "nil",
	} {
		t.Run(expression, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()
			v, err := mrb.Eval(expression + `.inspect`)
			if err != nil {
				t.Fatal(err)
			}
			if got := mrb.String(v); got != want {
				t.Fatalf("%s = %s, want %s", expression, got, want)
			}
		})
	}
}

// Shims for older mruby must not shadow what mruby provides natively
// (a BasicObject#method_missing shim once made every NoMethodError recurse).
func TestCoreExtNativeOverlap(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.Require("core_ext")

	cases := map[string]string{
		`begin; Object.new.no_such; rescue NoMethodError => e; e.name.to_s; end`:                                                   "no_such",
		`class CeBO < BasicObject; end; begin; CeBO.new.zz; rescue ::NoMethodError; "ok"; end`:                                     "ok",
		`class CeMM; def method_missing(n, *a) = super; end; begin; CeMM.new.x; rescue NoMethodError; "ok"; end`:                   "ok",
		`begin; begin; raise KeyError, "in"; rescue; raise; end; rescue => e; e.class.to_s; end`:                                   "KeyError",
		`begin; begin; raise KeyError; rescue => c; raise ArgumentError, "o", cause: c; end; rescue => e; e.cause.class.to_s; end`: "KeyError",
		`class CeN < Numeric; undef_method :coerce; end; 1.coerce(2.5).inspect`:                                                    "[2.5, 1.0]",
		`caller_locations(0, 1).first.class.to_s`:                                                                                  "Thread::Backtrace::Location",
	}
	for code, want := range cases {
		v, err := mrb.Eval(code)
		if err != nil {
			t.Errorf("%s: %v", code, err)
			continue
		}
		if got := mrb.String(v); got != want {
			t.Errorf("%s = %q, want %q", code, got, want)
		}
	}
}

// Encoding is MRI's class of Encoding objects; strings report them.
func TestCoreExtEncoding(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.Require("core_ext")

	cases := map[string]string{
		`"x".encoding.inspect`:                                      "#<Encoding:UTF-8>",
		`"x".b.encoding.inspect`:                                    "#<Encoding:BINARY (ASCII-8BIT)>",
		`"x".force_encoding("ISO-8859-1").encoding.name`:            "ISO-8859-1",
		`"x".force_encoding("ISO-8859-1").dup.encoding.name`:        "ISO-8859-1",
		`"x".force_encoding("US-ASCII").b.encoding.name`:            "ASCII-8BIT",
		`"x".force_encoding("ISO-8859-1").instance_variables.size`:  "0",
		`:a.encoding.name`:                                          "US-ASCII",
		`__ENCODING__.name`:                                         "UTF-8",
		`Encoding.find("binary").equal?(Encoding::ASCII_8BIT).to_s`: "true",
		`(Encoding::UTF_8 == "UTF-8").to_s`:                         "false",
		`Encoding.compatible?("abc", "é").name`:                     "UTF-8",
		`(Encoding.find("nope") rescue $!.class).to_s`:              "ArgumentError",
		`("a".force_encoding(:BINARY) rescue $!.class).to_s`:        "TypeError",
		`(Encoding.allocate rescue $!.class).to_s`:                  "TypeError",
		`200.chr("ISO-8859-1").encoding.name`:                       "ISO-8859-1",
		`Encoding.list.size.to_s`:                                   "103",
	}
	for code, want := range cases {
		v, err := mrb.Eval(code)
		if err != nil {
			t.Errorf("%s: %v", code, err)
			continue
		}
		if got := mrb.String(v); got != want {
			t.Errorf("%s = %q, want %q", code, got, want)
		}
	}
}
