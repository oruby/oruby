package oruby

import (
	"bytes"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPreprocessRuby(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "dynamic alias lhs",
			input:    `alias :"#{'a'}" value`,
			expected: `::Kernel.__oruby_alias__(self, :"#{'a'}", :value)`,
		},
		{
			name:     "dynamic alias lhs expression",
			input:    `alias :"#{'a' + ''.to_s}" value`,
			expected: `::Kernel.__oruby_alias__(self, :"#{'a' + ''.to_s}", :value)`,
		},
		{
			name:     "dynamic alias rhs",
			input:    `alias a :"#{'value'}"`,
			expected: `::Kernel.__oruby_alias__(self, :a, :"#{'value'}")`,
		},
		{
			name:     "dynamic alias rhs expression",
			input:    `alias a :"#{'value' + ''.to_s}"`,
			expected: `::Kernel.__oruby_alias__(self, :a, :"#{'value' + ''.to_s}")`,
		},
		{
			name:     "static alias left bare right bare",
			input:    `alias foo bar`,
			expected: `alias foo bar`,
		},
		{
			name:     "static alias symbol",
			input:    `alias :foo :bar`,
			expected: `alias :foo :bar`,
		},
		{
			name:     "static alias quoted symbol",
			input:    `alias :"foo" :"bar"`,
			expected: `alias :"foo" :"bar"`,
		},
		{
			name:     "dynamic undef simple",
			input:    `undef :"#{'meth'}"`,
			expected: `::Kernel.__oruby_undef__(self, :"#{'meth'}")`,
		},
		{
			name:     "dynamic undef expression",
			input:    `undef :"#{'meth'.to_sym}"`,
			expected: `::Kernel.__oruby_undef__(self, :"#{'meth'.to_sym}")`,
		},
		{
			name:     "static undef single",
			input:    `undef meth`,
			expected: `undef meth`,
		},
		{
			name:     "static undef multiple",
			input:    `undef :method1, :method2`,
			expected: `undef :method1, :method2`,
		},
		{
			name:     "method def with **nil (mruby compiles it)",
			input:    `def no_kw(arg1, **nil, &); inner(&) end`,
			expected: `def no_kw(arg1, **nil, &); inner(&) end`,
		},
		{
			name:     "comment with alias does not break",
			input:    `# alias :"#{a}" b`,
			expected: `# alias :"#{a}" b`,
		},
		{
			name:     "string with **nil does not break",
			input:    `s = "**nil"`,
			expected: `s = "**nil"`,
		},
		{
			name:     "rescue safe navigation target",
			input:    `begin; rescue => a&.b; end`,
			expected: `begin; rescue => __rescued_exc__; a&.b = __rescued_exc__; end`,
		},
		{
			name:     "for safe navigation target",
			input:    "for o&.target in [1, 2]\n  o.target\nend",
			expected: "for __for_elem__ in [1, 2]; o&.target = __for_elem__;\n  o.target\nend",
		},
		{
			name:     "begin block desugaring",
			input:    "BEGIN { x = 1 }",
			expected: "begin  x = 1  end",
		},
		{
			name:     "alias global variable desugaring",
			input:    "alias $new_var $&",
			expected: `::Kernel.__oruby_alias_gvar__(:$new_var, :$&)`,
		},
		{
			name:     "alias global variable backtick",
			input:    "alias $predefined_spec_backquote $`",
			expected: "::Kernel.__oruby_alias_gvar__(:$predefined_spec_backquote, :$`)",
		},
		{
			name:     "alias global variable single quote",
			input:    "alias $predefined_spec_single_quote $'",
			expected: "::Kernel.__oruby_alias_gvar__(:$predefined_spec_single_quote, :$')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := PreprocessRuby(tt.input)
			if actual != tt.expected {
				t.Errorf("\nInput:    %s\nExpected: %s\nActual:   %s", tt.input, tt.expected, actual)
			}
		})
	}
}

func TestPreprocessRubyFlipFlop(t *testing.T) {
	input2 := "if (i == 1)..(i == 3)\n  true\nend"
	actual2 := PreprocessRuby(input2)
	if !strings.Contains(actual2, "::Kernel.__oruby_ff__(") || !strings.Contains(actual2, "(i == 1)") || !strings.Contains(actual2, "(i == 3)") {
		t.Errorf("expected __oruby_ff__ call, got: %s", actual2)
	}

	input3 := "if (i == 1)...(i == 3)\n  true\nend"
	actual3 := PreprocessRuby(input3)
	if !strings.Contains(actual3, ", true, ->{") || !strings.Contains(actual3, "(i == 1)") || !strings.Contains(actual3, "(i == 3)") {
		t.Errorf("expected exclusive __oruby_ff__ call, got: %s", actual3)
	}

	inputTernary := "(from..to ? 3 : 4)"
	actualTernary := PreprocessRuby(inputTernary)
	if !strings.Contains(actualTernary, "::Kernel.__oruby_ff__(") {
		t.Errorf("expected __oruby_ff__ for ternary, got: %s", actualTernary)
	}
}

