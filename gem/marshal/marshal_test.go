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
		{"largest MRI fixnum", `Marshal.dump(1073741823)`, "04086904ffffff3f"},
		{"smallest MRI fixnum", `Marshal.dump(-1073741824)`, "040869fc000000c0"},
		{"first positive bignum", `Marshal.dump(1073741824)`, "04086c2b0700000040"},
		{"first negative bignum", `Marshal.dump(-1073741825)`, "04086c2d0701000040"},
		{"large negative bignum", `Marshal.dump(-1267650600228229401496703205376)`, "04086c2d0c0000000000000000000000001000"},
		{"linked bignum", `(number = 1267650600228229401496703205376; Marshal.dump([number, number]))`, "04085b076c2b0c00000000000000000000000010004006"},
		{"exception metadata", `(exception = RuntimeError.new("bad"); exception.set_backtrace(["x.rb:2"]); exception.instance_variable_set(:@extra, :ok); Marshal.dump(exception))`, "04086f3a1152756e74696d654572726f72083a096d657367492208626164063a0645543a0762745b0649220b782e72623a32063b07543a0b4065787472613a076f6b"},
		{"utf8 string", `Marshal.dump("hello")`, "040849220a68656c6c6f063a064554"},
		{"binary string", `Marshal.dump("\xFF".b)`, "04082206ff"},
		{"symbol", `Marshal.dump(:name)`, "04083a096e616d65"},
		{"float", `Marshal.dump(1.5)`, "04086608312e35"},
		{"regexp", `Marshal.dump(Regexp.new("a+", 1))`, "0408492f07612b01063a064546"},
		{"array", `Marshal.dump([1, "x"])`, "04085b07690649220678063a064554"},
		{"cycle", `(cycle = []; cycle << cycle; Marshal.dump(cycle))`, "04085b064000"},
		{"hash", `Marshal.dump({a: 1})`, "04087b063a06616906"},
		{"inclusive range", `Marshal.dump(1..3)`, "04086f3a0a52616e6765083a096578636c463a0a626567696e69063a08656e646908"},
		{"exclusive range", `Marshal.dump(1...3)`, "04086f3a0a52616e6765083a096578636c543a0a626567696e69063a08656e646908"},
		{"exception", `Marshal.dump(ArgumentError.new("bad"))`, "04086f3a12417267756d656e744572726f72073a096d657367492208626164063a0645543a07627430"},
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

