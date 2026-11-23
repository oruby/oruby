ERB.const_set(:BINDING_FACTORY, proc { binding })

class ERB
  def result_with_hash(variables)
    raise TypeError, "expected a Hash" unless variables.is_a?(Hash)

    scope = BINDING_FACTORY.call
    variables.each { |name, value| scope.local_variable_set(name, value) }
    result(scope)
  end

  module Util
    include Escape

    def html_escape(s)
      Escape.html_escape(s)
    end
    alias h html_escape
    module_function :html_escape, :h

    def html_escape_once(s)
      s.to_s.gsub(/["><']|&(?!([a-zA-Z]+|(#\d+)|(#[xX][\da-fA-F]+));)/) do |ch|
        Escape.html_escape(ch)
      end
    end
    module_function :html_escape_once

    def url_encode(s)
      Escape.__url_encode(s)
    end
    alias u url_encode
    module_function :url_encode, :u
  end
end