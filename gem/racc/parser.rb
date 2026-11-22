module Racc
  class ParseError < StandardError; end unless const_defined?(:ParseError)

  class Parser
    Racc_Runtime_Version = ::Racc::VERSION
    Racc_Runtime_Core_Version_R = ::Racc::VERSION
    Racc_Main_Parsing_Routine = :_racc_do_parse_c
    Racc_YY_Parse_Method = :_racc_yyparse_c
    Racc_Runtime_Core_Version = Racc_Runtime_Core_Version_C
    Racc_Runtime_Type = "c"

    def self.racc_runtime_type
      Racc_Runtime_Type
    end

    def _racc_setup
      @yydebug = false unless self.class.const_defined?(:Racc_debug_parser) && self.class::Racc_debug_parser
      @yydebug = false unless defined?(@yydebug)
      if @yydebug
        @racc_debug_out ||= $stderr
      end
      arg = self.class::Racc_arg
      arg[13] = true if arg.size < 14
      arg
    end

    def do_parse
      _racc_do_parse_c(_racc_setup, false)
    end

    def yyparse(recv, mid)
      _racc_yyparse_c(recv, mid, _racc_setup, false)
    end

    def next_token
      raise NotImplementedError, "#{self.class}#next_token is not defined"
    end

    def on_error(t, val, vstack)
      raise ParseError, sprintf("\nparse error on value %s (%s)", val.inspect, token_to_str(t) || "?")
    end

    def yyerror
      throw :racc_jump, 1
    end

    def yyaccept
      throw :racc_jump, 2
    end

    def yyerrok
      @racc_error_status = 0
    end

    def token_to_str(t)
      self.class::Racc_token_to_s_table[t]
    end

    def racc_token2str(tok)
      self.class::Racc_token_to_s_table[tok] or
        raise "[Racc Bug] can't convert token #{tok} to string"
    end

    def racc_read_token(t, tok, val)
      @racc_debug_out.print "read    ", tok.inspect, "(", racc_token2str(t), ") ", val.inspect, "\n\n"
    end

    def racc_shift(tok, tstack, vstack)
      @racc_debug_out.puts "shift   #{racc_token2str(tok)}\n\n"
    end

    def racc_reduce(toks, sim, tstack, vstack)
      @racc_debug_out.puts "reduce  --> #{racc_token2str(sim)}\n\n"
    end

    def racc_accept
      @racc_debug_out.puts "accept\n\n"
    end

    def racc_e_pop(state, tstack, vstack)
      @racc_debug_out.puts "error recovering mode: pop token\n\n"
    end

    def racc_next_state(curstate, state)
      @racc_debug_out.puts "goto    #{curstate}\n\n"
    end
  end
end

unless Object.const_defined?(:ParseError)
  ParseError = Racc::ParseError
end
