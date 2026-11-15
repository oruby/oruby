package marshal_test

import (
	"encoding/hex"
	"testing"
	"unsafe"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func openMarshal(t *testing.T) *oruby.MrbState {
	t.Helper()
	mrb := oruby.MrbOpen()
	t.Cleanup(mrb.Close)
	if _, err := mrb.Resolve("marshal"); err != nil {
		t.Fatalf("Resolve(marshal): %v", err)
	}
	if _, err := mrb.Resolve("core_ext"); err != nil {
		t.Fatalf("Resolve(core_ext): %v", err)
	}
	return mrb
}

func valueBytes(v oruby.Value) []byte {
	ptr, n := oruby.RStringMem(v)
	if n == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(ptr, n)...)
}

func TestMarshalMRIWireFormat(t *testing.T) {
	mrb := openMarshal(t)
	cases := []struct {
		name string
		ruby string
		hex  string
	}{
		{"nil", `Marshal.dump(nil)`, "040830"},
		{"true", `Marshal.dump(true)`, "040854"},
		{"false", `Marshal.dump(false)`, "040846"},
		{"zero", `Marshal.dump(0)`, "04086900"},
		{"positive fixnum", `Marshal.dump(123)`, "040869017b"},
		{"negative fixnum", `Marshal.dump(-125)`, "040869ff83"},
		{"utf8 string", `Marshal.dump("hello")`, "040849220a68656c6c6f063a064554"},
		{"binary string", `Marshal.dump("\xFF".b)`, "04082206ff"},
		{"symbol", `Marshal.dump(:name)`, "04083a096e616d65"},
		{"float", `Marshal.dump(1.5)`, "04086608312e35"},
		{"regexp", `Marshal.dump(Regexp.new("a+", 1))`, "0408492f07612b01063a064546"},
		{"array", `Marshal.dump([1, "x"])`, "04085b07690649220678063a064554"},
		{"cycle", `(cycle = []; cycle << cycle; Marshal.dump(cycle))`, "04085b064000"},
		{"hash", `Marshal.dump({a: 1})`, "04087b063a06616906"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := mrb.Eval(tc.ruby)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got := hex.EncodeToString(valueBytes(value.Value())); got != tc.hex {
				t.Fatalf("wire bytes = %s; MRI 4.1 = %s", got, tc.hex)
			}
		})
	}
}

func TestMarshalLoadsMRI41Fixtures(t *testing.T) {
	mrb := openMarshal(t)
	marshalModule := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Marshal"))
	load := func(encoded string) oruby.Value {
		t.Helper()
		wire, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		value, err := mrb.Funcall(marshalModule, mrb.Intern("load"), mrb.BytesValue(wire))
		if err != nil {
			t.Fatalf("load MRI fixture %s: %v", encoded, err)
		}
		return value
	}

	cycle := load("04085b064000")
	cycleItems := oruby.RArraySlice(cycle)
	if len(cycleItems) != 1 || cycleItems[0].Pointer() != cycle.Pointer() {
		t.Fatalf("MRI cycle fixture did not preserve self-reference: %v", cycleItems)
	}
	shared := load("04085b0749220678063a0645544006")
	sharedItems := oruby.RArraySlice(shared)
	if len(sharedItems) != 2 || sharedItems[0].Pointer() != sharedItems[1].Pointer() {
		t.Fatalf("MRI alias fixture did not preserve identity: %v", sharedItems)
	}
	hash := load("04087d00690c")
	defaultValue, err := mrb.Funcall(hash, mrb.Intern("[]"), mrb.SymbolValue(mrb.Intern("missing")))
	if err != nil || defaultValue.Int64() != 7 {
		t.Fatalf("MRI default-hash fixture = %v, %v; want 7, nil", defaultValue, err)
	}
	if _, err := mrb.Eval(`class MarshalFixtureMri; end`); err != nil {
		t.Fatal(err)
	}
	object := load("04086f3a164d61727368616c466978747572654d7269063a0b406974656d735b0769063a0678")
	items := mrb.IVGet(object, mrb.Intern("@items"))
	if object.Type() != oruby.MrbTTObject || len(oruby.RArraySlice(items)) != 2 {
		t.Fatalf("MRI object fixture did not restore its ivars: %v", items)
	}
	regexp := load("0408492f07612b01063a064546")
	casefold, err := mrb.Funcall(regexp, mrb.Intern("casefold?"))
	vars, varsErr := mrb.Funcall(regexp, mrb.Intern("instance_variables"))
	for _, variable := range oruby.RArraySlice(vars) {
		if mrb.SymName(variable.Symbol()) == "E" {
			t.Fatal("MRI regexp encoding metadata was incorrectly restored as a Ruby instance variable")
		}
	}
	if err != nil || varsErr != nil || !casefold.Bool() {
		t.Fatalf("MRI regexp fixture = %v, %v; want a case-insensitive Regexp", casefold, err)
	}
}

