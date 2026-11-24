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
		`[["<ab>", "ASCII-8BIT"], ["UTF-8", "ASCII-8BIT"], ["\xCF\x80", "ASCII-8BIT"], [:"\xCF\x80", "ASCII-8BIT"]]`)
	eval(t, `CAPITest.hash_pairs({a: 1, "b" => "c"}).inspect`, `["a=1", "b=\"c\""]`)
	eval(t, `class KW; attr_reader :a; def initialize(p, x:) = @a = [p, x]; end; CAPITest.new_kw(KW).a.inspect`, `[0, 1]`)
	eval(t, `CAPITest.block_call([1, 2]).inspect`, `[11, 12]`)
	eval(t, `CAPITest.types([nil, true, false, 1, :s, 1.5, "s", [], {}, Object.new, Object, Kernel, 2**70]).inspect`,
		`[17, 18, 19, 21, 20, 4, 5, 7, 8, 1, 2, 3, 10]`)
	eval(t, `["ab", "π", "ab".b, "ab".encode("UTF-16LE"), "ab".encode("EUC-JP")].map { CAPITest.enc_info(_1) }.inspect`,
		`[[4, 1, true], [4, 1, false], [1, 1, true], [4, 2, false], [3, 1, true]]`)
	eval(t, `CAPITest.st_hash("abc").inspect`, `[true, true]`)
	eval(t, `["a\u00e9\u20ac\u{1F600}", "ab".b + "\xff".b, "\xe2\x82".dup.force_encoding("UTF-8"), "\xc0\x80", "\xff", "\xed\xa0\x80"].map { |s| CAPITest.codepoints(s) rescue $!.message }.inspect`,
		`[[97, 233, 8364, 128512], [97, 98, 255], "invalid byte sequence in UTF-8", "invalid byte sequence in UTF-8", "invalid byte sequence in UTF-8", "invalid byte sequence in UTF-8"]`)
	eval(t, `CAPITest.ary_sum((1..100).to_a)`, `5050`)
	eval(t, `a = [1, 2, 3]; [CAPITest.ary_double(a) { |x| x }, a].inspect`, `[[2, 4, 6], [2, 4, 6]]`)
	eval(t, `a = [1, 2, 3]; CAPITest.ary_double(a); a.inspect`, `[2, 4, 6]`)
	eval(t, `a = [1, 2]; [CAPITest.ary_use(a), a].inspect`, `[:first, [:first, 2]]`)
	eval(t, `CAPITest.ary_fresh([1, 2, 3]).inspect`, `[1, 7, 99, 4]`)
	eval(t, `CAPITest.str_encs.inspect`, `["ASCII-8BIT", "ASCII-8BIT", "ASCII-8BIT", "ASCII-8BIT", "UTF-8", "UTF-8", `+
		`"US-ASCII", "US-ASCII", "UTF-8", "UTF-8", "US-ASCII", "ASCII-8BIT", "UTF-8", "ASCII-8BIT", "EUC-JP", "ASCII-8BIT"]`)
}

func TestExtensionCXX(t *testing.T) {
	if _, err := mrb.Resolve("capi_testext_cxx.so"); err != nil {
		t.Fatal(err)
	}
	eval(t, `[CAPITestCXX.m0, CAPITestCXX.m1(2), CAPITestCXX.mv(1, 2, 3), CAPITestCXX.mv].inspect`, `[:m0, [:m1, 2], 3, 0]`)
	eval(t, `CAPITestCXX.concat("ab", "cd").inspect`, `"abcd"`)
	eval(t, `CAPITestCXX.keys({a: 1, b: 2}).inspect`, `[:a, :b]`)
	eval(t, `CAPITestCXX.protect(7).inspect`, `[7]`)
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
