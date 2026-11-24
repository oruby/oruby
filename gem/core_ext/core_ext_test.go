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

func TestVisibilityFailureCallsMethodMissing(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`
      class VisibilityFailure
        private def hidden = :unreachable
        protected def guarded = :unreachable

        private def method_missing(name, *args, **kwargs, &block)
          return [args, kwargs, block.call] if name == :hidden
          super
        end
      end

      obj = VisibilityFailure.new
      visibility_error = begin
        obj.guarded
      rescue NoMethodError => error
        [error.name, error.receiver.equal?(obj), error.message.include?("protected method")]
      end
      expected = [[1], {key: 2}, :block]
      obj.hidden(1, key: 2) { :block } == expected &&
        obj.public_send(:hidden, 1, key: 2) { :block } == expected &&
        visibility_error == [:guarded, true, true]
    `)
	if err != nil || !got.Bool() {
		t.Fatalf("visibility and method_missing = %v, %v; want true", got, err)
	}
}

func TestInheritedVisibilityFollowsRedefinition(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`
      class VisibilityParent
        private def example = :before
      end
      class VisibilityChild < VisibilityParent
        public :example
      end
      obj = VisibilityChild.new
      before = obj.example
      prior_method = obj.method(:example)
      class VisibilityParent
        private def example = :after
      end
      after = obj.example
      class VisibilityParent
        remove_method :example
      end
      before == :before && after == :after && prior_method.owner == VisibilityChild &&
        prior_method.call == :before && !obj.respond_to?(:example)
    `)
	if err != nil || !got.Bool() {
		t.Fatalf("inherited method visibility = %v, %v; want true", got, err)
	}
}

func TestSplatCallsLegacyRespondTo(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`
      class LegacySplat
        def respond_to?(name) = name == :to_a
        def to_a = [1, 2, 3]
      end
      [*LegacySplat.new] == [1, 2, 3]
    `)
	if err != nil || !got.Bool() {
		t.Fatalf("splat with one-argument respond_to? = %v, %v; want true", got, err)
	}
}

func TestRuby2KeywordsPromotesOnlySplatCalls(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`
      def receive(*args, **keywords) = [args, keywords]
      marked = Hash.ruby2_keywords_hash({a: 1})
      positional, keywords = receive(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, marked)
      splatted = receive(*[1, marked])
      positional.length == 15 && positional.last.equal?(marked) && keywords.empty? &&
        splatted == [[1], {a: 1}] && !Hash.ruby2_keywords_hash?(splatted.last)
    `)
	if err != nil || !got.Bool() {
		t.Fatalf("ruby2_keywords argument packing = %v, %v; want true", got, err)
	}
}

func TestLiteralFreezeHonorsOverride(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
      class String
        def freeze = :override
      end
      "literal".freeze == :override && (-String.new("literal")).frozen?
    `)
	if err != nil || !v.Bool() {
		t.Fatalf("literal freeze and unary minus = %v, %v; want true", v, err)
	}
}

func TestLiteralFreezeHonorsRefinement(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
      module LiteralFreezeRefinement
        refine String do
          def freeze = :refined
        end
      end
      using LiteralFreezeRefinement
      "literal".freeze == :refined
    `)
	if err != nil || !v.Bool() {
		t.Fatalf("refined literal freeze = %v, %v; want true", v, err)
	}
}

func TestStringDedupCopiesSubclassContents(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
      class DedupString < String
        def initialize_copy(other)
          raise "unexpected initialize_copy"
        end
      end
      string = DedupString.new("literal".encode("UTF-16LE"))
      string.instance_variable_set(:@tag, 1)
      def string.tag = @tag
      copy = string.dedup
      copy.instance_of?(DedupString) && copy == string && copy.encoding == string.encoding &&
        copy.frozen? && copy.instance_variables.empty? && !copy.respond_to?(:tag) &&
        !string.frozen? && string.tag == 1
    `)
	if err != nil || !v.Bool() {
		t.Fatalf("subclass dedup = %v, %v; want true", v, err)
	}
}

func TestHashCopiesEncodedStringKeys(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
      enc = Encoding::UTF_16LE
      utf16 = ->(ascii) { ascii.bytes.pack("v*").force_encoding(enc) }
      key = utf16.call("e")
      key.instance_variable_set(:@tag, true)
      replacements = {}
      replacements[key] = utf16.call("EH!")
      stored = replacements.keys.first
      key.clear
      result = utf16.call("hello").tr(replacements)
      stored.frozen? && stored.instance_variables.empty? && stored.encoding == enc &&
        result == utf16.call("hEH!llo") && result.encoding == enc
    `)
	if err != nil || !v.Bool() {
		t.Fatalf("encoded hash key and translation = %v, %v; want true", v, err)
	}
}