func TestMarshalRoundTripGraphAndHooks(t *testing.T) {
	mrb := openMarshal(t)
	got, err := mrb.Eval(`
    class MarshalHookFixture
      attr_accessor :value
      def initialize
        @value = "initialized"
      end
      def marshal_dump
        [@value]
      end
      def marshal_load(data)
        @value = data[0]
        @loaded = true
      end
      def loaded?; @loaded; end
    end
    cycle = []
    cycle << cycle
    shared = ["shared"]
    hook = MarshalHookFixture.new
    hook.value = "restored"
    restored_cycle, left, right, restored_hook = Marshal.load(Marshal.dump([cycle, shared, shared, hook]))
    restored_cycle.equal?(restored_cycle[0]) && left.equal?(right) &&
      restored_hook.value == "restored" && restored_hook.loaded?
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Marshal graph and custom-hook round trip = %v, %v; want true, nil", got, err)
	}
}

func TestMarshalRegexpAndObjectIvars(t *testing.T) {
	mrb := openMarshal(t)
	if _, err := mrb.Resolve("regexp"); err != nil {
		t.Fatalf("Resolve(regexp): %v", err)
	}
	got, err := mrb.Eval(`
    class MarshalIvarFixture
      attr_accessor :value
    end
    object = MarshalIvarFixture.new
    object.value = [1, :two]
    regexp = Regexp.new("a+", 1)
    copy = Marshal.load(Marshal.dump([object, regexp]))
    [copy[0].class == MarshalIvarFixture, copy[0].value == [1, :two],
      copy[1].source == "a+", copy[1].casefold?].map { |check| check ? "1" : "0" }.join
  `)
	if err != nil || mrb.String(got) != "1111" {
		t.Fatalf("Marshal regexp/object round trip = %v, %v; want 1111, nil", mrb.String(got), err)
	}
}

func TestMarshalIOAndHashDefault(t *testing.T) {
	mrb := openMarshal(t)
	if _, err := mrb.Resolve("stringio"); err != nil {
		t.Fatalf("Resolve(stringio): %v", err)
	}
	got, err := mrb.Eval(`
    io = StringIO.new
    Marshal.dump(1, io)
    Marshal.dump(2, io)
    io.rewind
    values = [Marshal.load(io), Marshal.load(io)]
    hash = Hash.new(7)
    hash[:key] = "value"
    copy = Marshal.load(Marshal.dump(hash))
    [values == [1, 2], copy[:key] == "value", copy[:missing] == 7].map { |check| check ? "1" : "0" }.join
  `)
	if err != nil || mrb.String(got) != "111" {
		t.Fatalf("Marshal IO/default hash round trip = %v, %v; want 111, nil", mrb.String(got), err)
	}
}

func TestMarshalRejectsUnsupportedStringInstanceVariables(t *testing.T) {
	mrb := openMarshal(t)
	if _, err := mrb.Eval(`Marshal.load("\x04\x08I\"\x07hi\x07:\x06EF:\x07@xi\x06")`); err == nil {
		t.Fatal("Marshal.load should reject String instance variables unsupported by this mruby runtime")
	}
}

func TestMarshalUserDumpLoad(t *testing.T) {
	mrb := openMarshal(t)
	got, err := mrb.Eval(`
    class MarshalUserFixture
      attr_reader :value
      def initialize(value); @value = value; end
      def _dump(depth); @value; end
      def self._load(data)
        value = allocate
        value.instance_variable_set(:@value, data)
        value
      end
    end
    Marshal.load(Marshal.dump(MarshalUserFixture.new("payload"))).value == "payload"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Marshal _dump/_load round trip = %v, %v; want true, nil", got, err)
	}
}

func TestMarshalStruct(t *testing.T) {
	mrb := openMarshal(t)
	got, err := mrb.Eval(`
    MarshalFixtureStruct = Struct.new(:left, :right)
    value = MarshalFixtureStruct.new("a", 2)
    copy = Marshal.load(Marshal.dump(value))
    copy.class == MarshalFixtureStruct && copy.left == "a" && copy.right == 2
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Marshal Struct round trip = %v, %v; want true, nil", got, err)
	}
}

func TestMarshalCrossStateAndLimits(t *testing.T) {
	from := openMarshal(t)
	to := openMarshal(t)
	dumped, err := from.Eval(`Marshal.dump(["owned in source", :same])`)
	if err != nil {
		t.Fatal(err)
	}
	marshalModule := to.ConstGet(to.ObjectClass(), to.Intern("Marshal"))
	loaded, err := to.Funcall(marshalModule, to.Intern("load"), to.BytesValue(valueBytes(dumped.Value())))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := to.Funcall(loaded, to.Intern("join"), ":"); err != nil || to.String(got) != "owned in source:same" {
		t.Fatalf("cross-state Marshal result = %q, %v", to.String(got), err)
	}
	for _, ruby := range []string{
		`Marshal.dump([1], 0)`,
		`Marshal.dump(Hash.new { |hash, key| key })`,
		`Marshal.dump(proc {})`,
		`Marshal.load(Marshal.dump(1)) { |value| value }`,
	} {
		if _, err := from.Eval(ruby); err == nil {
			t.Errorf("expected Marshal.dump(%s) to fail", ruby)
		}
	}
}

func TestMarshalRejectsMalformedStreams(t *testing.T) {
	mrb := openMarshal(t)
	for _, ruby := range []string{
		`Marshal.load("\x04\x07")`,
		`Marshal.load("\x04\x08?")`,
		`Marshal.load("\x04\x08@\x06")`,
	} {
		t.Run(ruby, func(t *testing.T) {
			if _, err := mrb.Eval(ruby); err == nil {
				t.Fatalf("Marshal.load(%q) unexpectedly succeeded", ruby)
			}
		})
	}
}
