# Helpers for specs
$VERBOSE = false if $VERBOSE.nil?

module Enumerable
  alias __orig_all? all?
  def all?(*args, &block)
    warn "warning: given block not used", uplevel: 1 if !args.empty? && block
    __orig_all?(*args, &block)
  end

  alias __orig_any? any?
  def any?(*args, &block)
    warn "warning: given block not used", uplevel: 1 if !args.empty? && block
    __orig_any?(*args, &block)
  end

  alias __orig_none? none?
  def none?(*args, &block)
    warn "warning: given block not used", uplevel: 1 if !args.empty? && block
    __orig_none?(*args, &block)
  end

  alias __orig_one? one?
  def one?(*args, &block)
    warn "warning: given block not used", uplevel: 1 if !args.empty? && block
    __orig_one?(*args, &block)
  end

  alias __orig_find find
  def find(*args, &block)
    if block_given?
      __orig_find(*args, &block)
    else
      to_enum(:find, *args) { nil }
    end
  end
  alias detect find
end
