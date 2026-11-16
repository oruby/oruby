package oruby

import (
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
