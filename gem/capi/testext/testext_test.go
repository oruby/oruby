package testext

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
)

// One state for the whole test: the C API binds to the first one.
var mrb = oruby.MrbOpen()

func init() {
	if _, err := mrb.Resolve("core_ext"); err != nil {
		panic(err)
	}
}

func eval(t *testing.T, code, want string) {
	t.Helper()
	v, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf("%s: %v", code, err)
	}
	if got := mrb.String(v); got != want {
		t.Fatalf("%s\n got: %s\nwant: %s", code, got, want)
	}
}

func TestExtension(t *testing.T) {
	if _, err := mrb.Resolve("capi_testext.so"); err != nil {
		t.Fatal(err)
	}
	eval(t, `CAPITest::ANSWER.inspect`, `42`)
	eval(t, `CAPITest.scan(1).inspect`, `[1, 1, nil, nil, [], nil, false]`)
	eval(t, `CAPITest.scan(1, 2, 3, 4, 5, k: 6) { }.inspect`, `[5, 1, 2, 3, [4, 5], {k: 6}, true]`)
	eval(t, `(CAPITest.scan rescue $!.message)`, `wrong number of arguments (given 0, expected 1+)`)
	eval(t, `CAPITest.yield2(3) { |a, b| a + b }.inspect`, `9`)
	eval(t, `CAPITest.yield2(3).inspect`, `:no_block`)
	eval(t, `CAPITest.protect(:x).inspect`, `[1, "bad thing 42 x"]`)
	eval(t, `CAPITest.rescue(:x).inspect`, `[:caught, ArgumentError]`)
	eval(t, `[(CAPITest.ensure(:y) rescue $!.class), CAPITest.ensure_hit].inspect`, `[ArgumentError, true]`)
	eval(t, `CAPITest.numbers(2**40).inspect`,
		`[4611686018427387903, -4611686018427387904, 18446744073709551615, 1.5, 1.0e+300, 1099511627777]`)
	eval(t, `CAPITest.strings("ab").map { |x| x.is_a?(String) ? [x, x.encoding.to_s] : [x, x.encoding.to_s] }.inspect`,
		`[["<ab>", "UTF-8"], ["UTF-8", "UTF-8"], ["\xCF\x80", "ASCII-8BIT"], [:"\xCF\x80", "ASCII-8BIT"]]`)
	eval(t, `CAPITest.hash_pairs({a: 1, "b" => "c"}).inspect`, `["a=1", "b=\"c\""]`)
	eval(t, `class KW; attr_reader :a; def initialize(p, x:) = @a = [p, x]; end; CAPITest.new_kw(KW).a.inspect`, `[0, 1]`)
	eval(t, `CAPITest.block_call([1, 2]).inspect`, `[11, 12]`)
	eval(t, `CAPITest.types([nil, true, false, 1, :s, 1.5, "s", [], {}, Object.new, Object, Kernel, 2**70]).inspect`,
		`[17, 18, 19, 21, 20, 4, 5, 7, 8, 1, 2, 3, 10]`)
}

// A Data object's dmark keeps what it holds alive, and a registered
// address keeps its VALUE: both through full GCs.
func TestGC(t *testing.T) {
	if _, err := mrb.Resolve("capi_testext.so"); err != nil {
		t.Fatal(err)
	}
	eval(t, `$boxes = 200.times.map { |i| CAPITest::Box.new("v#{i}" * 3) }; GC.start; 20.times { Array.new(1000) { "x" * 50 } }; GC.start; $boxes.map(&:held).last`, `v199v199v199`)
	eval(t, `(CAPITest::Box.allocate.held rescue $!.class).inspect`, `nil`)
	eval(t, `GC.start; CAPITest.cached`, `kept by rb_gc_register_address`)
}
