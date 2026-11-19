module Warning
  CATEGORIES = [:deprecated, :experimental, :performance, :strict_unused_block].freeze

  module Base
    def warn(msg, category: nil)
      if category
        cat_sym = category.to_sym rescue nil
        return nil if cat_sym && Warning::CATEGORIES.include?(cat_sym) && !Warning[cat_sym]
      end
      $stderr.write(msg)
      nil
    end
  end

  include Base

  def categories
    CATEGORIES
  end

  def [](category)
    raise TypeError, "no implicit conversion of #{category.class} into Symbol" unless category.is_a?(Symbol)
    raise ArgumentError, "unknown category: #{category}" unless CATEGORIES.include?(category)
    @settings ||= {}
    return @settings[category] if @settings.key?(category)
    case category
    when :deprecated
      $VERBOSE ? true : false
    when :experimental
      true
    when :performance, :strict_unused_block
      false
    end
  end

  def []=(category, val)
    raise TypeError, "no implicit conversion of #{category.class} into Symbol" unless category.is_a?(Symbol)
    raise ArgumentError, "unknown category: #{category}" unless CATEGORIES.include?(category)
    @settings ||= {}
    @settings[category] = !!val
  end

  def warn(msg, category: nil)
    super(msg, category: category)
  end

  extend self
end