func TestHashKeepsCustomCodesAcrossTableChanges(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`
      class StoredHashKey
        attr_accessor :code
        attr_reader :calls

        def initialize(code)
          @code = code
          @calls = 0
        end

        def hash
          @calls += 1
          raise "rehash stopped" if @code == :raise
          @code
        end

        def eql?(other)
          raise "eql? called for a different code" if other.is_a?(StoredHashKey)
          false
        end
      end

      key = StoredHashKey.new(7)
      table = {key => :value}
      raise "wrong insert count" unless key.calls == 1
      raise "truncated hash code" if table.key?(StoredHashKey.new((1 << 32) + 7))

      128.times { |i| table[i] = i }
      96.times { |i| table.delete(i) }
      128.times { |i| table[i + 256] = i }
      copy = table.dup
      raise "resize or copy rehashed the key" unless key.calls == 1
      raise "lost key on resize or copy" unless table[key] == :value && copy[key] == :value

      key.code = :raise
      begin
        table.rehash
        raise "expected a rehash failure"
      rescue RuntimeError => error
        raise unless error.message == "rehash stopped"
      end
      key.code = 7
      raise "rehash failure damaged the table" unless table[key] == :value && table[257] == 1
      key.code = 12
      raise "lookup ignored stored code" if table.key?(key)
      raise "shift ignored stored code" unless copy.shift == [key, :value]

      calls = key.calls
      table.compare_by_identity
      raise "identity conversion called hash" unless table[key] == :value && key.calls == calls
      lone = StoredHashKey.new(1)
      empty = {lone => true}
      empty.delete(lone)
      calls = lone.calls
      raise "empty lookup called hash" unless empty[lone].nil? && lone.calls == calls
      begin
        {}[StoredHashKey.new(nil)] = 1
        false
      rescue TypeError
        true
      end
    `)
	if err != nil || !got.Bool() {
		t.Fatalf("custom Hash codes = %v, %v; want true", got, err)
	}
}

func TestHashComparesCustomKeysWithBuiltinKeys(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`
      key = Object.new
      def key.hash = :match.hash
      def key.eql?(other) = other.equal?(:match)

      small = {match: :value}
      large = small.dup
      64.times { |i| large[i] = i }
      32.times { |i| large.delete(i) }
      raise "missing builtin match" unless small[key] == :value && large[key] == :value
      raise "failed to delete a builtin match" unless large.delete(key) == :value && !large.key?(:match)

      large[:match] = :old
      large[key] = :new
      small[key] = :new
      small.keys == [:match] && small[:match] == :new &&
        large.size == 33 && large[:match] == :new && large[key] == :new
    `)
	if err != nil || !got.Bool() {
		t.Fatalf("mixed Hash keys = %v, %v; want true", got, err)
	}
}

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

func TestCoreExtRailsEcosystemRegressions(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.Require("core_ext")

	_, err := mrb.Eval(`
		# 1. Frozen Set#each must not raise FrozenError
		s = Set[1, 2, 3].freeze
		out = []
		s.each { |x| out << x }
		raise "frozen set" unless out.sort == [1, 2, 3]

		# 2. Hash subclass overriding #size or #[] must not recurse in #each
		class SizeHash < Hash
			def size; keys.sum { |k| self[k].to_s.bytesize }; end
		end
		sh = SizeHash.new
		sh["a"] = "hello"
		pairs = []
		sh.each { |k, v| pairs << [k, v] }
		raise "size hash" unless pairs == [["a", "hello"]]

		class ArefHash < Hash
			def [](k)
				each { |ek, ev| return ev if ek.to_s.downcase == k.to_s.downcase }
				nil
			end
		end
		ah = ArefHash.new
		ah["Content-Type"] = "text/plain"
		raise "aref hash" unless ah["content-type"] == "text/plain"

		# 3. Frozen anonymous Module assigned to a constant (Zeitwerk pattern)
		FROZEN_MOD_CONST = Module.new.freeze
		raise "frozen const" unless FROZEN_MOD_CONST.frozen?

		# 4. Module#append_features / prepend_features on singleton class (ActiveJob pattern)
		module SingletonMixin; def ping; :pong; end; end
		class SingletonHost
			class << self
				include SingletonMixin
			end
		end
		raise "sclass include" unless SingletonHost.ping == :pong

		# 5. prepend + module_function must not corrupt ancestors or instance_method.owner
		module PrepMod; end
		module TargetMod
			def helper; :ok; end
		end
		TargetMod.prepend(PrepMod)
		module TargetMod
			module_function
			def mf; :mf_ok; end
		end
		raise "prepend+mf ancestors" unless TargetMod.ancestors == [PrepMod, TargetMod]
		raise "prepend+mf owner" unless TargetMod.instance_method(:helper).owner == TargetMod
		raise "prepend+mf call" unless TargetMod.mf == :mf_ok
	`)
	if err != nil {
		t.Fatalf("Rails ecosystem regressions failed: %v", err)
	}
}

func TestEnumerableDoesNotDefineHash(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		raise "Enumerable should not define #hash" if Enumerable.instance_methods(false).include?(:hash)
		called = false
		cls = Class.new do
			include Enumerable
			define_method(:each) { |&b| called = true; b.call(self) }
		end
		obj = cls.new
		h = obj.hash
		raise "hash should be Integer" unless h.is_a?(Integer)
		raise "Object#hash must not iterate #each" if called
		raise "Array#hash" unless [1, 2].hash == [1, 2].hash
		raise "Hash#hash" unless {a: 1}.hash == {a: 1}.hash
		raise "Range#hash" unless (1..5).hash == (1..5).hash
	`)
	if err != nil {
		t.Fatalf("Enumerable#hash check failed: %v", err)
	}
}
