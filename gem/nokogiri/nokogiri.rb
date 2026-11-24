module Nokogiri
  module ClassResolver
    VALID_NAMESPACES = ["HTML", "HTML4", "HTML5", "XML", "SAX"].freeze

    def related_class(class_name)
      klass = nil
      inspecting = self.class

      while inspecting
        if inspecting.name
          namespace_path = inspecting.name.split("::")[0..-2]
          if VALID_NAMESPACES.include?(namespace_path.last)
            related_class_name = (namespace_path + [class_name.to_s]).join("::")
            klass =
              begin
                Object.const_get(related_class_name)
              rescue NameError
                nil
              end
            break if klass
          end
        end
        inspecting = inspecting.superclass
      end
      klass
    end
  end

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

  def self.make(input = nil, opts = {}, &blk)
    if input
      HTML4::DocumentFragment.parse(input.to_s)
    elsif blk
      frag = HTML4::DocumentFragment.parse("")
      HTML4::Builder.with(frag, &blk)
      frag.children.length == 1 ? frag.children.first : frag
    else
      HTML4::DocumentFragment.parse("")
    end
  end

  def self.Slop(*args, &block)
    doc = XML(*args, &block)
    doc.slop!
    doc
  end

  def self.XSLT(stylesheet, modules = {})
    XSLT.parse(stylesheet, modules)
  end

  module XML
    def self.parse(thing, url = nil, encoding = nil, options = ParseOptions::DEFAULT_XML, &block)
      Document.parse(thing, url, encoding, options, &block)
    end

    def self.fragment(string, encoding = nil, &block)
      frag = DocumentFragment.parse(string, encoding)
      yield frag if block
      frag
    end

    class XPathContext
      def initialize(node)
        @node = node
        @namespaces = {}
        @variables = {}
      end

      def node=(node)
        @node = node
      end

      def register_ns(prefix, uri)
        @namespaces[prefix.to_s] = uri.to_s
        self
      end

      def register_namespaces(namespaces)
        namespaces.each { |k, v| register_ns(k, v) }
        self
      end

      def register_variable(name, value)
        @variables[name.to_s] = value
        self
      end

      def register_variables(variables)
        variables.each { |k, v| register_variable(k, v) }
        self
      end

      def evaluate(search_path, handler = nil)
        @node.xpath(search_path, @namespaces.empty? ? nil : @namespaces, handler, @variables.empty? ? nil : @variables)
      end
    end

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

      def default_xml
        @options |= DEFAULT_XML
        self
      end

      def default_html
        @options |= DEFAULT_HTML
        self
      end

      def default_xslt
        @options |= DEFAULT_XSLT
        self
      end

      def default_schema
        @options |= DEFAULT_SCHEMA
        self
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
        # Separate trailing ns hash / nil / custom handler / binds hash from path strings
        extra = []
        while paths.length > 1 && !paths.last.is_a?(String)
          extra.unshift(paths.pop)
        end
        ns, handler, binds = __split_xpath_extras(extra)
        if paths.length == 1
          return __xpath(paths[0], ns, handler, binds)
        end
        out = NodeSet.new(document)
        paths.each do |p|
          r = __xpath(p, ns, handler, binds)
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
        while paths.length > 1 && !paths.last.is_a?(String)
          extra.unshift(paths.pop)
        end
        ns, handler, binds = __split_xpath_extras(extra)
        out = NodeSet.new(document)
        paths.each do |path|
          p = path.to_s
          if p.start_with?("/", "./", "(", "..")
            r = __xpath(p, ns, handler, binds)
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

      def >(selector)
        sel = selector.to_s
        if sel.start_with?("/", "./", "(", "..")
          xpath(sel)
        else
          NodeSet.new(document, element_children.select { |c| c.matches?(sel) })
        end
      end

      private

      def __split_xpath_extras(extra)
        ns = nil
        handler = nil
        binds = nil
        seen_ns_slot = false
        extra.each do |arg|
          if arg.nil?
            seen_ns_slot = true
          elsif arg.is_a?(Hash)
            if !seen_ns_slot && handler.nil?
              ns = arg
              seen_ns_slot = true
            else
              binds = arg
            end
          else
            handler = arg
            seen_ns_slot = true
          end
        end
        [ns, handler, binds]
      end
    end

    class Node
      include ClassResolver
      include Searchable
      include Enumerable

      class SaveOptions
        FORMAT         = 1 << 0
        NO_DECLARATION = 1 << 1
        NO_EMPTY_TAGS  = 1 << 2
        NO_XHTML       = 1 << 3
        XHTML          = 1 << 4
        AS_XML         = 1 << 5
        AS_HTML        = 1 << 6
        AS_XHTML       = 1 << 7
        AS_BUILDER     = 1 << 8

        DEFAULT_XML   = FORMAT | AS_XML
        DEFAULT_HTML  = FORMAT | NO_DECLARATION | NO_EMPTY_TAGS | AS_HTML
        DEFAULT_XHTML = FORMAT | NO_DECLARATION | NO_EMPTY_TAGS | AS_XHTML

        attr_accessor :options

        def initialize(options = 0)
          @options = options.to_i
        end

        def to_i
          @options
        end

        def ==(other)
          @options == other.to_i
        end

        {
          format: FORMAT,
          no_declaration: NO_DECLARATION,
          no_empty_tags: NO_EMPTY_TAGS,
          no_xhtml: NO_XHTML,
          xhtml: XHTML,
          as_xml: AS_XML,
          as_html: AS_HTML,
          as_xhtml: AS_XHTML,
          as_builder: AS_BUILDER,
        }.each do |name, bit|
          define_method(name) do
            @options |= bit
            self
          end

          define_method("#{name}?") do
            (@options & bit) != 0
          end
        end
      end

      def each
        return to_enum(:each) unless block_given?
        attribute_nodes.each { |a| yield [a.name, a.value] }
        self
      end

      def child
        children.first
      end

      def children=(node_or_tags)
        children.each(&:unlink)
        add_child(node_or_tags)
      end

      def next=(node_or_tags)
        add_next_sibling(node_or_tags)
      end

      def previous=(node_or_tags)
        add_previous_sibling(node_or_tags)
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

      # CSS class & keyword-attribute helpers (Nokogiri 1.11+ kwattr / classes API)
      def kwattr_values(attribute_name)
        val = self[attribute_name]
        return [] if val.nil? || val.empty?
        val.split
      end

      def kwattr_add(attribute_name, keywords)
        list = kwattr_values(attribute_name)
        to_add = keywords.is_a?(Array) ? keywords : keywords.to_s.split
        to_add.each do |c|
          next if c.nil? || c.empty?
          list << c unless list.include?(c)
        end
        self[attribute_name] = list.join(" ")
        self
      end

      def kwattr_append(attribute_name, keywords)
        list = kwattr_values(attribute_name)
        to_add = keywords.is_a?(Array) ? keywords : keywords.to_s.split
        to_add.each do |c|
          next if c.nil? || c.empty?
          list << c
        end
        self[attribute_name] = list.join(" ")
        self
      end

      def kwattr_remove(attribute_name, keywords = nil)
        if keywords.nil?
          delete(attribute_name)
          return self
        end
        to_rem = keywords.is_a?(Array) ? keywords : keywords.to_s.split
        remaining = kwattr_values(attribute_name) - to_rem
        if remaining.empty?
          delete(attribute_name)
        else
          self[attribute_name] = remaining.join(" ")
        end
        self
      end

      def classes
        kwattr_values("class")
      end

      def add_class(names)
        kwattr_add("class", names)
      end

      def append_class(names)
        kwattr_append("class", names)
      end

      def remove_class(names = nil)
        kwattr_remove("class", names)
      end

      def value?(val)
        values.include?(val.to_s)
      end

      def lang
        cur = self
        while cur
          v = cur["xml:lang"] || cur["lang"]
          return v if v
          cur = cur.parent
        end
        nil
      end

      def lang=(value)
        self["xml:lang"] = value
      end

      def read_only?
        false
      end

      def data_ptr?
        true
      end

      def description
        html? ? HTML4::ElementDescription[name] : nil
      end

      def decorate!
        document&.decorate(self)
        self
      end

      def accept(visitor)
        visitor.visit(self)
      end

      def fragment(tags = nil)
        return document.fragment(tags) if document
        XML::DocumentFragment.parse(tags.to_s)
      end

      def parse(string_or_io, _options = nil)
        src = string_or_io.respond_to?(:read) ? string_or_io.read : string_or_io.to_s
        fragment(src).children
      end

      def coerce(data)
        case data
        when XML::NodeSet
          return data
        when XML::DocumentFragment
          return data.children
        when String
          return fragment(data).children
        when Document, XML::Attr
          # unacceptable
        when XML::Node
          return data
        end
        raise ArgumentError, "Requires a Node, NodeSet or String argument, and cannot accept a #{data.class}."
      end

      def safe_process_xinclude(options = nil)
        do_xinclude(options)
      end

      def to_str
        content
      end

      def deconstruct_keys(_keys)
        {
          name: name,
          attributes: attribute_nodes,
          children: children,
          content: content,
          namespace: namespace,
        }
      end

      def pretty_print(pp)
        pp.text(inspect)
      end

      def encode_special_chars(str)
        str.to_s.gsub("&", "&amp;").gsub("<", "&lt;").gsub(">", "&gt;").gsub("\r", "&#13;")
      end

      def __resolve_save_options(options, as_html_default, xml_decl_default)
        opts = options || {}
        as_html = as_html_default
        xml_decl = xml_decl_default
        no_empty = false
        indent = opts[:indent]
        if opts.key?(:save_with)
          sw = opts[:save_with].to_i
          as_html = true if (sw & SaveOptions::AS_HTML) != 0
          as_html = false if (sw & SaveOptions::AS_XML) != 0 || (sw & SaveOptions::AS_XHTML) != 0
          xml_decl = false if (sw & SaveOptions::NO_DECLARATION) != 0
          no_empty = true if (sw & SaveOptions::NO_EMPTY_TAGS) != 0
          indent = 2 if indent.nil? && (sw & SaveOptions::FORMAT) != 0
        end
        [as_html, indent || 0, xml_decl, no_empty]
      end

      def to_xml(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, false, false)
        __serialize(as_html, indent, xml_decl, no_empty)
      end

      def to_html(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, true, false)
        __serialize(as_html, indent, xml_decl, no_empty)
      end

      def to_xhtml(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, false, false)
        __serialize(as_html, indent, xml_decl, no_empty || !options.key?(:save_with))
      end

      def write_to(io, *options)
        opts = options.last.is_a?(Hash) ? options.last : {}
        io << (html? ? to_html(opts) : to_xml(opts))
      end

      def write_xml_to(io, options = {})
        io << to_xml(options)
      end

      def write_html_to(io, options = {})
        io << to_html(options)
      end

      def write_xhtml_to(io, options = {})
        io << to_xhtml(options)
      end

      def serialize(*args, &block)
        opts = args.last.is_a?(Hash) ? args.last : {}
        if args.first.is_a?(String) && !opts.key?(:encoding)
          opts = opts.merge(encoding: args.first)
        end
        out = html? ? to_html(opts) : to_xml(opts)
        yield out if block
        out
      end

      def to_s(*args)
        args.empty? ? (html? ? to_html : to_xml) : serialize(*args)
      end

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
        (related_class("DocumentFragment") || DocumentFragment).new(self, tags)
      end

      def to_xml(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, false, true)
        __serialize(as_html, indent, xml_decl, no_empty)
      end

      def to_html(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, true, false)
        __serialize(as_html, indent, xml_decl, no_empty)
      end

      def to_xhtml(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, false, false)
        __serialize(as_html, indent, xml_decl, no_empty || !options.key?(:save_with))
      end

      def validate
        dtd = internal_subset
        return nil unless dtd
        dtd.validate(self)
      end

      def namespace_inheritance
        @namespace_inheritance.nil? ? true : @namespace_inheritance
      end

      def namespace_inheritance=(val)
        @namespace_inheritance = !!val
      end

      def xpath_doctype
        :xml
      end

      def slop!
        decorators(XML::Node) << ::Nokogiri::Decorators::Slop
        decorate(self)
        self
      end
    end

    class Namespace
      def deconstruct_keys(_keys)
        { prefix: prefix, href: href }
      end

      def pretty_print(pp)
        pp.text(inspect)
      end
    end

    class DocumentFragment
      def self.parse(tags, encoding = nil)
        doc = Document.new
        doc.encoding = encoding if encoding
        new(doc, tags)
      end

      def deconstruct
        children.to_a
      end

      def parse_options
        @parse_options ||= ParseOptions.new(ParseOptions::DEFAULT_XML)
      end

      def to_html(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, true, false)
        __serialize(as_html, indent, xml_decl, no_empty)
      end

      def to_xml(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, false, false)
        __serialize(as_html, indent, xml_decl, no_empty)
      end

      def to_xhtml(options = {})
        as_html, indent, xml_decl, no_empty = __resolve_save_options(options, false, false)
        __serialize(as_html, indent, xml_decl, no_empty || !options.key?(:save_with))
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

      def to_html(*args)
        map { |n| n.to_html(*args) }.join
      end

      def to_xml(*args)
        map { |n| n.to_xml(*args) }.join
      end

      def to_xhtml(*args)
        map { |n| n.to_xhtml(*args) }.join
      end

      def to_s
        map(&:to_s).join
      end

      def deconstruct
        to_a
      end

      def inspect
        "[#{map(&:inspect).join(', ')}]"
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
      alias attribute attr

      def remove_attr(name)
        each { |n| n.delete(name) }
        self
      end
      alias remove_attribute remove_attr

      def add_class(name)
        each { |n| n.add_class(name) }
        self
      end

      def append_class(name)
        each { |n| n.append_class(name) }
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

      def >(selector)
        out = NodeSet.new(document)
        each { |n| out = out | (n > selector) }
        out
      end
    end

    class Builder
      include ClassResolver
      DEFAULT_DOCUMENT_OPTIONS = { encoding: "UTF-8" }.freeze
      attr_accessor :doc, :parent, :context, :arity

      def self.with(root, &block)
        new({}, root, &block)
      end

      def initialize(options = {}, root = nil, &block)
        @ns = nil
        @context = nil
        @arity = block ? block.arity : 0
        if root
          @doc = root.document
          @parent = root
        else
          doc_cls = related_class("Document") || XML::Document
          @doc = doc_cls.new
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

      def [](ns)
        @ns = ns.to_s
        self
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
        ns_prefix = @ns
        @ns = nil
        tag = method.to_s
        tag = tag[0..-2] if tag.end_with?("_")
        qname = ns_prefix && !ns_prefix.empty? ? "#{ns_prefix}:#{tag}" : tag
        node = @doc.create_element(qname)
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
        include ClassResolver
        Attribute = Struct.new(:localname, :prefix, :uri, :value)
        attr_accessor :document, :encoding

        def initialize(doc = Document.new, encoding = "UTF-8")
          @document = doc
          @encoding = encoding
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
        attr_accessor :document, :options

        def initialize(doc = Document.new, _file_name = nil, _encoding = "UTF-8")
          @document = doc
          @options = ParseOptions::DEFAULT_XML
          @replace_entities = false
          @state = SAX::PushState.new
        end

        def replace_entities
          @replace_entities
        end

        def replace_entities=(val)
          @replace_entities = !!val
        end

        def write(chunk, last_chunk = false)
          @state.write(@document, chunk.to_s) if chunk && !chunk.empty?
          finish if last_chunk
          self
        end
        alias << write

        def finish
          @state.finish(@document)
          @state = SAX::PushState.new
          self
        end
      end
    end

    def self.Reader(string_or_io, url = nil, encoding = nil, options = 0)
      if string_or_io.respond_to?(:read)
        Reader.from_io(string_or_io, url, encoding, options)
      else
        Reader.from_memory(string_or_io.to_s, url, encoding, options)
      end
    end

    class Reader
      def self.from_io(io, url = nil, encoding = nil, options = 0)
        from_memory(io.read, url, encoding, options)
      end
    end

    def self.Schema(string_or_io, options = ParseOptions::DEFAULT_SCHEMA)
      Schema.new(string_or_io, options)
    end

    def self.RelaxNG(string_or_io, options = ParseOptions::DEFAULT_SCHEMA)
      RelaxNG.new(string_or_io, options)
    end

    class Schema
      attr_accessor :parse_options

      def self.new(string_or_io, options = ParseOptions::DEFAULT_SCHEMA)
        file_path = string_or_io.respond_to?(:path) ? string_or_io.path : nil
        src = string_or_io.respond_to?(:read) ? string_or_io.read : string_or_io.to_s
        obj = __compile(src, file_path)
        obj.parse_options = options.is_a?(ParseOptions) ? options : ParseOptions.new(options)
        obj
      end

      def self.from_document(doc, options = ParseOptions::DEFAULT_SCHEMA)
        obj = __compile(doc.to_xml, doc.respond_to?(:url) ? doc.url : nil)
        obj.parse_options = options.is_a?(ParseOptions) ? options : ParseOptions.new(options)
        obj
      end

      def self.read_memory(string, options = ParseOptions::DEFAULT_SCHEMA)
        obj = __compile(string.to_s, nil)
        obj.parse_options = options.is_a?(ParseOptions) ? options : ParseOptions.new(options)
        obj
      end

      def validate(thing, &block)
        xml_str =
          if thing.is_a?(Node)
            thing.to_xml
          elsif thing.is_a?(String) && File.exist?(thing)
            File.read(thing)
          elsif thing.respond_to?(:read)
            thing.read
          else
            thing.to_s
          end
        errs = __validate_xml(xml_str)
        errs.each(&block) if block
        errs
      end

      def valid?(thing)
        validate(thing).empty?
      end
    end

    class RelaxNG < Schema
    end
  end

  module XSLT
    @registered_extensions = {}

    def self.register(uri, custom_handler_class)
      @registered_extensions[uri.to_s] = custom_handler_class
      nil
    end

    def self.registered_extensions
      @registered_extensions
    end

    def self.parse(string, modules = {})
      modules.each { |uri, cls| register(uri, cls) } if modules.is_a?(Hash)
      file_path = string.respond_to?(:path) ? string.path : nil
      src = string.respond_to?(:read) ? string.read : string.to_s
      Stylesheet.__compile(src, file_path)
    end

    def self.quote_params(params)
      parray = params.flatten
      parray.each_with_index do |v, i|
        next if i.even?
        s = v.to_s
        if s.include?("'")
          parts = s.split("'", -1).map { |p| "'#{p}'" }.join(%(, "'", ))
          parray[i] = "concat(#{parts})"
        else
          parray[i] = "'#{s}'"
        end
      end
      parray
    end

    class Stylesheet
      def self.parse_stylesheet_doc(doc)
        __compile(doc.to_xml, doc.respond_to?(:url) ? doc.url : nil)
      end

      def apply_to(document, params = [])
        xml_src = document.is_a?(XML::Node) ? document.to_xml : document.to_s
        out, _method = __apply(xml_src, params)
        out
      end

      def transform(document, params = [])
        xml_src = document.is_a?(XML::Node) ? document.to_xml : document.to_s
        out, method = __apply(xml_src, params)
        if method == "html"
          HTML4::Document.parse(out)
        else
          XML::Document.parse(out)
        end
      end

      def serialize(document)
        document.to_s
      end
    end
  end

  module HTML4
    def self.parse(thing, url = nil, encoding = nil, options = XML::ParseOptions::DEFAULT_HTML, &block)
      Document.parse(thing, url, encoding, options, &block)
    end

    def self.fragment(string, encoding = nil, &block)
      frag = DocumentFragment.parse(string, encoding)
      yield frag if block
      frag
    end

    class ElementDescription
      VOID_ELEMENTS = %w[area base basefont br col command embed frame hr img input isindex keygen link meta param source track wbr].freeze
      BLOCK_ELEMENTS = %w[
        address article aside blockquote canvas dd div dl dt fieldset figcaption figure
        footer form h1 h2 h3 h4 h5 h6 header hgroup hr li main nav noscript ol output p
        pre section table tfoot ul video
      ].freeze
      DEPRECATED_ELEMENTS = %w[applet basefont center dir font isindex menu s strike u big blink marquee tt].freeze

      attr_reader :name

      def self.[](tag_name)
        return nil unless tag_name
        @cache ||= {}
        key = tag_name.to_s.downcase
        @cache[key] ||= new(key)
      end

      def initialize(name)
        @name = name.to_s.downcase
      end

      def empty?
        VOID_ELEMENTS.include?(@name)
      end

      def block?
        BLOCK_ELEMENTS.include?(@name)
      end

      def inline?
        !block?
      end

      def deprecated?
        DEPRECATED_ELEMENTS.include?(@name)
      end

      def save_end_tag?
        !empty?
      end

      def implied_start_tag?
        @name == "html" || @name == "head" || @name == "body" || @name == "tbody"
      end

      def implied_end_tag?
        %w[p li dt dd option thead tbody tfoot tr td th].include?(@name)
      end

      def sub_elements
        []
      end

      def default_sub_element
        nil
      end

      def optional_attributes
        []
      end

      def deprecated_attributes
        []
      end

      def required_attributes
        []
      end

      def description
        @name
      end

      def inspect
        "#<#{self.class}: #{@name}>"
      end
      alias to_s inspect
    end

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

      def meta_encoding
        meta = at_css("meta[charset]")
        return meta["charset"] if meta && meta["charset"]
        css("meta").each do |m|
          next unless m["http-equiv"]&.downcase == "content-type"
          if m["content"] =~ /charset\s*=\s*([^\s;]+)/i
            return $1
          end
        end
        nil
      end

      def meta_encoding=(encoding)
        if (meta = at_css("meta[charset]"))
          meta["charset"] = encoding.to_s
          return encoding
        end
        css("meta").each do |m|
          next unless m["http-equiv"]&.downcase == "content-type"
          if m["content"] =~ /charset\s*=\s*[^\s;]+/i
            m["content"] = m["content"].sub(/charset\s*=\s*[^\s;]+/i, "charset=#{encoding}")
            return encoding
          end
        end
        head = at_css("head") || root
        if head
          m = create_element("meta", "charset" => encoding.to_s)
          head.prepend_child(m)
        end
        encoding
      end

      def fragment(tags = nil)
        (related_class("DocumentFragment") || HTML4::DocumentFragment).new(self, tags)
      end

      def xpath_doctype
        :html4
      end

      def to_s(*args)
        args.empty? ? to_html : serialize(*args)
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
        @ns = nil
        @context = nil
        @arity = block ? block.arity : 0
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
        def initialize(doc = XML::SAX::Document.new, _file_name = nil, _encoding = "UTF-8")
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
          XML::SAX.__emit(@document, @buf, true)
          @buf = +""
          self
        end
      end
    end
  end

  module HTML5
    module QuirksMode
      NO_QUIRKS      = 0
      QUIRKS         = 1
      LIMITED_QUIRKS = 2
    end

    class Builder < HTML4::Builder
      def initialize(options = {}, root = nil, &block)
        @ns = nil
        @context = nil
        @arity = block ? block.arity : 0
        if root
          @doc = root.document
          @parent = root
        else
          @doc = HTML5::Document.new
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
    end

    class Document
      def self.parse(thing, url = nil, encoding = nil, **options, &block)
        src = thing.respond_to?(:read) ? thing.read : thing.to_s
        max_errors = options[:max_errors]
        max_tree_depth = options[:max_tree_depth]
        doc = __parse_html(src, url, encoding, XML::ParseOptions::DEFAULT_HTML, true, max_errors, max_tree_depth)
        if options.key?(:parse_noscript_content_as_text)
          doc.parse_noscript_content_as_text = options[:parse_noscript_content_as_text]
        end
        yield doc if block
        doc
      end

      def self.fragment(tags, encoding = nil, context: nil, **options)
        DocumentFragment.parse(tags, encoding, context: context, **options)
      end

      def xpath_doctype
        :html5
      end

      def quirks_mode
        dtd = internal_subset
        if dtd.nil?
          QuirksMode::QUIRKS
        elsif dtd.name.to_s.downcase == "html" && (dtd.external_id.nil? || dtd.external_id.empty?) && (dtd.system_id.nil? || dtd.system_id.empty?)
          QuirksMode::NO_QUIRKS
        else
          QuirksMode::LIMITED_QUIRKS
        end
      end
    end

    class DocumentFragment
      def self.parse(tags, encoding = nil, context: nil, **_options)
        doc = HTML5::Document.new
        doc.encoding = encoding if encoding
        new(doc, tags, context)
      end
    end

    def self.parse(thing, url = nil, encoding = nil, **options, &block)
      Document.parse(thing, url, encoding, **options, &block)
    end

    def self.fragment(tags, encoding = nil, context: nil, **options)
      DocumentFragment.parse(tags, encoding, context: context, **options)
    end

    def self.read_and_encode(string, encoding)
      src = string.respond_to?(:read) ? string.read : string.to_s
      encoding ? src.dup.force_encoding(encoding) : src
    end
  end

  module CSS
    class SelectorCache
      def initialize
        @cache = {}
      end

      def [](key)
        @cache[key]
      end

      def []=(key, value)
        @cache[key] = value
      end

      def clear_cache
        @cache.clear
      end

      def key(selector:, visitor:)
        [selector, visitor.config]
      end
    end

    class Node
      attr_reader :selector

      def initialize(selector)
        @selector = selector.to_s
      end

      def to_xpath(prefix = "//", _visitor = nil)
        CSS.__xpath_for(@selector, prefix)
      end

      def to_s
        @selector
      end
    end

    class Parser < (defined?(::Racc::Parser) ? ::Racc::Parser : Object)
      def initialize(namespaces = {})
        @namespaces = namespaces || {}
      end

      def parse(selector)
        selector.to_s.split(",").map { |part| Node.new(part.strip) }
      end

      def xpath_for(selector, options = {})
        prefix = options[:prefix] || "//"
        [CSS.__xpath_for(selector.to_s, prefix)]
      end

      def unescape_css_element_name(name)
        name.to_s.gsub(/\\(.)/, '\1')
      end
    end

    def self.parse(selector)
      Parser.new.parse(selector)
    end

    def self.xpath_for(*rules)
      opts = rules.last.is_a?(Hash) ? rules.pop : {}
      prefix = opts[:prefix] || "//"
      rules.map { |r| __xpath_for(r.to_s, prefix) }
    end
  end

  class EncodingHandler
    @handlers = {}

    attr_reader :name

    def initialize(name)
      @name = name.to_s
    end

    def self.[](name)
      key = name.to_s.upcase
      @handlers[key] ||= begin
        Encoding.find(name)
        new(key)
      rescue ArgumentError
        nil
      end
    end

    def self.alias(orig, alias_name)
      h = self[orig]
      @handlers[alias_name.to_s.upcase] = h if h
      h
    end

    def self.delete(name)
      @handlers.delete(name.to_s.upcase)
    end

    def self.clear_aliases!
      @handlers.clear
    end
  end

  module XML
    module SAX
      class ParserContext
        attr_accessor :replace_entities, :recovery

        def self.memory(data, encoding = "UTF-8")
          raise TypeError, "expected String" unless data.is_a?(String)
          new(data, encoding)
        end

        def self.io(io, encoding = 0)
          raise TypeError, "expected IO" unless io.respond_to?(:read)
          new(io.read, encoding)
        end

        def self.file(path, encoding = "UTF-8")
          raise TypeError, "expected String" unless path.is_a?(String)
          new(File.read(path), encoding)
        end

        def initialize(data, encoding = "UTF-8")
          @data = data
          @encoding = encoding
          @replace_entities = false
          @recovery = false
        end

        def parse_with(sax_handler)
          raise TypeError, "expected SAX Parser" unless sax_handler.is_a?(Parser)
          sax_handler.parse_memory(@data)
        end
      end
    end
  end

  module HTML4
    module SAX
      class ParserContext < XML::SAX::ParserContext
        def parse_with(sax_handler)
          raise TypeError, "expected SAX Parser" unless sax_handler.is_a?(XML::SAX::Parser)
          XML::SAX.__emit(sax_handler.document, @data.to_s, true)
        end
      end
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

module Kernel
  private

  def Nokogiri(*args, &block)
    if block_given?
      builder = Nokogiri::HTML4::Builder.new(&block)
      return builder.doc.root
    end
    Nokogiri.parse(*args)
  end
end
