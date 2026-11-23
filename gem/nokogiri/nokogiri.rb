module Nokogiri
  VERSION_INFO = {
    "nokogiri" => { "version" => VERSION, "cppflags" => [], "ldflags" => [] },
    "ruby" => { "version" => RUBY_VERSION, "platform" => RUBY_PLATFORM, "engine" => "oruby" },
    "other_libraries" => { "go_html" => "x/net/html", "xpath" => "antchfx/xpath", "css" => "cascadia" },
  }

  class VersionInfo
    def self.instance
      @instance ||= new
    end

    def libxml2?
      false
    end

    def jruby?
      false
    end

    def libxml2_using_packaged?
      false
    end

    def libxml2_using_system?
      false
    end

    def to_hash
      VERSION_INFO
    end
  end

  def self.uses_libxml?(_requirement = nil)
    false
  end

  def self.uses_gumbo?
    true
  end

  def self.jruby?
    false
  end

  def self.XML(thing, url = nil, encoding = nil, options = XML::ParseOptions::DEFAULT_XML, &block)
    XML::Document.parse(thing, url, encoding, options, &block)
  end

  def self.HTML4(thing, url = nil, encoding = nil, options = XML::ParseOptions::DEFAULT_HTML, &block)
    HTML4::Document.parse(thing, url, encoding, options, &block)
  end

  def self.HTML(thing, url = nil, encoding = nil, options = XML::ParseOptions::DEFAULT_HTML, &block)
    HTML4(thing, url, encoding, options, &block)
  end

  def self.HTML5(thing, url = nil, encoding = nil, **options, &block)
    HTML5::Document.parse(thing, url, encoding, **options, &block)
  end

  def self.parse(string, url = nil, encoding = nil, options = nil)
    s = string.respond_to?(:read) ? string.read : string.to_s
    if s.lstrip.start_with?("<?xml")
      XML(s, url, encoding, options || XML::ParseOptions::DEFAULT_XML)
    else
      HTML(s, url, encoding, options || XML::ParseOptions::DEFAULT_HTML)
    end
  end

  def self.Slop(*args, &block)
    doc = XML(*args, &block)
    doc.slop!
    doc
  end

  module XML
    class SyntaxError
      attr_reader :line, :column, :level

      def none?
        (@level || 0) == 0
      end

      def warning?
        @level == 1
      end

      def error?
        @level == 2
      end

      def fatal?
        @level == 3
      end
    end

    class ParseOptions
      STRICT      = 0
      RECOVER     = 1 << 0
      NOENT       = 1 << 1
      DTDLOAD     = 1 << 2
      DTDATTR     = 1 << 3
      DTDVALID    = 1 << 4
      NOERROR     = 1 << 5
      NOWARNING   = 1 << 6
      PEDANTIC    = 1 << 7
      NOBLANKS    = 1 << 8
      SAX1        = 1 << 9
      XINCLUDE    = 1 << 10
      NONET       = 1 << 11
      NODICT      = 1 << 12
      NSCLEAN     = 1 << 13
      NOCDATA     = 1 << 14
      NOXINCNODE  = 1 << 15
      COMPACT     = 1 << 16
      HUGE        = 1 << 19

      DEFAULT_XML  = RECOVER | NONET
      DEFAULT_HTML = RECOVER | NOERROR | NOWARNING | NONET
      DEFAULT_XSLT = RECOVER | NOENT | DTDLOAD | DTDATTR | NOCDATA
      DEFAULT_SCHEMA = NONET

      attr_accessor :options

      def initialize(options = STRICT)
        @options = options.to_i
      end

      def to_i
        @options
      end

      def ==(other)
        @options == other.to_i
      end

      def strict
        @options &= ~RECOVER
        self
      end

      def strict?
        (@options & RECOVER) == 0
      end

      {
        recover: RECOVER,
        noent: NOENT,
        dtdload: DTDLOAD,
        dtdattr: DTDATTR,
        dtdvalid: DTDVALID,
        noerror: NOERROR,
        nowarning: NOWARNING,
        pedantic: PEDANTIC,
        noblanks: NOBLANKS,
        sax1: SAX1,
        xinclude: XINCLUDE,
        nonet: NONET,
        nodict: NODICT,
        nsclean: NSCLEAN,
        nocdata: NOCDATA,
        noxincnode: NOXINCNODE,
        compact: COMPACT,
        huge: HUGE,
      }.each do |name, bit|
        define_method(name) do
          @options |= bit
          self
        end

        define_method("no#{name}") do
          @options &= ~bit
          self
        end

        define_method("#{name}?") do
          (@options & bit) != 0
        end
      end
    end

    module Searchable
      def xpath(*paths)
        return NodeSet.new(document) if paths.empty?
        # Separate trailing ns hash / nil / vars hash from path strings
        extra = []
        while paths.length > 1 && (paths.last.is_a?(Hash) || paths.last.nil?)
          extra.unshift(paths.pop)
        end
        if paths.length == 1
          return __xpath(paths[0], *extra)
        end
        out = NodeSet.new(document)
        paths.each do |p|
          r = __xpath(p, *extra)
          if r.is_a?(NodeSet)
            out = out | r
          else
            return r
          end
        end
        out
      end

      def at_xpath(*paths)
        res = xpath(*paths)
        res.is_a?(NodeSet) ? res.first : res
      end

      def css(*rules)
        return NodeSet.new(document) if rules.empty?
        # Separate trailing namespaces hash / custom handler objects from CSS rule strings
        while !rules.empty? && !rules.last.is_a?(String)
          rules.pop
        end
        return NodeSet.new(document) if rules.empty?
        if rules.length == 1
          return __css(rules[0])
        end
        out = NodeSet.new(document)
        rules.each do |r|
          out = out | __css(r)
        end
        out
      end

      def at_css(*rules)
        css(*rules).first
      end

      def search(*paths)
        return NodeSet.new(document) if paths.empty?
        extra = []
        while paths.length > 1 && (paths.last.is_a?(Hash) || paths.last.nil?)
          extra.unshift(paths.pop)
        end
        out = NodeSet.new(document)
        paths.each do |path|
          p = path.to_s
          if p.start_with?("/", "./", "(", "..")
            r = __xpath(p, *extra)
            out = out | r if r.is_a?(NodeSet)
          else
            out = out | __css(p)
          end
        end
        out
      end
      alias / search

      def at(*paths)
        search(*paths).first
      end
      alias % at
    end

    class Node
      include Searchable
      include Enumerable

      def each
        return to_enum(:each) unless block_given?
        attribute_nodes.each { |a| yield [a.name, a.value] }
        self
      end

      def <<(node_or_tags)
        add_child(node_or_tags)
        self
      end

      def after(node_or_tags)
        add_next_sibling(node_or_tags)
        self
      end

      def before(node_or_tags)
        add_previous_sibling(node_or_tags)
        self
      end

      def swap(node_or_tags)
        replace(node_or_tags)
        self
      end

      def wrap(html_str)
        frag =
          if document && document.html?
            HTML4::DocumentFragment.parse(html_str)
          else
            XML::DocumentFragment.parse(html_str)
          end
        wrapper = frag.first_element_child || frag.children.first
        return self unless wrapper
        leaf = wrapper
        while leaf.first_element_child
          leaf = leaf.first_element_child
        end
        add_previous_sibling(wrapper)
        leaf.add_child(self)
        self
      end

      def element?
        node_type == ELEMENT_NODE
      end
      alias elem? element?

      def text?
        node_type == TEXT_NODE
      end

      def cdata?
        node_type == CDATA_SECTION_NODE
      end

      def comment?
        node_type == COMMENT_NODE
      end

      def processing_instruction?
        node_type == PI_NODE
      end

      def document?
        node_type == DOCUMENT_NODE || node_type == HTML_DOCUMENT_NODE
      end

      def fragment?
        node_type == DOCUMENT_FRAG_NODE
      end

      def html?
        document ? document.node_type == HTML_DOCUMENT_NODE : false
      end

      def xml?
        !html?
      end

      def blank?
        text? && content.strip.empty?
      end

      def ancestors(selector = nil)
        list = []
        cur = parent
        while cur
          if selector.nil? || cur.matches?(selector)
            list << cur
          end
          cur = cur.parent
        end
        NodeSet.new(document, list)
      end

      def traverse(&block)
        return to_enum(:traverse) unless block
        children.each { |c| c.traverse(&block) }
        yield self
      end

      # CSS class attribute helpers (Nokogiri 1.11+ kwattr / classes API)
      def classes
        val = self["class"]
        return [] if val.nil? || val.empty?
        val.split
      end

      def add_class(names)
        list = classes
        to_add = names.is_a?(Array) ? names : names.to_s.split
        to_add.each do |c|
          next if c.nil? || c.empty?
          list << c unless list.include?(c)
        end
        self["class"] = list.join(" ")
        self
      end

      def remove_class(names = nil)
        if names.nil?
          delete("class")
          return self
        end
        to_rem = names.is_a?(Array) ? names : names.to_s.split
        remaining = classes - to_rem
        if remaining.empty?
          delete("class")
        else
          self["class"] = remaining.join(" ")
        end
        self
      end

      def encode_special_chars(str)
        str.to_s.gsub("&", "&amp;").gsub("<", "&lt;").gsub(">", "&gt;").gsub("\r", "&#13;")
      end

      def to_xml(options = {})
        indent = options[:indent] || 0
        __serialize(false, indent, false)
      end

      def to_html(options = {})
        indent = options[:indent] || 0
        __serialize(true, indent, false)
      end
      alias to_xhtml to_xml

      def to_s
        html? ? to_html : to_xml
      end
      alias serialize to_s

      def inspect
        "#<#{self.class}:0x#{pointer_id.to_s(16)} name=#{name.inspect}>"
      end
    end

    class Document
      def self.parse(thing, url = nil, encoding = nil, options = ParseOptions::DEFAULT_XML, &block)
        opts = options.is_a?(ParseOptions) ? options : ParseOptions.new(options || ParseOptions::DEFAULT_XML)
        yield opts if block
        src = thing.respond_to?(:read) ? thing.read : thing.to_s
        __parse_xml(src, url, encoding, opts.to_i)
      end

      class << self
        alias read_memory parse
        alias read_io parse
      end

      def create_element(name, *args, &block)
        el = Node.new(name.to_s, self)
        args.each do |arg|
          if arg.is_a?(Hash)
            arg.each { |k, v| el[k.to_s] = v.to_s }
          else
            el.content = arg.to_s
          end
        end
        yield el if block
        el
      end

      def create_text_node(text, &block)
        t = Text.new(text.to_s, self)
        yield t if block
        t
      end

      def create_cdata(text, &block)
        c = CDATA.new(self, text.to_s)
        yield c if block
        c
      end

      def create_comment(text, &block)
        c = Comment.new(self, text.to_s)
        yield c if block
        c
      end

      def fragment(tags = nil)
        DocumentFragment.new(self, tags)
      end

      def to_xml(options = {})
        indent = options[:indent] || 0
        __serialize(false, indent, true)
      end

      def to_html(options = {})
        indent = options[:indent] || 0
        __serialize(true, indent, false)
      end

      def slop!
        decorators(XML::Node) << ::Nokogiri::Decorators::Slop
        decorate(self)
        self
      end
    end

    class DocumentFragment
      def self.parse(tags, encoding = nil)
        doc = Document.new
        doc.encoding = encoding if encoding
        new(doc, tags)
      end

      def to_html(options = {})
        __serialize(true, options[:indent] || 0, false)
      end

      def to_xml(options = {})
        __serialize(false, options[:indent] || 0, false)
      end

      def to_s
        html? ? to_html : to_xml
      end
    end

    class NodeSet
      def to_a
        out = []
        each { |n| out << n }
        out
      end
      alias to_ary to_a

      def first(n = nil)
        return self[0] if n.nil?
        self[0, n]
      end

      def last
        empty? ? nil : self[length - 1]
      end

      def text
        map(&:text).join
      end
      alias inner_text text

      def inner_html
        map(&:inner_html).join
      end

      def to_html
        map(&:to_html).join
      end

      def to_xml
        map(&:to_xml).join
      end

      def to_s
        map(&:to_s).join
      end

      def attr(key, value = nil)
        if value.nil? && !key.is_a?(Hash)
          return first ? first.attribute(key) : nil
        end
        each do |n|
          if key.is_a?(Hash)
            key.each { |k, v| n[k.to_s] = v.to_s }
          else
            n[key.to_s] = value.to_s
          end
        end
        self
      end
      alias set attr

      def add_class(name)
        each { |n| n.add_class(name) }
        self
      end

      def remove_class(name = nil)
        each { |n| n.remove_class(name) }
        self
      end

      def remove
        each(&:unlink)
        self
      end
      alias unlink remove

      def wrap(html_str)
        each { |n| n.wrap(html_str) }
        self
      end

      def before(datum)
        first&.before(datum)
        self
      end

      def after(datum)
        last&.after(datum)
        self
      end

      def children
        out = NodeSet.new(document)
        each { |n| out = out | n.children }
        out
      end

      def filter(expr)
        NodeSet.new(document, select { |n| n.matches?(expr) })
      end

      def xpath(*paths)
        out = NodeSet.new(document)
        each do |n|
          r = n.xpath(*paths)
          out = out | r if r.is_a?(NodeSet)
        end
        out
      end

      def at_xpath(*paths)
        xpath(*paths).first
      end

      def css(*rules)
        out = NodeSet.new(document)
        each { |n| out = out | n.css(*rules) }
        out
      end

      def at_css(*rules)
        css(*rules).first
      end

      def search(*paths)
        out = NodeSet.new(document)
        each { |n| out = out | n.search(*paths) }
        out
      end
      alias / search

      def at(*paths)
        search(*paths).first
      end
      alias % at
    end

    class Builder
      attr_accessor :doc, :parent

      def self.with(root, &block)
        new({}, root, &block)
      end

      def initialize(options = {}, root = nil, &block)
        if root
          @doc = root.document
          @parent = root
        else
          @doc = XML::Document.new
          @doc.encoding = options[:encoding] if options[:encoding]
          @parent = @doc
        end
        if block
          if block.arity == 1
            block.call(self)
          else
            instance_eval(&block)
          end
        end
      end

      def text(str)
        t = @doc.create_text_node(str.to_s)
        @parent.add_child(t)
        t
      end

      def cdata(str)
        c = @doc.create_cdata(str.to_s)
        @parent.add_child(c)
        c
      end

      def comment(str)
        c = @doc.create_comment(str.to_s)
        @parent.add_child(c)
        c
      end

      def <<(str)
        frag = @doc.fragment(str.to_s)
        @parent.add_child(frag)
        self
      end

      def to_xml(options = {})
        @doc.to_xml(options)
      end

      def method_missing(method, *args, **kw, &block)
        tag = method.to_s
        tag = tag[0..-2] if tag.end_with?("_")
        node = @doc.create_element(tag)
        args.each do |arg|
          if arg.is_a?(Hash)
            arg.each { |k, v| __apply_attr(node, k.to_s, v.to_s) }
          elsif !arg.nil?
            node.content = arg.to_s
          end
        end
        kw.each { |k, v| __apply_attr(node, k.to_s, v.to_s) }
        @parent.add_child(node)
        if block
          old_parent = @parent
          @parent = node
          begin
            if block.arity == 1
              block.call(self)
            else
              instance_eval(&block)
            end
          ensure
            @parent = old_parent
          end
        end
        node
      end

      private

      def __apply_attr(node, k, v)
        if k == "xmlns"
          node.add_namespace_definition(nil, v)
        elsif k.start_with?("xmlns:")
          node.add_namespace_definition(k[6..-1], v)
        else
          node[k] = v
        end
      end
    end

    module SAX
      class Document
        def xmldecl(version, encoding, standalone); end
        def start_document; end
        def end_document; end
        def start_element(name, attrs = []); end
        def end_element(name); end
        def characters(string); end
        def cdata_block(string); end
        def comment(string); end
        def processing_instruction(name, content); end
        def warning(string); end
        def error(string); end

        def start_element_namespace(name, attrs = [], prefix = nil, uri = nil, ns = []); end
        def end_element_namespace(name, prefix = nil, uri = nil); end
      end

      class Parser
        Attribute = Struct.new(:localname, :prefix, :uri, :value)
        attr_accessor :document

        def initialize(doc = Document.new, _encoding = "UTF-8")
          @document = doc
        end

        def parse(thing, &block)
          if thing.respond_to?(:read)
            parse_io(thing, &block)
          else
            parse_memory(thing, &block)
          end
        end

        def parse_memory(data)
          SAX.__emit(@document, data.to_s, false)
        end

        def parse_io(io)
          parse_memory(io.read)
        end

        def parse_file(filename)
          parse_memory(File.read(filename))
        end
      end

      class PushParser
        attr_accessor :document

        def initialize(doc = Document.new, _file_name = nil, _encoding = "UTF-8")
          @document = doc
          @buf = +""
        end

        def write(chunk, last_chunk = false)
          @buf << chunk.to_s if chunk
          finish if last_chunk
          self
        end
        alias << write

        def finish
          SAX.__emit(@document, @buf, false)
          @buf = +""
          self
        end
      end
    end

    class Reader
      def self.from_io(io, url = nil, encoding = nil, options = 0)
        from_memory(io.read, url, encoding, options)
      end
    end
  end

  module HTML4
    class Document
      def self.parse(thing, url = nil, encoding = nil, options = XML::ParseOptions::DEFAULT_HTML, &block)
        opts = options.is_a?(XML::ParseOptions) ? options : XML::ParseOptions.new(options || XML::ParseOptions::DEFAULT_HTML)
        yield opts if block
        src = thing.respond_to?(:read) ? thing.read : thing.to_s
        __parse_html(src, url, encoding, opts.to_i, false)
      end

      class << self
        alias read_memory parse
        alias read_io parse
      end

      def title
        t = at_css("title")
        t ? t.text : nil
      end

      def title=(text)
        t = at_css("title")
        unless t
          head = at_css("head") || root
          t = create_element("title")
          head&.add_child(t)
        end
        t.content = text.to_s
        text
      end

      def fragment(tags = nil)
        HTML4::DocumentFragment.new(self, tags)
      end

      def to_s
        to_html
      end
    end

    class DocumentFragment
      def self.parse(tags, encoding = nil)
        doc = HTML4::Document.new
        doc.encoding = encoding if encoding
        new(doc, tags)
      end
    end

    class Builder < XML::Builder
      def initialize(options = {}, root = nil, &block)
        if root
          @doc = root.document
          @parent = root
        else
          @doc = HTML4::Document.new
          @doc.encoding = options[:encoding] if options[:encoding]
          @parent = @doc
        end
        if block
          if block.arity == 1
            block.call(self)
          else
            instance_eval(&block)
          end
        end
      end

      def to_html(options = {})
        @doc.to_html(options)
      end
    end

    module SAX
      class Parser < XML::SAX::Parser
        def parse_memory(data)
          XML::SAX.__emit(@document, data.to_s, true)
        end
      end

      class PushParser < XML::SAX::PushParser
        def finish
          XML::SAX.__emit(@document, @buf, true)
          @buf = +""
          self
        end
      end
    end
  end

  module HTML5
    class Document
      def self.parse(thing, url = nil, encoding = nil, **options, &block)
        src = thing.respond_to?(:read) ? thing.read : thing.to_s
        __parse_html(src, url, encoding, XML::ParseOptions::DEFAULT_HTML, true)
      end

      def self.fragment(tags, encoding = nil)
        DocumentFragment.parse(tags, encoding)
      end
    end

    class DocumentFragment
      def self.parse(tags, encoding = nil)
        doc = HTML5::Document.new
        doc.encoding = encoding if encoding
        new(doc, tags)
      end
    end

    def self.parse(thing, url = nil, encoding = nil, **options, &block)
      Document.parse(thing, url, encoding, **options, &block)
    end

    def self.fragment(tags, encoding = nil)
      DocumentFragment.parse(tags, encoding)
    end
  end

  module CSS
    def self.xpath_for(*rules)
      opts = rules.last.is_a?(Hash) ? rules.pop : {}
      prefix = opts[:prefix] || "//"
      rules.map { |r| __xpath_for(r.to_s, prefix) }
    end
  end

  module Decorators
    module Slop
      def method_missing(name, *args, &block)
        list = xpath("./#{name}")
        return super if list.empty?
        return list.first if list.length == 1
        list
      end
    end
  end
end
