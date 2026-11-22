package racc_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/racc"
	_ "github.com/oruby/oruby/gem/regexp"
)

func TestRaccCparseRealGrammar(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "racc/parser"

		raise "expected c runtime" unless Racc::Parser.racc_runtime_type == "c"
		raise "missing version" unless Racc::Parser::Racc_Runtime_Core_Version_C == "1.8.1"

		# Real Racc 1.8.1 generated LALR(1) tables for CalcParser
		class CalcParser < Racc::Parser
			racc_action_table = [
				 9,    10,     7,     8,     6,    18,     4,     3,    12,     5,
				 9,    10,     7,     8,     4,     3,    13,     5,     4,     3,
			   nil,     5,     4,     3,   nil,     5,     4,     3,   nil,     5,
				 4,     3,   nil,     5,     9,    10,     9,    10 ]

			racc_action_check = [
				11,    11,    11,    11,     1,    11,     0,     0,     4,     0,
				 2,     2,     2,     2,     3,     3,     6,     3,     7,     7,
			   nil,     7,     8,     8,   nil,     8,     9,     9,   nil,     9,
				10,    10,   nil,    10,    14,    14,    15,    15 ]

			racc_action_pointer = [
				 0,     4,     7,     8,    -1,   nil,    16,    12,    16,    20,
				24,    -3,   nil,   nil,    31,    33,   nil,   nil,   nil ]

			racc_action_default = [
				-2,   -10,    -1,   -10,   -10,    -9,   -10,   -10,   -10,   -10,
			   -10,   -10,    -8,    19,    -3,    -4,    -5,    -6,    -7 ]

			racc_goto_table = [
				 2,     1,   nil,    11,   nil,   nil,   nil,    14,    15,    16,
				17 ]

			racc_goto_check = [
				 2,     1,   nil,     2,   nil,   nil,   nil,     2,     2,     2,
				 2 ]

			racc_goto_pointer = [ nil, 1, 0 ]
			racc_goto_default = [ nil, nil, nil ]

			racc_reduce_table = [
			  0, 0, :racc_error,
			  1, 11, :_reduce_none,
			  0, 11, :_reduce_2,
			  3, 12, :_reduce_3,
			  3, 12, :_reduce_4,
			  3, 12, :_reduce_5,
			  3, 12, :_reduce_6,
			  3, 12, :_reduce_7,
			  2, 12, :_reduce_8,
			  1, 12, :_reduce_none ]

			racc_reduce_n = 10
			racc_shift_n = 19

			racc_token_table = {
			  false => 0,
			  :error => 1,
			  :UMINUS => 2,
			  "*" => 3,
			  "/" => 4,
			  "+" => 5,
			  "-" => 6,
			  "(" => 7,
			  ")" => 8,
			  :NUMBER => 9 }

			racc_nt_base = 10
			racc_use_result_var = true

			Racc_arg = [
			  racc_action_table, racc_action_check, racc_action_default, racc_action_pointer,
			  racc_goto_table, racc_goto_check, racc_goto_default, racc_goto_pointer,
			  racc_nt_base, racc_reduce_table, racc_token_table,
			  racc_shift_n, racc_reduce_n, racc_use_result_var ]

			Racc_token_to_s_table = [
			  "$end", "error", "UMINUS", '"*"', '"/"', '"+"', '"-"', '"("', '")"',
			  "NUMBER", "$start", "target", "exp" ]

			Racc_debug_parser = false

			def _reduce_2(val, _values, result); 0; end
			def _reduce_3(val, _values, result); result + val[2]; end
			def _reduce_4(val, _values, result); result - val[2]; end
			def _reduce_5(val, _values, result); result * val[2]; end
			def _reduce_6(val, _values, result); result / val[2]; end
			def _reduce_7(val, _values, result); val[1]; end
			def _reduce_8(val, _values, result); -val[1]; end
			def _reduce_none(val, _values, result); val[0]; end

			def tokenize(str)
				tokens = []
				i = 0
				while i < str.length
					ch = str[i]
					if ch == " " || ch == "\t"
						i += 1
					elsif ch >= "0" && ch <= "9"
						j = i
						j += 1 while j < str.length && str[j] >= "0" && str[j] <= "9"
						tokens << [:NUMBER, str[i...j].to_i]
						i = j
					else
						tokens << [ch, ch]
						i += 1
					end
				end
				tokens << [false, false]
				tokens
			end

			def parse_do(str)
				@q = tokenize(str)
				do_parse
			end

			def next_token
				@q.shift
			end

			def parse_yy(str)
				toks = tokenize(str)
				yyparse(toks, :each)
			end
		end

		calc = CalcParser.new

		cases = [
			["", 0],
			["42", 42],
			["1 + 2 * 3", 7],
			["(1 + 2) * 3", 9],
			["100 - 20 / 4 + 3 * (2 + 5)", 116],
			["-5 + 12", 7],
			["(10 + 20) * (30 - 15) / 5", 90],
		]

		cases.each do |expr, expected|
			r1 = calc.parse_do(expr)
			raise "do_parse(#{expr.inspect}) = #{r1}, want #{expected}" unless r1 == expected
			r2 = calc.parse_yy(expr)
			raise "yyparse(#{expr.inspect}) = #{r2}, want #{expected}" unless r2 == expected
		end

		# Syntax error raises Racc::ParseError
		raised = false
		begin
			calc.parse_do("1 + * 2")
		rescue Racc::ParseError
			raised = true
		end
		raise "expected Racc::ParseError" unless raised
	`)
	if err != nil {
		t.Fatalf("racc/cparse test failed: %v", err)
	}
}
