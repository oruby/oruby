module Racc
  class CparseParams
    def self.__call_action(parser, mid, use_result_var, tmp_v, vstack, tmp)
      catch(:racc_jump) do
        res =
          if use_result_var
            parser.__send__(mid, tmp_v, vstack, tmp)
          else
            parser.__send__(mid, tmp_v, vstack)
          end
        [0, res]
      end
    end
  end

  class Parser
    private

    def _racc_do_parse_c(arg, sysdebug)
      params = ::Racc::CparseParams.__new(self, arg, sysdebug, false, nil)
      params.__parse_all
    end

    def _racc_yyparse_c(lexer, lexmid, arg, sysdebug)
      params = ::Racc::CparseParams.__new(self, arg, sysdebug, true, lexmid)
      params.__parse_init
      lexer.__send__(lexmid) do |tok_val|
        params.__feed_token(tok_val)
        break if params.__should_break?
      end
      unless params.__finished?
        raise ArgumentError, "#{lexmid}() is finished before EndOfToken"
      end
      params.__retval
    end
  end
end