func TestMarshalBignumRoundTrip(t *testing.T) {
	mrb := openMarshal(t)
	got, err := mrb.Eval(`
    value = 1267650600228229401496703205376
    copy = Marshal.load(Marshal.dump(value))
    copy.class == Integer && copy == value
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Bignum round trip = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    value = 1267650600228229401496703205376
    copy = Marshal.load(Marshal.dump([value, value]))
    copy[0].equal?(copy[1])
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Bignum object link = %v, %v; want true, nil", got, err)
	}
}

func TestMarshalLoadsMRI41Bignums(t *testing.T) {
	mrb := openMarshal(t)
	marshalModule := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Marshal"))
	for _, tc := range []struct {
		wire string
		want string
	}{
		{"04086c2b0700000040", "1073741824"},
		{"04086c2d0701000040", "-1073741825"},
		{"04086c2b0c0000000000000000000000001000", "1267650600228229401496703205376"},
		{"04086c2d0c0000000000000000000000001000", "-1267650600228229401496703205376"},
	} {
		encoded, err := hex.DecodeString(tc.wire)
		if err != nil {
			t.Fatal(err)
		}
		value, err := mrb.Funcall(marshalModule, mrb.Intern("load"), mrb.BytesValue(encoded))
		if err != nil {
			t.Fatalf("load MRI bignum %s: %v", tc.wire, err)
		}
		text, err := mrb.Funcall(value, mrb.Intern("to_s"))
		if err != nil || mrb.String(text) != tc.want {
			t.Fatalf("MRI bignum %s = %q, %v; want %q", tc.wire, mrb.String(text), err, tc.want)
		}
		if mrb.ClassOf(value).Name() != "Integer" {
			t.Fatalf("MRI bignum %s produced %s, want Integer", tc.wire, mrb.TypeName(value))
		}
	}
}

func TestMarshalRangeAndExceptionFixtures(t *testing.T) {
	mrb := openMarshal(t)
	marshalModule := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Marshal"))
	load := func(encoded string) oruby.Value {
		t.Helper()
		data, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		value, err := mrb.Funcall(marshalModule, mrb.Intern("load"), mrb.BytesValue(data))
		if err != nil {
			t.Fatalf("load MRI fixture %s: %v", encoded, err)
		}
		return value
	}

	inclusive := load("04086f3a0a52616e6765083a096578636c463a0a626567696e69063a08656e646908")
	exclusive := load("04086f3a0a52616e6765083a096578636c543a0a626567696e69063a08656e646908")
	for _, tc := range []struct {
		value     oruby.Value
		exclusive bool
	}{
		{inclusive, false},
		{exclusive, true},
	} {
		if tc.value.Type() != oruby.MrbTTRange {
			t.Fatalf("MRI Range fixture returned %s", mrb.TypeName(tc.value))
		}
		rangeValue, err := mrb.MrbRangePtr(tc.value)
		if err != nil || rangeValue.Begin().Int64() != 1 || rangeValue.End().Int64() != 3 || rangeValue.Exclusive() != tc.exclusive {
			t.Fatalf("MRI Range fixture = (%v, %v, %v), %v", rangeValue.Begin(), rangeValue.End(), rangeValue.Exclusive(), err)
		}
	}

	exception := load("04086f3a12417267756d656e744572726f72073a096d657367492208626164063a0645543a07627430")
	if exception.Type() != oruby.MrbTTException || mrb.ObjClassname(exception) != "ArgumentError" {
		t.Fatalf("MRI Exception fixture = %s, want ArgumentError", mrb.ObjClassname(exception))
	}
	message, err := mrb.Funcall(exception, mrb.Intern("message"))
	if err != nil || mrb.String(message) != "bad" {
		t.Fatalf("MRI Exception message = %q, %v; want bad", mrb.String(message), err)
	}
	nilMessage := load("04086f3a0e457863657074696f6e063a096d657367303a07627430")
	if !mrb.ExcMessage(nilMessage).IsNil() {
		t.Fatal("MRI nil Exception message was not preserved")
	}

	extra := load("04086f3a1152756e74696d654572726f72083a096d657367492208626164063a0645543a0762745b0649220b782e72623a32063b07543a0b4065787472613a076f6b")
	backtrace, err := mrb.Funcall(extra, mrb.Intern("backtrace"))
	instanceVariable, ivarErr := mrb.Funcall(extra, mrb.Intern("instance_variable_get"), mrb.SymbolValue(mrb.Intern("@extra")))
	if err != nil || ivarErr != nil || len(oruby.RArraySlice(backtrace)) != 1 || mrb.String(oruby.RArraySlice(backtrace)[0]) != "x.rb:2" || instanceVariable.Type() != oruby.MrbTTSymbol || mrb.SymName(instanceVariable.Symbol()) != "ok" {
		t.Fatalf("MRI Exception metadata = (%v, %v), %v; want backtrace and @extra", backtrace, instanceVariable, err)
	}

	graph, err := mrb.Eval(`
    items = []
    range = items..nil
    items << range
    restored = Marshal.load(Marshal.dump(range))
    restored.begin[0].equal?(restored)
  `)
	if err != nil || !graph.Bool() {
		t.Fatalf("cyclic Range endpoint round trip = %v, %v; want true, nil", graph, err)
	}
}

func TestMarshalExceptionLoadingSkipsInitializer(t *testing.T) {
	mrb := openMarshal(t)
	if _, err := mrb.Eval(`
    class RuntimeError
      def initialize(*); raise "Marshal must not call exception initialize"; end
    end
	`); err != nil {
		t.Fatal(err)
	}
	encoded, err := hex.DecodeString("04086f3a1152756e74696d654572726f72073a096d657367492208626164063a0645543a07627430")
	if err != nil {
		t.Fatal(err)
	}
	marshalModule := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Marshal"))
	exception, err := mrb.Funcall(marshalModule, mrb.Intern("load"), mrb.BytesValue(encoded))
	if err != nil {
		t.Fatalf("loading exception invoked overridden initialize: %v", err)
	}
	got, err := mrb.Funcall(exception, mrb.Intern("message"))
	if err != nil || mrb.String(got) != "bad" {
		t.Fatalf("loaded exception message = %q, %v; want bad without calling initialize", mrb.String(got), err)
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

// mruby keeps instance variables of Strings in a side table: they round trip
func TestMarshalStringInstanceVariables(t *testing.T) {
	mrb := openMarshal(t)
	got, err := mrb.Eval(`s = Marshal.load("\x04\x08I\"\x07hi\x07:\x06EF:\x07@xi\x06"); [s, s.encoding.to_s, s.instance_variable_get(:@x)]`)
	if err != nil || mrb.String(mrb.Inspect(got)) != `["hi", "US-ASCII", 1]` {
		t.Fatalf("Marshal.load(String with @x) = %v, %v", mrb.String(mrb.Inspect(got)), err)
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
	} {
		if _, err := from.Eval(ruby); err == nil {
			t.Errorf("expected %s to fail", ruby)
		}
	}
}

func TestMarshalRejectsMalformedStreams(t *testing.T) {
	mrb := openMarshal(t)
	for _, ruby := range []string{
		`Marshal.load("\x04\x07")`,
		`Marshal.load("\x04\x08?")`,
		`Marshal.load("\x04\x08@\x06")`,
		`Marshal.load("\x04\x08l?\x00")`,
		`Marshal.load("\x04\x08l+\x06")`,
	} {
		t.Run(ruby, func(t *testing.T) {
			if _, err := mrb.Eval(ruby); err == nil {
				t.Fatalf("Marshal.load(%q) unexpectedly succeeded", ruby)
			}
		})
	}
}

func TestMarshalLoadProcAndTags(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	for code, want := range map[string]string{
		`Marshal.load(Marshal.dump([1, 2]), proc { |v| Integer === v ? v * 10 : v }).inspect`:  "[10, 20]",
		`Marshal.load("\x04\be:\vKernelo:\vObject\x00").singleton_class.include?(Kernel).to_s`: "true",
		`class MSub < Array; end; Marshal.load("\x04\bC:\tMSub[\x06i\x06").class.name`:         "MSub",
	} {
		v, err := mrb.Eval(code)
		if err != nil {
			t.Errorf("%s: %v", code, err)
		} else if v.String() != want {
			t.Errorf("%s = %q, want %q", code, v.String(), want)
		}
	}
}
