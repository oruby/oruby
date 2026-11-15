class CGI
  module Escape
    def escape(string)
      enc = string.encoding
      CGI.__escape(string, true).force_encoding(enc)
    end

    def unescape(string, encoding = Encoding::UTF_8)
      str = CGI.__unescape(string, true).force_encoding(encoding)
      str.valid_encoding? ? str : str.force_encoding(string.encoding)
    end

    def escapeURIComponent(string)
      enc = string.encoding
      CGI.__escape(string, false).force_encoding(enc)
    end
    alias escape_uri_component escapeURIComponent

    def unescapeURIComponent(string, encoding = Encoding::UTF_8)
      str = CGI.__unescape(string, false).force_encoding(encoding)
      str.valid_encoding? ? str : str.force_encoding(string.encoding)
    end
    alias unescape_uri_component unescapeURIComponent

    def escapeHTML(string)
      enc = string.encoding
      CGI.__escape_html(string).force_encoding(enc)
    end

    def unescapeHTML(string)
      enc = string.encoding
      CGI.__unescape_html(string).force_encoding(enc)
    end

    alias escape_html escapeHTML
    alias h escapeHTML
    alias unescape_html unescapeHTML

    def escapeElement(string, *elements)
      elements = elements[0] if elements[0].is_a?(Array)
      return string if elements.empty?
      names = elements.map { |e| Regexp.escape(e.to_s) }.join("|")
      string.gsub(/<\/?(?:#{names})(?:\s[^>]*)?>/i) { |m| escapeHTML(m) }
    end
    alias escape_element escapeElement

    def unescapeElement(string, *elements)
      elements = elements[0] if elements[0].is_a?(Array)
      return string if elements.empty?
      names = elements.map { |e| Regexp.escape(e.to_s) }.join("|")
      string.gsub(/&lt;\/?(?:#{names})(?:\s.*?)?&gt;/i) { |m| unescapeHTML(m) }
    end
    alias unescape_element unescapeElement
  end

  @@accept_charset = Encoding::UTF_8

  include Escape
  extend Escape

  module Util
    include Escape
  end

  RFC822_DAYS = %w[Sun Mon Tue Wed Thu Fri Sat]
  RFC822_MONTHS = %w[Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec]

  def self.rfc1123_date(time)
    t = time.clone.gmtime
    format("%s, %.2d %s %.4d %.2d:%.2d:%.2d GMT",
           RFC822_DAYS[t.wday], t.day, RFC822_MONTHS[t.month - 1], t.year, t.hour, t.min, t.sec)
  end

  def self.pretty(string, shift = "  ")
    lines = string.gsub(/(?!\A)<.*?>/m, "\n\\0").gsub(/<.*?>(?!\n)/m, "\\0\n")
    end_pos = 0
    while (end_pos = lines.index(/^<\/(\w+)/, end_pos))
      element = $1.dup
      start_pos = lines.rindex(/^\s*<#{element}/i, end_pos)
      lines[start_pos..end_pos] = "__" + lines[start_pos..end_pos].gsub(/\n(?!\z)/, "\n" + shift) + "__"
    end
    lines.gsub(/^((?:#{Regexp.quote(shift)})*)__(?=<\/?\w)/, '\1')
  end
end
