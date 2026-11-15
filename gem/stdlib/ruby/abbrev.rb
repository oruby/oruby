module Abbrev
  def abbrev(words, pattern = nil)
    table = {}
    seen = Hash.new(0)
    words = words.uniq
    if pattern.is_a?(String)
      pattern = /\A#{Regexp.quote(pattern)}/
    end
    words.each do |word|
      next if word.empty?
      word.size.downto(1) do |len|
        abbrev = word[0...len]
        next if pattern && pattern !~ abbrev
        case seen[abbrev] += 1
        when 1
          table[abbrev] = word
        when 2
          table.delete(abbrev)
        else
          break
        end
      end
    end
    words.each do |word|
      next if pattern && pattern !~ word
      table[word] = word
    end
    table
  end
  module_function :abbrev
end

class Array
  def abbrev(pattern = nil)
    Abbrev.abbrev(self, pattern)
  end
end
