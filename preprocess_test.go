package oruby

import (
	"os"
	"strings"
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
			expected: `__oruby_alias__(:"#{'a'}", :value)`,
		},
		{
			name:     "dynamic alias lhs expression",
			input:    `alias :"#{'a' + ''.to_s}" value`,
			expected: `__oruby_alias__(:"#{'a' + ''.to_s}", :value)`,
		},
		{
			name:     "dynamic alias rhs",
			input:    `alias a :"#{'value'}"`,
			expected: `__oruby_alias__(:a, :"#{'value'}")`,
		},
		{
			name:     "dynamic alias rhs expression",
			input:    `alias a :"#{'value' + ''.to_s}"`,
			expected: `__oruby_alias__(:a, :"#{'value' + ''.to_s}")`,
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
			expected: `__oruby_undef__(:"#{'meth'}")`,
		},
		{
			name:     "dynamic undef expression",
			input:    `undef :"#{'meth'.to_sym}"`,
			expected: `__oruby_undef__(:"#{'meth'.to_sym}")`,
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
			name:     "method def with **nil",
			input:    `def no_kw(arg1, **nil, &); inner(&) end`,
			expected: `def no_kw(arg1, **__no_kw_disallow__, &); inner(&) end`,
		},
		{
			name:     "proc with **nil",
			input:    `proc { |**nil| :ok }`,
			expected: `proc { |**__no_kw_disallow__| :ok }`,
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
			input:    `rescue => a&.b`,
			expected: `rescue => __rescued_exc__; a&.b = __rescued_exc__`,
		},
		{
			name:     "for safe navigation target",
			input:    "for o&.target in [1, 2]\n  o.target\nend",
			expected: "for __for_elem__ in [1, 2]; o&.target = __for_elem__;\n  o.target\nend",
		},
		{
			name:     "begin block desugaring",
			input:    "BEGIN { x = 1 }",
			expected: "begin; x = 1 end",
		},
		{
			name:     "alias global variable desugaring",
			input:    "alias $new_var $&",
			expected: `__oruby_alias_gvar__(:$new_var, :$&)`,
		},
		{
			name:     "alias global variable backtick",
			input:    "alias $predefined_spec_backquote $`",
			expected: "__oruby_alias_gvar__(:$predefined_spec_backquote, :$`)",
		},
		{
			name:     "alias global variable single quote",
			input:    "alias $predefined_spec_single_quote $'",
			expected: "__oruby_alias_gvar__(:$predefined_spec_single_quote, :$')",
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
	if !strings.Contains(actual2, "__oruby_ff2__") || !strings.Contains(actual2, "(i == 1)") || !strings.Contains(actual2, "(i == 3)") {
		t.Errorf("expected __oruby_ff2__ call, got: %s", actual2)
	}

	input3 := "if (i == 1)...(i == 3)\n  true\nend"
	actual3 := PreprocessRuby(input3)
	if !strings.Contains(actual3, "__oruby_ff3__") || !strings.Contains(actual3, "(i == 1)") || !strings.Contains(actual3, "(i == 3)") {
		t.Errorf("expected __oruby_ff3__ call, got: %s", actual3)
	}

	inputTernary := "(from..to ? 3 : 4)"
	actualTernary := PreprocessRuby(inputTernary)
	if !strings.Contains(actualTernary, "__oruby_ff2__") {
		t.Errorf("expected __oruby_ff2__ for ternary, got: %s", actualTernary)
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