func TestPredefinedSpecPreprocess(t *testing.T) {
	sample := `  it "is read-only when aliased" do
    alias $predefined_spec_ampersand $&
    -> {
      $predefined_spec_ampersand = ""
    }.should.raise(NameError, '$predefined_spec_ampersand is a read-only variable')
  end`
	actual := PreprocessRuby(sample)
	if strings.Contains(actual, "alias $") {
		t.Errorf("failed to preprocess global alias: %s", actual)
	}

	b, err := os.ReadFile("../oruby_test/spec/ruby/language/predefined_spec.rb")
	if err == nil {
		actualFile := PreprocessRuby(string(b))
		for _, line := range strings.Split(actualFile, "\n") {
			if strings.Contains(line, "alias $") {
				t.Errorf("line still contains alias $: %s", line)
			}
		}
	}
}

func TestPrismRewriteRuns(t *testing.T) {
	mrb, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer mrb.Close()
	for code, want := range map[string]string{
		`(1..10).select { |i| true if (i == 3)..(i == 5) }.inspect`:        "[3, 4, 5]",
		`(1..10).select { |i| true if (i == 3)...(i == 3) }.inspect`:       "[3, 4, 5, 6, 7, 8, 9, 10]",
		`c = 0; (1..6).each { |i| true if (c += 1; i == 2)..(i == 4) }; c`: "4",
		`module PM1; end; PM1::X ||= 1; PM1::X ||= 2; PM1::X`:              "1",
		`module PM2; X = 1; end; PM2::X &&= 5; PM2::X += 3; PM2::X`:        "8",
		`s = "a..b ||= x if $1"; s`:                                        "a..b ||= x if $1",
		`alias $prism_test_out $stdout; $prism_test_out.equal?($stdout)`:   "true",
		`(1` + strings.Repeat("0", 300) + ` % 7).to_s`:                     "1",
		`class PM3; def a = 1; alias :"b#{1}" :a; end; PM3.new.b1`:         "1",
	} {
		v, err := mrb.Eval(code)
		if err != nil {
			t.Errorf("%s: %v", code, err)
			continue
		}
		if got := v.String(); got != want {
			t.Errorf("%s = %q, want %q", code, got, want)
		}
	}
}

// TestPrismPrecompiledMrbSiteIDIsolation verifies that two files precompiled to
// .mrb in separate compiler processes (where per-process site counters both
// start at 0) do not collide on runtime caches (__oruby_relit, __oruby_ff__,
// __oruby_to_ary/__oruby_masgn, __oruby_end__) when loaded into the same state.
func TestPrismPrecompiledMrbSiteIDIsolation(t *testing.T) {
	compileToMrb := func(filename, src string) []byte {
		// Simulate a fresh orubyc process where all site counters start at 0
		atomic.StoreUint64(&regexpSiteCounter, 0)
		atomic.StoreUint64(&ffCounter, 0)
		atomic.StoreUint64(&endSiteCounter, 0)
		atomic.StoreUint64(&masgnSiteCounter, 0)

		mrb := MrbOpen()
		defer mrb.Close()
		c := mrb.MrbCcontextNew()
		defer c.Free()
		c.Filename(filename)
		c.SetNoExec(true)

		val, err := c.LoadBytes([]byte(src))
		if err != nil {
			t.Fatalf("compiling %s: %v", filename, err)
		}
		var buf bytes.Buffer
		if st, err := mrb.DumpIrepBinary(mrb.RProc(val).IRep(), MrbDumpDebugInfo, &buf); err != nil || st != MrbDumpOK {
			t.Fatalf("dumping %s: status=%d err=%v", filename, st, err)
		}
		return buf.Bytes()
	}

	mrbA := compileToMrb("app/models/alpha.rb", `
def alpha_vals
  [(1..3), (10...20), ((a, b = *[10, 20]))]
end
`)
	mrbB := compileToMrb("app/models/beta.rb", `
def beta_vals
  [(4..6), (30...40), ((x, y = *[30, 40]))]
end
`)
	// Also test identical range literal expression in two different file paths
	mrbSame1 := compileToMrb("app/a/same.rb", `def same_range; 1..5; end`)
	mrbSame2 := compileToMrb("app/b/same.rb", `def other_range; 1..9; end`)

	runtimeMrb, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeMrb.Close()

	for _, bytecode := range [][]byte{mrbA, mrbB, mrbSame1, mrbSame2} {
		if _, err := runtimeMrb.LoadIrep(bytecode); err != nil {
			t.Fatalf("LoadIrep failed: %v", err)
		}
	}

	res, err := runtimeMrb.Eval(`[alpha_vals.inspect, beta_vals.inspect, same_range.inspect, other_range.inspect].join(" | ")`)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	want := `[1..3, 10...20, [10, 20]] | [4..6, 30...40, [30, 40]] | 1..5 | 1..9`
	if got := res.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
