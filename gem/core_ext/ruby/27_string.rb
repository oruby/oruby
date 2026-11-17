# String additions and MRI behaviour mruby's String lacks

class String
  # -"str": the deduplicated frozen string, one object per content and
  # encoding (frozen string literals are compiled to this)
  def -@
    t = ::Kernel.instance_variable_get(:@__oruby_fstr) || ::Kernel.instance_variable_set(:@__oruby_fstr, {})
    enc = encoding
    by_enc = (t[enc] ||= {})
    by_enc[self] ||= frozen? && instance_of?(String) ? self : String.new(self).freeze
  end
  alias dedup -@
end
